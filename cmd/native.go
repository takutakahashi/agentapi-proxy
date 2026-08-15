package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

const nativeDefaultInstance = "default"

var nativeInstanceNameRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)

var NativeCmd = &cobra.Command{
	Use:   "native",
	Short: "Install and manage a native External Session Manager",
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		// `native list` enumerates every instance, so it does not select one.
		if cmd.Name() == "list" {
			return nil
		}
		return validateNativeInstanceName(nativeManageOpts.instance)
	},
}

type nativeManageOptions struct {
	upstream, name, listen, apiKeyEnv, apiKeyFile, configPath, instance                      string
	scope, teamID, drainTimeout, registrationToken                                           string
	labels                                                                                   []string
	environment                                                                              []string
	defaultManager, apiKeyStdin, force, drain, keepRegistration, keepData, filesystemSandbox bool
	inheritRuntimeProfile                                                                    bool
	logsFollow, logsDaemon                                                                   bool
	jsonOutput                                                                               bool
	logsTail                                                                                 int
}

var nativeManageOpts nativeManageOptions

type nativeRegistrationResponse struct {
	ID              string            `json:"id"`
	InstanceID      string            `json:"instance_id"`
	Name            string            `json:"name"`
	ConnectionToken string            `json:"connection_token"`
	Labels          map[string]string `json:"labels"`
	Created         bool              `json:"created"`
	LastHeartbeatAt *time.Time        `json:"last_heartbeat_at"`
}

func init() {
	p := NativeCmd.PersistentFlags()
	p.StringVar(&nativeManageOpts.configPath, "config", "", "native daemon config path")
	p.StringVar(&nativeManageOpts.instance, "instance", "", "named native ESM instance; omit to select the default instance")
	p.StringVar(&nativeManageOpts.apiKeyEnv, "api-key-env", "AGENTAPI_KEY", "environment variable containing the API key for authenticated lifecycle operations")
	p.StringVar(&nativeManageOpts.apiKeyFile, "api-key-file", "", "file containing the API key for authenticated lifecycle operations")
	p.BoolVar(&nativeManageOpts.apiKeyStdin, "api-key-stdin", false, "read the API key for authenticated lifecycle operations from stdin")

	install := &cobra.Command{Use: "install", Short: "Register and install the native ESM daemon", RunE: runNativeInstall}
	f := install.Flags()
	f.StringVar(&nativeManageOpts.upstream, "upstream", "", "parent agentapi-proxy URL")
	f.StringVar(&nativeManageOpts.name, "name", "", "human-readable manager name")
	f.StringVar(&nativeManageOpts.listen, "listen", ":8080", "native ESM listen address")
	f.StringVar(&nativeManageOpts.scope, "scope", "user", "registration scope: user or team")
	f.StringVar(&nativeManageOpts.teamID, "team-id", "", "team ID when --scope=team")
	f.StringVar(&nativeManageOpts.registrationToken, "registration-token", "", "one-time registration token issued by the parent proxy")
	f.StringSliceVar(&nativeManageOpts.labels, "label", nil, "allocator label in key=value form")
	f.StringArrayVar(&nativeManageOpts.environment, "manager-env", nil, "native manager environment variable in KEY=VALUE form (repeatable)")
	f.BoolVar(&nativeManageOpts.defaultManager, "default", false, "make this the default external session manager")
	f.BoolVar(&nativeManageOpts.force, "force", false, "install even if the existing state directory contains sessions")
	f.BoolVar(&nativeManageOpts.filesystemSandbox, "filesystem-sandbox", false, "sandbox native session filesystem access on macOS")
	f.BoolVar(&nativeManageOpts.inheritRuntimeProfile, "inherit-runtime-profile", false, "apply runtime profile received from the parent proxy")

	status := &cobra.Command{Use: "status", Short: "Show daemon and connection status", RunE: runNativeStatus}
	status.Flags().BoolVar(&nativeManageOpts.jsonOutput, "json", false, "output machine-readable JSON")
	doctor := &cobra.Command{Use: "doctor", Short: "Validate daemon configuration and connectivity", RunE: runNativeDoctor}
	doctor.Flags().BoolVar(&nativeManageOpts.jsonOutput, "json", false, "output machine-readable JSON")
	restart := &cobra.Command{Use: "restart", Short: "Restart the native ESM daemon", RunE: runNativeRestart}
	update := &cobra.Command{Use: "update", Short: "Replace the managed daemon binary with this executable and restart", RunE: runNativeUpdate}
	rotate := &cobra.Command{Use: "rotate-token", Short: "Rotate the ESM connection token and restart", RunE: runNativeRotateToken}
	uninstall := &cobra.Command{Use: "uninstall", Short: "Stop and remove the native ESM daemon", RunE: runNativeUninstall}
	uninstall.Flags().BoolVar(&nativeManageOpts.force, "force", false, "terminate active sessions")
	uninstall.Flags().BoolVar(&nativeManageOpts.drain, "drain", false, "wait for active sessions to finish")
	uninstall.Flags().StringVar(&nativeManageOpts.drainTimeout, "drain-timeout", "30m", "maximum time to wait with --drain")
	uninstall.Flags().BoolVar(&nativeManageOpts.keepRegistration, "keep-registration", false, "keep the parent registration")
	uninstall.Flags().BoolVar(&nativeManageOpts.keepData, "keep-data", false, "keep daemon state and configuration")
	listCmd := &cobra.Command{Use: "list", Short: "List installed native ESM instances on this host", Args: cobra.NoArgs, RunE: runNativeList}
	listCmd.Flags().BoolVar(&nativeManageOpts.jsonOutput, "json", false, "output machine-readable JSON")
	sessionList := &cobra.Command{Use: "session-list", Aliases: []string{"sessions"}, Short: "List native sessions on this host", Args: cobra.NoArgs, RunE: runNativeSessionList}
	sessionList.Flags().BoolVar(&nativeManageOpts.jsonOutput, "json", false, "output machine-readable JSON")
	logs := &cobra.Command{Use: "logs [session-id]", Short: "Show native daemon or session logs", Args: cobra.MaximumNArgs(1), RunE: runNativeLogs}
	logs.Flags().BoolVarP(&nativeManageOpts.logsFollow, "follow", "f", false, "follow log output")
	logs.Flags().IntVarP(&nativeManageOpts.logsTail, "tail", "n", 100, "number of lines to show")
	logs.Flags().BoolVar(&nativeManageOpts.logsDaemon, "daemon", false, "show the native daemon log")
	NativeCmd.AddCommand(install, status, doctor, restart, update, rotate, uninstall, listCmd, sessionList, logs)
}

// resolveNativeInstance validates and normalizes the selected instance name,
// returning nativeDefaultInstance when --instance is omitted.
func resolveNativeInstance() (string, error) {
	if err := validateNativeInstanceName(nativeManageOpts.instance); err != nil {
		return "", err
	}
	if nativeManageOpts.instance == "" {
		return nativeDefaultInstance, nil
	}
	return nativeManageOpts.instance, nil
}

func validateNativeInstanceName(name string) error {
	if name == "" {
		return nil
	}
	if name == nativeDefaultInstance {
		return nil
	}
	if !nativeInstanceNameRegexp.MatchString(name) {
		return fmt.Errorf("invalid --instance %q; use 1-32 lowercase letters, digits, and hyphens (hyphens only in the middle)", name)
	}
	return nil
}

func runNativeInstall(command *cobra.Command, _ []string) error {
	if nativeManageOpts.upstream == "" {
		return errors.New("--upstream is required")
	}
	if nativeManageOpts.filesystemSandbox && runtime.GOOS != "darwin" {
		return errors.New("--filesystem-sandbox is only supported on macOS")
	}
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	if nativeManageOpts.configPath != "" && instance != nativeDefaultInstance {
		return errors.New("--config cannot be combined with a non-default --instance; the instance selects its own isolated configuration path")
	}
	if instance != nativeDefaultInstance && !command.Flags().Changed("listen") {
		return errors.New("--listen is required for non-default instances; specify a distinct port so it does not collide with the default :8080")
	}
	hostname, _ := os.Hostname()
	if nativeManageOpts.name == "" {
		nativeManageOpts.name = hostname
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	active, _ := filepath.Glob(filepath.Join(paths.state, "sessions", "*"))
	if len(active) > 0 && !nativeManageOpts.force {
		return fmt.Errorf("refusing to replace daemon with %d session(s) in state; drain them first or use --force", len(active))
	}
	existing, _ := readNativeConfig(paths.config)
	instanceID := existing.InstanceID
	if instanceID == "" {
		instanceID = stableNativeInstanceID(hostname, instance)
	}
	labels := map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH, "hostname": hostname, "native_instance": instance}
	for _, raw := range nativeManageOpts.labels {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return fmt.Errorf("invalid --label %q; expected key=value", raw)
		}
		key := strings.TrimSpace(parts[0])
		if key == "native_instance" {
			return fmt.Errorf("invalid --label %q; the native_instance label is managed automatically by --instance", raw)
		}
		labels[key] = parts[1]
	}
	environment := make(map[string]string, len(existing.ManagerEnvironment)+len(nativeManageOpts.environment))
	for key, value := range existing.ManagerEnvironment {
		environment[key] = value
	}
	for _, raw := range nativeManageOpts.environment {
		key, value, parseErr := parseNativeManagerEnvironment(raw)
		if parseErr != nil {
			return parseErr
		}
		environment[key] = value
	}
	if nativeManageOpts.registrationToken == "" {
		return errors.New("--registration-token is required")
	}
	payload := map[string]interface{}{
		"instance_id": instanceID, "name": nativeManageOpts.name,
		"labels": labels, "default": nativeManageOpts.defaultManager,
		"version": nativeBuildVersion(), "registration_token": nativeManageOpts.registrationToken,
	}
	registration, err := enrollNativeManager(nativeManageOpts.upstream, payload)
	if err != nil {
		return err
	}
	token := registration.ConnectionToken
	if token == "" {
		token = existing.ConnectionToken
	}
	if token == "" {
		return errors.New("registration did not return a connection token; run native rotate-token")
	}
	cfg := nativeDaemonConfig{Listen: nativeManageOpts.listen, UpstreamURL: strings.TrimRight(nativeManageOpts.upstream, "/"),
		ConnectionToken: token, StateDir: paths.state,
		BinaryPath: paths.binary, ManagerID: registration.ID, InstanceID: instanceID, Scope: nativeManageOpts.scope, TeamID: nativeManageOpts.teamID,
		Labels: labels, ManagerEnvironment: environment, Version: nativeBuildVersion(),
		FilesystemSandbox:     nativeFilesystemSandboxConfig{Enabled: nativeManageOpts.filesystemSandbox},
		InheritRuntimeProfile: nativeManageOpts.inheritRuntimeProfile}
	if err := installNativeService(paths, cfg); err != nil {
		return err
	}
	if err := waitNativeHealth(cfg.Listen, 30*time.Second); err != nil {
		return err
	}
	if err := sendNativeHeartbeat(cfg); err != nil {
		return fmt.Errorf("daemon installed but parent heartbeat failed: %w", err)
	}
	fmt.Printf("Native ESM installed\nInstance: %s\nManager ID: %s\nService: %s\nLabels: %s\n", instance, registration.ID, nativeServiceName(instance), formatLabels(labels))
	return nil
}

type nativeInstallPaths struct{ config, credentials, state, binary, service, logDir string }

func nativePaths(configOverride, instance string) (nativeInstallPaths, error) {
	if runtime.GOOS == "linux" && os.Geteuid() != 0 && configOverride == "" {
		return nativeInstallPaths{}, errors.New("native install on Linux must run as root (use sudo)")
	}
	paths, err := nativeInstallPathsFor(runtime.GOOS, instance, configOverride)
	if err != nil {
		return nativeInstallPaths{}, err
	}
	return paths, nil
}

// nativeInstallPathsFor computes the install paths for an instance on the given
// OS without touching the filesystem or checking privileges, which makes it
// testable from non-root test processes. The default instance preserves the
// historical paths and service names exactly.
func nativeInstallPathsFor(goos, instance, configOverride string) (nativeInstallPaths, error) {
	if instance == "" {
		instance = nativeDefaultInstance
	}
	isDefault := instance == nativeDefaultInstance
	if goos == "linux" {
		var configDir, credentials, state, logDir string
		binary := "/usr/local/libexec/agentapi-proxy/ccplant"
		if isDefault {
			configDir = "/etc/agentapi-native"
			credentials = "/etc/agentapi-native/credentials.json"
			state = "/var/lib/agentapi-native"
			logDir = "/var/log/agentapi-native"
		} else {
			configDir = "/etc/agentapi-native-" + instance
			credentials = filepath.Join(configDir, "credentials.json")
			state = "/var/lib/agentapi-native-" + instance
			logDir = "/var/log/agentapi-native-" + instance
		}
		config := filepath.Join(configDir, "config.json")
		if configOverride != "" {
			config = configOverride
		}
		service := filepath.Join("/etc/systemd/system", nativeServiceNameFor(goos, instance))
		return nativeInstallPaths{config: config, credentials: credentials, state: state, binary: binary, service: service, logDir: logDir}, nil
	}
	if goos == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nativeInstallPaths{}, err
		}
		var base, logDir string
		if isDefault {
			base = filepath.Join(home, "Library", "Application Support", "agentapi-native")
			logDir = filepath.Join(home, "Library", "Logs", "agentapi-native")
		} else {
			base = filepath.Join(home, "Library", "Application Support", "agentapi-native-"+instance)
			logDir = filepath.Join(home, "Library", "Logs", "agentapi-native-"+instance)
		}
		config := filepath.Join(base, "config.json")
		if configOverride != "" {
			config = configOverride
		}
		return nativeInstallPaths{config: config, credentials: filepath.Join(base, "credentials.json"), state: filepath.Join(base, "state"), binary: filepath.Join(base, "bin", "ccplant"), service: filepath.Join(home, "Library", "LaunchAgents", nativeServiceNameFor(goos, instance)+".plist"), logDir: logDir}, nil
	}
	return nativeInstallPaths{}, fmt.Errorf("unsupported OS: %s", goos)
}

func installNativeService(paths nativeInstallPaths, cfg nativeDaemonConfig) error {
	for _, dir := range []string{filepath.Dir(paths.config), paths.state, filepath.Dir(paths.binary), paths.logDir, filepath.Dir(paths.service)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if runtime.GOOS == "linux" {
		if err := ensureLinuxServiceUser(); err != nil {
			return err
		}
		uid, gid := lookupUID("agentapi"), lookupGID("agentapi")
		if err := filepath.Walk(paths.state, func(path string, _ os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			return os.Chown(path, uid, gid)
		}); err != nil {
			return err
		}
	}
	if err := copyExecutable(paths.binary); err != nil {
		return err
	}
	cfg.CredentialsPath = paths.credentials
	credentials, _ := json.MarshalIndent(map[string]string{"connection_token": cfg.ConnectionToken}, "", "  ")
	if err := atomicWriteFile(paths.credentials, append(credentials, '\n'), 0o600); err != nil {
		return err
	}
	cfg.ConnectionToken = ""
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := atomicWriteFile(paths.config, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		if err := secureNativeConfig(paths.credentials); err != nil {
			return err
		}
		if err := secureNativeConfig(paths.config); err != nil {
			return err
		}
		unit := renderNativeSystemdUnit(paths, cfg.ManagerEnvironment)
		if err := atomicWriteFile(paths.service, []byte(unit), 0o644); err != nil {
			return err
		}
		if err := runCommand("systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := runCommand("systemctl", "enable", nativeServiceUnitName(paths.service)); err != nil {
			return err
		}
		return runCommand("systemctl", "restart", nativeServiceUnitName(paths.service))
	}
	plist := renderNativeLaunchAgent(paths, cfg.ManagerEnvironment)
	if err := atomicWriteFile(paths.service, []byte(plist), 0o600); err != nil {
		return err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = runCommand("launchctl", "bootout", domain+"/"+nativeLaunchLabel(paths.service))
	return runCommand("launchctl", "bootstrap", domain, paths.service)
}

func parseNativeManagerEnvironment(raw string) (string, string, error) {
	key, value, ok := strings.Cut(raw, "=")
	if !ok || !validEnvironmentKey(key) {
		return "", "", fmt.Errorf("invalid --manager-env %q; expected KEY=VALUE", raw)
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", "", fmt.Errorf("invalid --manager-env %q; value must not contain NUL or newlines", raw)
	}
	return key, value, nil
}

func validEnvironmentKey(key string) bool {
	if key == "" || (!asciiLetter(key[0]) && key[0] != '_') {
		return false
	}
	for i := 1; i < len(key); i++ {
		if !asciiLetter(key[i]) && (key[i] < '0' || key[i] > '9') && key[i] != '_' {
			return false
		}
	}
	return true
}

func asciiLetter(value byte) bool {
	return (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z')
}

func sortedEnvironmentKeys(environment map[string]string) []string {
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func renderNativeSystemdUnit(paths nativeInstallPaths, environment map[string]string) string {
	var env strings.Builder
	for _, key := range sortedEnvironmentKeys(environment) {
		value := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%").Replace(environment[key])
		fmt.Fprintf(&env, "Environment=\"%s=%s\"\n", key, value)
	}
	return fmt.Sprintf("[Unit]\nDescription=agentapi-proxy native external session manager\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nUser=agentapi\nGroup=agentapi\n%sExecStart=%s native-session-manager --config %s\nRestart=always\nRestartSec=3\nKillMode=process\nTimeoutStopSec=30\nLimitNOFILE=65536\n\n[Install]\nWantedBy=multi-user.target\n", env.String(), paths.binary, paths.config)
}

func renderNativeLaunchAgent(paths nativeInstallPaths, environment map[string]string) string {
	label := nativeLaunchLabel(paths.service)
	var env strings.Builder
	if len(environment) > 0 {
		env.WriteString("<key>EnvironmentVariables</key><dict>")
		for _, key := range sortedEnvironmentKeys(environment) {
			fmt.Fprintf(&env, "<key>%s</key><string>%s</string>", xmlEscape(key), xmlEscape(environment[key]))
		}
		env.WriteString("</dict>")
	}
	return fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array><string>%s</string><string>native-session-manager</string><string>--config</string><string>%s</string></array>%s<key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>StandardOutPath</key><string>%s/native.log</string><key>StandardErrorPath</key><string>%s/native.log</string></dict></plist>\n", xmlEscape(label), xmlEscape(paths.binary), xmlEscape(paths.config), env.String(), xmlEscape(paths.logDir), xmlEscape(paths.logDir))
}

type nativeStatusOutput struct {
	Instance          string            `json:"instance"`
	Service           string            `json:"service"`
	ManagerID         string            `json:"manager_id"`
	Upstream          string            `json:"upstream"`
	Labels            map[string]string `json:"labels"`
	Version           string            `json:"version"`
	FilesystemSandbox bool              `json:"filesystem_sandbox"`
	ActiveSessions    int               `json:"active_sessions"`
	Health            string            `json:"health"`
	State             string            `json:"state"`
}

func runNativeStatus(command *cobra.Command, _ []string) error {
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		return err
	}
	service := "stopped"
	if nativeServiceRunning(instance) {
		service = "running"
	}
	health := "unreachable"
	if nativeHealth(cfg.Listen) == nil {
		health = "ok"
	}
	active, _ := filepath.Glob(filepath.Join(cfg.StateDir, "sessions", "*"))
	status := nativeStatusOutput{Instance: instance, Service: service, ManagerID: cfg.ManagerID, Upstream: cfg.UpstreamURL,
		Labels: cfg.Labels, Version: cfg.Version, FilesystemSandbox: cfg.FilesystemSandbox.Enabled,
		ActiveSessions: len(active), Health: health, State: cfg.StateDir}
	if nativeManageOpts.jsonOutput {
		return json.NewEncoder(command.OutOrStdout()).Encode(status)
	}
	_, err = fmt.Fprintf(command.OutOrStdout(), "Instance: %s\nService: %s\nManager ID: %s\nUpstream: %s\nLabels: %s\nVersion: %s\nFilesystem sandbox: %t\nActive sessions: %d\nHealth: %s\nState: %s\n", instance, service, cfg.ManagerID, cfg.UpstreamURL, formatLabels(cfg.Labels), cfg.Version, cfg.FilesystemSandbox.Enabled, len(active), health, cfg.StateDir)
	return err
}

type nativeSessionListEntry struct {
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	Status    string    `json:"status"`
	LogPath   string    `json:"log_path"`
}

func readNativeSessionList(stateDir string) ([]nativeSessionListEntry, error) {
	sessionDirs, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*"))
	if err != nil {
		return nil, err
	}
	entries := make([]nativeSessionListEntry, 0, len(sessionDirs))
	for _, sessionDir := range sessionDirs {
		data, readErr := os.ReadFile(filepath.Join(sessionDir, "runtime", "state.json"))
		if readErr != nil {
			continue
		}
		var entry nativeSessionListEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil, fmt.Errorf("read session state %s: %w", filepath.Base(sessionDir), err)
		}
		if entry.ID == "" {
			entry.ID = filepath.Base(sessionDir)
		}
		entry.LogPath = filepath.Join(sessionDir, "runtime", "provisioner.log")
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].StartedAt.Before(entries[j].StartedAt) })
	return entries, nil
}

func runNativeSessionList(command *cobra.Command, _ []string) error {
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		return err
	}
	entries, err := readNativeSessionList(cfg.StateDir)
	if err != nil {
		return err
	}
	if nativeManageOpts.jsonOutput {
		return json.NewEncoder(command.OutOrStdout()).Encode(entries)
	}
	w := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "SESSION ID\tSTATUS\tPID\tSTARTED\tLOG")
	for _, entry := range entries {
		started := "-"
		if !entry.StartedAt.IsZero() {
			started = entry.StartedAt.Local().Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", entry.ID, entry.Status, entry.PID, started, entry.LogPath)
	}
	return w.Flush()
}

func nativeLogPath(paths nativeInstallPaths, cfg nativeDaemonConfig, sessionID string, daemon bool) (string, error) {
	if daemon {
		if sessionID != "" {
			return "", errors.New("a session ID cannot be used with --daemon")
		}
		return filepath.Join(paths.logDir, "native.log"), nil
	}
	if sessionID == "" {
		return "", errors.New("session ID is required (or use --daemon)")
	}
	if sessionID == "." || filepath.Base(sessionID) != sessionID {
		return "", errors.New("invalid session ID")
	}
	return filepath.Join(cfg.StateDir, "sessions", sessionID, "runtime", "provisioner.log"), nil
}

func runNativeLogs(command *cobra.Command, args []string) error {
	if nativeManageOpts.logsTail < 0 {
		return errors.New("--tail must be zero or greater")
	}
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		return err
	}
	sessionID := ""
	if len(args) == 1 {
		sessionID = args[0]
	}
	logPath, err := nativeLogPath(paths, cfg, sessionID, nativeManageOpts.logsDaemon)
	if err != nil {
		return err
	}
	if _, err := os.Stat(logPath); err != nil {
		return fmt.Errorf("open log %s: %w", logPath, err)
	}
	tailArgs := []string{"-n", strconv.Itoa(nativeManageOpts.logsTail)}
	if nativeManageOpts.logsFollow {
		tailArgs = append(tailArgs, "-F")
	}
	tailArgs = append(tailArgs, logPath)
	tail := exec.CommandContext(command.Context(), "tail", tailArgs...)
	tail.Stdout = command.OutOrStdout()
	tail.Stderr = command.ErrOrStderr()
	return tail.Run()
}

type nativeDoctorCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type nativeDoctorOutput struct {
	OK     bool                `json:"ok"`
	Checks []nativeDoctorCheck `json:"checks"`
}

func runNativeDoctor(command *cobra.Command, _ []string) error {
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	result := nativeDoctorOutput{OK: true, Checks: make([]nativeDoctorCheck, 0, 5)}
	var firstErr error
	check := func(id, success string, err error) {
		entry := nativeDoctorCheck{ID: id, Status: "ok", Message: success}
		if err != nil {
			entry.Status = "error"
			entry.Message = err.Error()
			result.OK = false
			if firstErr == nil {
				firstErr = err
			}
		}
		result.Checks = append(result.Checks, entry)
	}
	for _, file := range []struct{ name, path string }{{"config", paths.config}, {"credentials", paths.credentials}} {
		name, path := file.name, file.path
		info, err := os.Stat(path)
		if err != nil {
			check(name, name+" permissions are secure", fmt.Errorf("%s: %w", name, err))
			continue
		}
		if info.Mode().Perm()&0o037 != 0 {
			check(name, name+" permissions are secure", fmt.Errorf("%s permissions are too open: %o", name, info.Mode().Perm()))
			continue
		}
		check(name, name+" permissions are secure", nil)
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		check("config_load", "configuration loaded", fmt.Errorf("configuration: %w", err))
		if nativeManageOpts.jsonOutput {
			return json.NewEncoder(command.OutOrStdout()).Encode(result)
		}
		return err
	}
	check("config_load", "configuration loaded", nil)
	if cfg.FilesystemSandbox.Enabled {
		var sandboxErr error
		if runtime.GOOS != "darwin" {
			sandboxErr = errors.New("filesystem sandbox is enabled but this host is not macOS")
		} else if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
			sandboxErr = fmt.Errorf("filesystem sandbox executable: %w", err)
		}
		check("filesystem_sandbox", "filesystem sandbox is available", sandboxErr)
	} else {
		check("filesystem_sandbox", "filesystem sandbox is disabled", nil)
	}
	localErr := nativeHealth(cfg.Listen)
	if localErr != nil {
		localErr = fmt.Errorf("local health: %w", localErr)
	}
	check("local_health", "local daemon is healthy", localErr)
	heartbeatErr := sendNativeHeartbeat(cfg)
	if heartbeatErr != nil {
		heartbeatErr = fmt.Errorf("parent heartbeat: %w", heartbeatErr)
	}
	check("parent_heartbeat", "parent heartbeat succeeded", heartbeatErr)
	if nativeManageOpts.jsonOutput {
		return json.NewEncoder(command.OutOrStdout()).Encode(result)
	}
	if firstErr != nil {
		return firstErr
	}
	_, err = fmt.Fprintln(command.OutOrStdout(), "OK: service, config permissions, local health, and parent heartbeat")
	return err
}

func runNativeRestart(_ *cobra.Command, _ []string) error {
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	return restartNativeService(instance)
}

func runNativeUpdate(_ *cobra.Command, _ []string) error {
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		return fmt.Errorf("read native daemon config: %w", err)
	}
	active, _ := filepath.Glob(filepath.Join(cfg.StateDir, "sessions", "*"))
	if len(active) > 0 {
		return fmt.Errorf("refusing to update daemon with %d active session(s); drain them first", len(active))
	}
	if err := copyExecutable(paths.binary); err != nil {
		return fmt.Errorf("replace managed daemon binary: %w", err)
	}
	cfg.Version = nativeBuildVersion()
	cfg.ConnectionToken = ""
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode updated native daemon config: %w", err)
	}
	if err := atomicWriteFile(paths.config, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write updated native daemon config: %w", err)
	}
	if err := secureNativeConfig(paths.config); err != nil {
		return fmt.Errorf("secure updated native daemon config: %w", err)
	}
	if err := restartNativeService(instance); err != nil {
		return fmt.Errorf("restart native daemon: %w", err)
	}
	if err := waitNativeHealth(cfg.Listen, 30*time.Second); err != nil {
		return fmt.Errorf("updated daemon did not become healthy: %w", err)
	}
	fmt.Printf("Native ESM instance %q updated and restarted\n", instance)
	return nil
}

func runNativeRotateToken(_ *cobra.Command, _ []string) error {
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		return err
	}
	apiKey, err := readLifecycleAPIKey()
	if err != nil {
		return err
	}
	query := url.Values{}
	if cfg.Scope != "" {
		query.Set("scope", cfg.Scope)
	}
	if cfg.TeamID != "" {
		query.Set("team_id", cfg.TeamID)
	}
	endpoint := strings.TrimRight(cfg.UpstreamURL, "/") + "/external-session-managers/" + url.PathEscape(cfg.ManagerID) + "/rotate-token"
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	req, _ := http.NewRequest(http.MethodPost, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return responseError(resp)
	}
	var registration nativeRegistrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&registration); err != nil {
		return err
	}
	cfg.ConnectionToken = registration.ConnectionToken
	credentials, _ := json.MarshalIndent(map[string]string{"connection_token": cfg.ConnectionToken}, "", "  ")
	if err := atomicWriteFile(cfg.CredentialsPath, append(credentials, '\n'), 0o600); err != nil {
		return err
	}
	if err := secureNativeConfig(cfg.CredentialsPath); err != nil {
		return err
	}
	if err := restartNativeService(instance); err != nil {
		return err
	}
	fmt.Printf("Connection token rotated and daemon %s restarted\n", nativeServiceName(instance))
	return nil
}

func runNativeUninstall(_ *cobra.Command, _ []string) error {
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	active, _ := filepath.Glob(filepath.Join(cfg.StateDir, "sessions", "*"))
	if len(active) > 0 && nativeManageOpts.drain {
		timeout, parseErr := time.ParseDuration(nativeManageOpts.drainTimeout)
		if parseErr != nil {
			return parseErr
		}
		deadline := time.Now().Add(timeout)
		for len(active) > 0 && time.Now().Before(deadline) {
			time.Sleep(2 * time.Second)
			active, _ = filepath.Glob(filepath.Join(cfg.StateDir, "sessions", "*"))
		}
	}
	if len(active) > 0 && !nativeManageOpts.force {
		return fmt.Errorf("refusing to uninstall: %d active session(s); use --drain or --force", len(active))
	}
	if len(active) > 0 && nativeManageOpts.force {
		terminateNativeSessions(active)
	}
	if runtime.GOOS == "linux" {
		_ = runCommand("systemctl", "disable", "--now", nativeServiceUnitName(paths.service))
		_ = os.Remove(paths.service)
		_ = runCommand("systemctl", "daemon-reload")
	} else {
		_ = runCommand("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+nativeLaunchLabel(paths.service))
		_ = os.Remove(paths.service)
	}
	if !nativeManageOpts.keepRegistration && cfg.ManagerID != "" {
		apiKey, keyErr := readLifecycleAPIKey()
		if keyErr != nil {
			return fmt.Errorf("service removed; registration not removed: %w", keyErr)
		}
		query := url.Values{}
		if cfg.Scope != "" {
			query.Set("scope", cfg.Scope)
		}
		if cfg.TeamID != "" {
			query.Set("team_id", cfg.TeamID)
		}
		endpoint := strings.TrimRight(cfg.UpstreamURL, "/") + "/external-session-managers/" + url.PathEscape(cfg.ManagerID)
		if encoded := query.Encode(); encoded != "" {
			endpoint += "?" + encoded
		}
		req, _ := http.NewRequest(http.MethodDelete, endpoint, nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, requestErr := http.DefaultClient.Do(req)
		if requestErr != nil {
			return requestErr
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("registration delete returned HTTP %d", resp.StatusCode)
		}
	}
	if !nativeManageOpts.keepData {
		// The managed binary may be shared by other instances (notably on Linux,
		// where every instance installs under /usr/local/libexec). Only remove it
		// when no other discovered instance still references it.
		if !nativeBinaryUsedByOtherInstances(instance, paths.binary) {
			_ = os.Remove(paths.binary)
		}
		_ = os.Remove(paths.config)
		_ = os.Remove(paths.credentials)
		if safeNativeStateDir(cfg.StateDir) {
			_ = os.RemoveAll(cfg.StateDir)
		}
	}
	fmt.Printf("Native ESM instance %q uninstalled\n", instance)
	return nil
}

func terminateNativeSessions(sessionDirs []string) {
	for _, dir := range sessionDirs {
		data, err := os.ReadFile(filepath.Join(dir, "runtime", "state.json"))
		if err != nil {
			continue
		}
		var state struct {
			PID int `json:"pid"`
		}
		if json.Unmarshal(data, &state) == nil && state.PID > 1 {
			_ = syscall.Kill(-state.PID, syscall.SIGTERM)
		}
	}
	time.Sleep(time.Second)
}

func safeNativeStateDir(path string) bool {
	clean := filepath.Clean(path)
	return clean != "/" && clean != "." && len(strings.Split(clean, string(filepath.Separator))) >= 3
}

// nativeBinaryUsedByOtherInstances reports whether any discovered instance
// other than the one being uninstalled references the same managed binary.
func nativeBinaryUsedByOtherInstances(currentInstance, binaryPath string) bool {
	return binarySharedWith(nativeDiscoverInstances(), currentInstance, binaryPath)
}

func binarySharedWith(entries []nativeInstanceListEntry, currentInstance, binaryPath string) bool {
	if binaryPath == "" {
		return false
	}
	for _, entry := range entries {
		if entry.Instance == currentInstance {
			continue
		}
		if entry.BinaryPath == binaryPath {
			return true
		}
	}
	return false
}

func enrollNativeManager(upstream string, payload interface{}) (*nativeRegistrationResponse, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(upstream, "/")+"/external-session-managers/enroll", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp)
	}
	var result nativeRegistrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func sendNativeHeartbeat(cfg nativeDaemonConfig) error {
	body, _ := json.Marshal(map[string]interface{}{"version": nativeBuildVersion()})
	req, _ := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.UpstreamURL, "/")+"/external-session-managers/"+cfg.ManagerID+"/heartbeat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+cfg.ConnectionToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return responseError(resp)
	}
	return nil
}

func readLifecycleAPIKey() (string, error) {
	var value string
	if nativeManageOpts.apiKeyStdin {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 64*1024))
		if err != nil {
			return "", err
		}
		value = string(data)
	} else if nativeManageOpts.apiKeyFile != "" {
		data, err := os.ReadFile(nativeManageOpts.apiKeyFile)
		if err != nil {
			return "", err
		}
		value = string(data)
	} else {
		value = os.Getenv(nativeManageOpts.apiKeyEnv)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("API key is required for this lifecycle operation; use --api-key-stdin, --api-key-file, or --api-key-env")
	}
	return value, nil
}

func readNativeConfig(path string) (nativeDaemonConfig, error) {
	var cfg nativeDaemonConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err = json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.ConnectionToken == "" && cfg.CredentialsPath != "" {
		credentialsData, readErr := os.ReadFile(cfg.CredentialsPath)
		if readErr != nil {
			return cfg, readErr
		}
		var credentials struct {
			ConnectionToken string `json:"connection_token"`
		}
		if err = json.Unmarshal(credentialsData, &credentials); err != nil {
			return cfg, err
		}
		cfg.ConnectionToken = credentials.ConnectionToken
	}
	return cfg, nil
}
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".native-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
func copyExecutable(destination string) error {
	source, err := os.Executable()
	if err != nil {
		return err
	}
	source, _ = filepath.EvalSymlinks(source)
	if source == destination {
		return nil
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".agentapi-proxy-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err = tmp.Chmod(0o755); err == nil {
		_, err = io.Copy(tmp, in)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, destination)
}
func stableNativeInstanceID(hostname, instance string) string {
	source := hostname
	if instance != "" && instance != nativeDefaultInstance {
		// Keep non-default instance IDs distinct from the default instance while
		// remaining stable across reinstalls of the same named instance.
		source += "\x00" + instance
	}
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		source += string(data)
	}
	sum := sha256.Sum256([]byte(source))
	return "native-" + sanitizeID(hostname) + "-" + hex.EncodeToString(sum[:4])
}
func sanitizeID(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
func formatLabels(labels map[string]string) string {
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}
func responseError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
}
func nativeHealth(listen string) error {
	address := listen
	if strings.HasPrefix(address, ":") {
		address = "127.0.0.1" + address
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + address + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
func waitNativeHealth(listen string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if nativeHealth(listen) == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("native daemon did not become healthy")
}
func runCommand(name string, args ...string) error {
	command := exec.Command(name, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// nativeServiceName returns the OS-native service identifier (systemd unit or
// launchd label) for the given instance. The default instance preserves the
// historical identifiers so existing installations keep working unchanged.
func nativeServiceName(instance string) string {
	return nativeServiceNameFor(runtime.GOOS, instance)
}

func nativeServiceNameFor(goos, instance string) string {
	if instance == "" {
		instance = nativeDefaultInstance
	}
	if goos == "linux" {
		if instance == nativeDefaultInstance {
			return "agentapi-native.service"
		}
		return "agentapi-native-" + instance + ".service"
	}
	if instance == nativeDefaultInstance {
		return "com.agentapi.native"
	}
	return "com.agentapi.native." + instance
}

// nativeServiceUnitName derives the systemd unit name from a service file path.
func nativeServiceUnitName(servicePath string) string { return filepath.Base(servicePath) }

// nativeLaunchLabel derives the launchd label from a plist service file path.
func nativeLaunchLabel(servicePath string) string {
	return strings.TrimSuffix(filepath.Base(servicePath), ".plist")
}

func nativeServiceRunning(instance string) bool {
	var command *exec.Cmd
	if runtime.GOOS == "linux" {
		command = exec.Command("systemctl", "is-active", "--quiet", nativeServiceName(instance))
	} else {
		command = exec.Command("launchctl", "print", "gui/"+strconv.Itoa(os.Getuid())+"/"+nativeServiceName(instance))
	}
	return command.Run() == nil
}
func restartNativeService(instance string) error {
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		return fmt.Errorf("read native daemon config: %w", err)
	}
	if runtime.GOOS == "linux" {
		unit := renderNativeSystemdUnit(paths, cfg.ManagerEnvironment)
		if err := atomicWriteFile(paths.service, []byte(unit), 0o644); err != nil {
			return fmt.Errorf("refresh native service environment: %w", err)
		}
		if err := runCommand("systemctl", "daemon-reload"); err != nil {
			return err
		}
		return runCommand("systemctl", "restart", nativeServiceName(instance))
	}
	plist := renderNativeLaunchAgent(paths, cfg.ManagerEnvironment)
	if err := atomicWriteFile(paths.service, []byte(plist), 0o600); err != nil {
		return fmt.Errorf("refresh native service environment: %w", err)
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = runCommand("launchctl", "bootout", domain+"/"+nativeServiceName(instance))
	return runCommand("launchctl", "bootstrap", domain, paths.service)
}
func ensureLinuxServiceUser() error {
	if exec.Command("id", "-u", "agentapi").Run() == nil {
		return nil
	}
	return runCommand("useradd", "--system", "--home-dir", "/var/lib/agentapi-native", "--shell", "/usr/sbin/nologin", "agentapi")
}
func lookupUID(name string) int {
	out, err := exec.Command("id", "-u", name).Output()
	if err != nil {
		return -1
	}
	value, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return value
}
func lookupGID(name string) int {
	out, err := exec.Command("id", "-g", name).Output()
	if err != nil {
		return -1
	}
	value, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return value
}
func secureNativeConfig(path string) error {
	if runtime.GOOS != "linux" {
		return os.Chmod(path, 0o600)
	}
	if err := os.Chown(path, 0, lookupGID("agentapi")); err != nil {
		return err
	}
	return os.Chmod(path, 0o640)
}
func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}

// nativeInstanceListEntry describes one discovered native ESM instance for
// `native list`.
type nativeInstanceListEntry struct {
	Instance   string `json:"instance"`
	Service    string `json:"service"`
	ManagerID  string `json:"manager_id"`
	Upstream   string `json:"upstream"`
	State      string `json:"state"`
	BinaryPath string `json:"binary_path,omitempty"`
	Config     string `json:"config"`
	Running    bool   `json:"running"`
}

func runNativeList(command *cobra.Command, _ []string) error {
	entries := nativeDiscoverInstances()
	if nativeManageOpts.jsonOutput {
		return json.NewEncoder(command.OutOrStdout()).Encode(entries)
	}
	w := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "INSTANCE\tSERVICE\tMANAGER ID\tSTATE\tRUNNING")
	for _, entry := range entries {
		running := "no"
		if entry.Running {
			running = "yes"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", entry.Instance, entry.Service, entry.ManagerID, entry.State, running)
	}
	return w.Flush()
}

// nativeInstanceBaseDirs returns the default instance config path and the glob
// pattern matching named-instance config paths for the current platform.
func nativeInstanceBaseDirs() (defaultConfig, pattern string) {
	return nativeInstanceBaseDirsFor(runtime.GOOS)
}

func nativeInstanceBaseDirsFor(goos string) (defaultConfig, pattern string) {
	if goos == "linux" {
		return "/etc/agentapi-native/config.json", "/etc/agentapi-native-*/config.json"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	base := filepath.Join(home, "Library", "Application Support")
	return filepath.Join(base, "agentapi-native", "config.json"), filepath.Join(base, "agentapi-native-*", "config.json")
}

// nativeDiscoverInstances enumerates installed native ESM instances on this
// host by scanning the well-known config directories. Missing or unreadable
// configs are skipped so a partially uninstalled host still lists the rest.
func nativeDiscoverInstances() []nativeInstanceListEntry {
	defaultConfig, pattern := nativeInstanceBaseDirs()
	return discoverNativeInstances(defaultConfig, pattern)
}

func discoverNativeInstances(defaultConfig, pattern string) []nativeInstanceListEntry {
	entries := make([]nativeInstanceListEntry, 0)
	seen := make(map[string]bool)
	add := func(name, configPath string) {
		if name == "" || seen[configPath] || configPath == "" {
			return
		}
		seen[configPath] = true
		cfg, err := readNativeConfig(configPath)
		if err != nil {
			return
		}
		entries = append(entries, nativeInstanceListEntry{
			Instance: name, Service: nativeServiceName(name), ManagerID: cfg.ManagerID,
			Upstream: cfg.UpstreamURL, State: cfg.StateDir,
			BinaryPath: cfg.BinaryPath, Config: configPath, Running: nativeServiceRunning(name),
		})
	}
	add(nativeDefaultInstance, defaultConfig)
	if pattern != "" {
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			name := strings.TrimPrefix(filepath.Base(filepath.Dir(match)), "agentapi-native-")
			add(name, match)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Instance == nativeDefaultInstance && entries[j].Instance != nativeDefaultInstance {
			return true
		}
		if entries[j].Instance == nativeDefaultInstance {
			return false
		}
		return entries[i].Instance < entries[j].Instance
	})
	return entries
}
