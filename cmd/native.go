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
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var NativeCmd = &cobra.Command{Use: "native", Short: "Install and manage a native External Session Manager"}

type nativeManageOptions struct {
	upstream, publicURL, name, listen, managerID, apiKeyEnv, apiKeyFile, configPath          string
	scope, teamID, drainTimeout                                                              string
	labels                                                                                   []string
	defaultManager, apiKeyStdin, force, drain, keepRegistration, keepData, filesystemSandbox bool
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
	p.StringVar(&nativeManageOpts.apiKeyEnv, "api-key-env", "AGENTAPI_KEY", "environment variable containing the install API key")
	p.StringVar(&nativeManageOpts.apiKeyFile, "api-key-file", "", "file containing the install API key")
	p.BoolVar(&nativeManageOpts.apiKeyStdin, "api-key-stdin", false, "read the install API key from stdin")

	install := &cobra.Command{Use: "install", Short: "Register and install the native ESM daemon", RunE: runNativeInstall}
	f := install.Flags()
	f.StringVar(&nativeManageOpts.upstream, "upstream", "", "parent agentapi-proxy URL")
	f.StringVar(&nativeManageOpts.publicURL, "public-url", "", "parent-reachable URL for this host")
	f.StringVar(&nativeManageOpts.name, "name", "", "human-readable manager name")
	f.StringVar(&nativeManageOpts.listen, "listen", ":8080", "native ESM listen address")
	f.StringVar(&nativeManageOpts.managerID, "manager-id", "", "stable manager ID (also migrates an existing registration)")
	f.StringVar(&nativeManageOpts.scope, "scope", "user", "registration scope: user or team")
	f.StringVar(&nativeManageOpts.teamID, "team-id", "", "team ID when --scope=team")
	f.StringSliceVar(&nativeManageOpts.labels, "label", nil, "allocator label in key=value form")
	f.BoolVar(&nativeManageOpts.defaultManager, "default", false, "make this the default external session manager")
	f.BoolVar(&nativeManageOpts.force, "force", false, "install even if the existing state directory contains sessions")
	f.BoolVar(&nativeManageOpts.filesystemSandbox, "filesystem-sandbox", false, "sandbox native session filesystem access on macOS")

	status := &cobra.Command{Use: "status", Short: "Show daemon and connection status", RunE: runNativeStatus}
	doctor := &cobra.Command{Use: "doctor", Short: "Validate daemon configuration and connectivity", RunE: runNativeDoctor}
	restart := &cobra.Command{Use: "restart", Short: "Restart the native ESM daemon", RunE: runNativeRestart}
	rotate := &cobra.Command{Use: "rotate-token", Short: "Rotate the ESM connection token and restart", RunE: runNativeRotateToken}
	uninstall := &cobra.Command{Use: "uninstall", Short: "Stop and remove the native ESM daemon", RunE: runNativeUninstall}
	uninstall.Flags().BoolVar(&nativeManageOpts.force, "force", false, "terminate active sessions")
	uninstall.Flags().BoolVar(&nativeManageOpts.drain, "drain", false, "wait for active sessions to finish")
	uninstall.Flags().StringVar(&nativeManageOpts.drainTimeout, "drain-timeout", "30m", "maximum time to wait with --drain")
	uninstall.Flags().BoolVar(&nativeManageOpts.keepRegistration, "keep-registration", false, "keep the parent registration")
	uninstall.Flags().BoolVar(&nativeManageOpts.keepData, "keep-data", false, "keep daemon state and configuration")
	NativeCmd.AddCommand(install, status, doctor, restart, rotate, uninstall)
}

func runNativeInstall(_ *cobra.Command, _ []string) error {
	if nativeManageOpts.upstream == "" || nativeManageOpts.publicURL == "" {
		return errors.New("--upstream and --public-url are required")
	}
	if nativeManageOpts.filesystemSandbox && runtime.GOOS != "darwin" {
		return errors.New("--filesystem-sandbox is only supported on macOS")
	}
	hostname, _ := os.Hostname()
	if nativeManageOpts.name == "" {
		nativeManageOpts.name = hostname
	}
	paths, err := nativePaths(nativeManageOpts.configPath)
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
		instanceID = nativeManageOpts.managerID
	}
	if instanceID == "" {
		instanceID = stableNativeInstanceID(hostname)
	}
	labels := map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH, "hostname": hostname}
	for _, raw := range nativeManageOpts.labels {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return fmt.Errorf("invalid --label %q; expected key=value", raw)
		}
		labels[strings.TrimSpace(parts[0])] = parts[1]
	}
	apiKey, err := readInstallAPIKey()
	if err != nil {
		return err
	}
	registration, err := registerNativeManager(nativeManageOpts.upstream, apiKey, map[string]interface{}{
		"manager_id": nativeManageOpts.managerID, "instance_id": instanceID, "name": nativeManageOpts.name,
		"scope": nativeManageOpts.scope, "team_id": nativeManageOpts.teamID,
		"labels": labels, "default": nativeManageOpts.defaultManager, "public_url": nativeManageOpts.publicURL,
		"version": nativeBuildVersion(), "rotate_token": existing.ConnectionToken == "",
	})
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
		ConnectionToken: token, PublicURL: strings.TrimRight(nativeManageOpts.publicURL, "/"), StateDir: paths.state,
		BinaryPath: paths.binary, ManagerID: registration.ID, InstanceID: instanceID, Scope: nativeManageOpts.scope, TeamID: nativeManageOpts.teamID,
		Labels: labels, Version: nativeBuildVersion(),
		FilesystemSandbox: nativeFilesystemSandboxConfig{Enabled: nativeManageOpts.filesystemSandbox}}
	if err := installNativeService(paths, cfg); err != nil {
		return err
	}
	if err := waitNativeHealth(cfg.Listen, 30*time.Second); err != nil {
		return err
	}
	if err := sendNativeHeartbeat(cfg); err != nil {
		return fmt.Errorf("daemon installed but parent heartbeat failed: %w", err)
	}
	fmt.Printf("Native ESM installed\nManager ID: %s\nService: %s\nLabels: %s\n", registration.ID, nativeServiceName(), formatLabels(labels))
	return nil
}

type nativeInstallPaths struct{ config, credentials, state, binary, service, logDir string }

func nativePaths(configOverride string) (nativeInstallPaths, error) {
	if runtime.GOOS == "linux" {
		if os.Geteuid() != 0 && configOverride == "" {
			return nativeInstallPaths{}, errors.New("native install on Linux must run as root (use sudo)")
		}
		config := "/etc/agentapi-native/config.json"
		if configOverride != "" {
			config = configOverride
		}
		return nativeInstallPaths{config: config, credentials: "/etc/agentapi-native/credentials.json", state: "/var/lib/agentapi-native", binary: "/usr/local/libexec/agentapi-proxy/agentapi-proxy", service: "/etc/systemd/system/agentapi-native.service", logDir: "/var/log/agentapi-native"}, nil
	}
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nativeInstallPaths{}, err
		}
		base := filepath.Join(home, "Library", "Application Support", "agentapi-native")
		config := filepath.Join(base, "config.json")
		if configOverride != "" {
			config = configOverride
		}
		return nativeInstallPaths{config: config, credentials: filepath.Join(base, "credentials.json"), state: filepath.Join(base, "state"), binary: filepath.Join(base, "bin", "agentapi-proxy"), service: filepath.Join(home, "Library", "LaunchAgents", "com.agentapi.native.plist"), logDir: filepath.Join(home, "Library", "Logs", "agentapi-native")}, nil
	}
	return nativeInstallPaths{}, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
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
		unit := fmt.Sprintf("[Unit]\nDescription=agentapi-proxy native external session manager\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nUser=agentapi\nGroup=agentapi\nExecStart=%s native-session-manager --config %s\nRestart=always\nRestartSec=3\nKillMode=process\nTimeoutStopSec=30\nLimitNOFILE=65536\n\n[Install]\nWantedBy=multi-user.target\n", paths.binary, paths.config)
		if err := atomicWriteFile(paths.service, []byte(unit), 0o644); err != nil {
			return err
		}
		if err := runCommand("systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := runCommand("systemctl", "enable", "agentapi-native.service"); err != nil {
			return err
		}
		return runCommand("systemctl", "restart", "agentapi-native.service")
	}
	plist := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict><key>Label</key><string>com.agentapi.native</string><key>ProgramArguments</key><array><string>%s</string><string>native-session-manager</string><string>--config</string><string>%s</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>StandardOutPath</key><string>%s/native.log</string><key>StandardErrorPath</key><string>%s/native.log</string></dict></plist>\n", xmlEscape(paths.binary), xmlEscape(paths.config), xmlEscape(paths.logDir), xmlEscape(paths.logDir))
	if err := atomicWriteFile(paths.service, []byte(plist), 0o600); err != nil {
		return err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = runCommand("launchctl", "bootout", domain+"/com.agentapi.native")
	return runCommand("launchctl", "bootstrap", domain, paths.service)
}

func runNativeStatus(_ *cobra.Command, _ []string) error {
	paths, err := nativePaths(nativeManageOpts.configPath)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		return err
	}
	service := "stopped"
	if nativeServiceRunning() {
		service = "running"
	}
	health := "unreachable"
	if nativeHealth(cfg.Listen) == nil {
		health = "ok"
	}
	active, _ := filepath.Glob(filepath.Join(cfg.StateDir, "sessions", "*"))
	fmt.Printf("Service: %s\nManager ID: %s\nUpstream: %s\nPublic URL: %s\nLabels: %s\nVersion: %s\nFilesystem sandbox: %t\nActive sessions: %d\nHealth: %s\nState: %s\n", service, cfg.ManagerID, cfg.UpstreamURL, cfg.PublicURL, formatLabels(cfg.Labels), cfg.Version, cfg.FilesystemSandbox.Enabled, len(active), health, cfg.StateDir)
	return nil
}

func runNativeDoctor(_ *cobra.Command, _ []string) error {
	paths, err := nativePaths(nativeManageOpts.configPath)
	if err != nil {
		return err
	}
	for name, path := range map[string]string{"config": paths.config, "credentials": paths.credentials} {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if info.Mode().Perm()&0o037 != 0 {
			return fmt.Errorf("%s permissions are too open: %o", name, info.Mode().Perm())
		}
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		return err
	}
	if cfg.FilesystemSandbox.Enabled {
		if runtime.GOOS != "darwin" {
			return errors.New("filesystem sandbox is enabled but this host is not macOS")
		}
		if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
			return fmt.Errorf("filesystem sandbox executable: %w", err)
		}
	}
	if err := nativeHealth(cfg.Listen); err != nil {
		return fmt.Errorf("local health: %w", err)
	}
	if err := sendNativeHeartbeat(cfg); err != nil {
		return fmt.Errorf("parent heartbeat: %w", err)
	}
	fmt.Println("OK: service, config permissions, local health, and parent heartbeat")
	return nil
}

func runNativeRestart(_ *cobra.Command, _ []string) error { return restartNativeService() }

func runNativeRotateToken(_ *cobra.Command, _ []string) error {
	paths, err := nativePaths(nativeManageOpts.configPath)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		return err
	}
	apiKey, err := readInstallAPIKey()
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
	if err := restartNativeService(); err != nil {
		return err
	}
	fmt.Println("Connection token rotated and daemon restarted")
	return nil
}

func runNativeUninstall(_ *cobra.Command, _ []string) error {
	paths, err := nativePaths(nativeManageOpts.configPath)
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
		_ = runCommand("systemctl", "disable", "--now", "agentapi-native.service")
		_ = os.Remove(paths.service)
		_ = runCommand("systemctl", "daemon-reload")
	} else {
		_ = runCommand("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/com.agentapi.native")
		_ = os.Remove(paths.service)
	}
	if !nativeManageOpts.keepRegistration && cfg.ManagerID != "" {
		apiKey, keyErr := readInstallAPIKey()
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
		_ = os.Remove(paths.binary)
		_ = os.Remove(paths.config)
		_ = os.Remove(paths.credentials)
		if safeNativeStateDir(cfg.StateDir) {
			_ = os.RemoveAll(cfg.StateDir)
		}
	}
	fmt.Println("Native ESM uninstalled")
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

func registerNativeManager(upstream, apiKey string, payload interface{}) (*nativeRegistrationResponse, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(upstream, "/")+"/external-session-managers", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
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
	body, _ := json.Marshal(map[string]interface{}{"public_url": cfg.PublicURL, "version": nativeBuildVersion()})
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

func readInstallAPIKey() (string, error) {
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
		return "", errors.New("install API key is required; use --api-key-stdin, --api-key-file, or --api-key-env")
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
func stableNativeInstanceID(hostname string) string {
	source := hostname
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
func nativeServiceName() string {
	if runtime.GOOS == "linux" {
		return "agentapi-native.service"
	}
	return "com.agentapi.native"
}
func nativeServiceRunning() bool {
	var command *exec.Cmd
	if runtime.GOOS == "linux" {
		command = exec.Command("systemctl", "is-active", "--quiet", "agentapi-native.service")
	} else {
		command = exec.Command("launchctl", "print", "gui/"+strconv.Itoa(os.Getuid())+"/com.agentapi.native")
	}
	return command.Run() == nil
}
func restartNativeService() error {
	if runtime.GOOS == "linux" {
		return runCommand("systemctl", "restart", "agentapi-native.service")
	}
	paths, err := nativePaths(nativeManageOpts.configPath)
	if err != nil {
		return err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = runCommand("launchctl", "bootout", domain+"/com.agentapi.native")
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
