package provisioner

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	provisionTempSettings = "/tmp/provision-settings.yaml"
	sessionEnvFile        = "/home/agentapi/.session/env"
	claudeMDPath          = "/home/agentapi/.claude/CLAUDE.md"
	workdirRepoPath       = "/home/agentapi/workdir/repo"
	// webhookPayloadPath is the well-known path where the webhook payload JSON
	// is exposed inside the session container.
	// For non-stock sessions this file is provided by a read-only Kubernetes
	// Secret volume mount.  For stock sessions (pre-warmed pods) the volume is
	// absent; the provisioner writes the file itself from SessionSettings so
	// that both paths behave identically.
	webhookPayloadPath = "/opt/webhook/payload.json"
)

// runProvision executes the full provisioning sequence and then supervises
// the agentapi subprocess.
//
// Sequence:
//  1. Write received settings to a temp YAML file
//  2. Run sessionsettings.Setup() (write-pem, clone-repo, compile, sync-extra)
//  3. Load the generated session env file
//  4. Fetch memory from the proxy and inject into CLAUDE.md
//  5. cd into the cloned repo if present
//  6. Start agentapi (or claude-agentapi / codex-agentapi) as a subprocess
//  7. Wait for agentapi to become ready
//  8. Send the initial message if specified in settings
//  9. Set status to "ready"; supervise the subprocess
func (s *Server) runProvision(ctx context.Context, settings *sessionsettings.SessionSettings) {
	log.Printf("[PROVISIONER] Starting provisioning for session %s", settings.Session.ID)

	// ── Step 1: write settings to temp YAML ──────────────────────────────────
	data, err := sessionsettings.MarshalYAML(settings)
	if err != nil {
		s.setStatus(StatusError, fmt.Sprintf("failed to marshal settings: %v", err))
		return
	}
	if err := os.WriteFile(provisionTempSettings, data, 0o600); err != nil {
		s.setStatus(StatusError, fmt.Sprintf("failed to write temp settings: %v", err))
		return
	}

	// ── Step 1.5: write webhook payload file ─────────────────────────────────
	// For stock sessions the pod is pre-created without a webhook-payload
	// Secret volume (payload unknown at pod creation time).  We write the
	// payload to the well-known path here so that /opt/webhook/payload.json is
	// available to the agent regardless of whether the session was fulfilled
	// from the stock inventory or from a freshly created pod.
	// For non-stock sessions the file already exists (read-only Secret volume
	// mount), so writeWebhookPayloadFile is a no-op in that case.
	if settings.WebhookPayload != "" {
		writeWebhookPayloadFile(settings.WebhookPayload)
	}

	// ── Step 2: run setup ─────────────────────────────────────────────────────
	// Override CompileOptions.InputPath to use the temp file written above,
	// not the default /session-settings/settings.yaml (which is no longer mounted).
	compileOpts := sessionsettings.DefaultCompileOptions()
	compileOpts.InputPath = provisionTempSettings

	opts := sessionsettings.SetupOptions{
		InputPath:                 provisionTempSettings,
		CompileOptions:            compileOpts,
		NotificationSubscriptions: "/notification-subscriptions-source",
		NotificationsDir:          "/home/agentapi/notifications",
		RegisterMarketplaces:      true,
	}
	log.Printf("[PROVISIONER] Running session setup")
	if err := sessionsettings.Setup(opts); err != nil {
		s.setStatus(StatusError, fmt.Sprintf("setup failed: %v", err))
		return
	}
	log.Printf("[PROVISIONER] Session setup complete")

	// ── Step 2.5: restore managed files from provision payload ───────────────
	// Files are embedded in SessionSettings.Files by the proxy at session creation
	// time (read from agentapi-agent-files-{userID} Secret).
	// Write each file to its path so agents can use them immediately.
	if len(settings.Files) > 0 {
		if err := writeFiles(settings.Files); err != nil {
			log.Printf("[PROVISIONER] Warning: failed to write managed files: %v", err)
		}
	} else if settings.Credentials != "" {
		// Backward compat: old sessions only have Credentials (= ~/.codex/auth.json).
		if err := writeCredentials(settings.Credentials); err != nil {
			log.Printf("[PROVISIONER] Warning: failed to write credentials (legacy): %v", err)
		} else {
			log.Printf("[PROVISIONER] Restored credentials to ~/.codex/auth.json (legacy)")
		}
	}

	// ── Step 3: load session env file ─────────────────────────────────────────
	envMap := loadEnvFile(sessionEnvFile)
	log.Printf("[PROVISIONER] Loaded %d env vars from session env file", len(envMap))

	// ── Step 4: fetch memory from proxy → inject into CLAUDE.md ──────────────
	s.fetchAndInjectMemory()

	// ── Step 5: cd into cloned repo ───────────────────────────────────────────
	if _, err := os.Stat(workdirRepoPath); err == nil {
		log.Printf("[PROVISIONER] Changing to repo directory %s", workdirRepoPath)
		if err := os.Chdir(workdirRepoPath); err != nil {
			log.Printf("[PROVISIONER] Warning: failed to chdir to %s: %v", workdirRepoPath, err)
		}
	}

	// ── Step 6: start otelcol subprocess if in-process mode ──────────────────
	// Must be started before agentapi so metrics scraping begins as soon as
	// Claude Code starts emitting metrics on its prometheus port.
	if settings.OtelCollector != nil && settings.OtelCollector.Enabled {
		go s.runOtelcol(ctx, settings.OtelCollector)
	}

	// ── Step 7: build and start the agent subprocess ──────────────────────────
	agentCmd, agentArgs := s.buildAgentCommand(settings, envMap)
	log.Printf("[PROVISIONER] Starting agent: %s %v", agentCmd, agentArgs)

	// Use an independent context so that the server context cancellation (SIGTERM)
	// does NOT immediately kill agentapi.  The shutdown goroutine in server.go
	// saves session memory first, then calls agentKillFn() to stop the agent.
	agentCtx, agentCancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(agentCtx, agentCmd, agentArgs...)
	cmd.Env = mergeEnv(os.Environ(), envMap)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		agentCancel()
		s.setStatus(StatusError, fmt.Sprintf("failed to start agent: %v", err))
		return
	}
	log.Printf("[PROVISIONER] Agent process started (pid %d)", cmd.Process.Pid)

	// Register kill function so the shutdown goroutine can stop the agent
	// after saving session memory.
	s.mu.Lock()
	s.agentKillFn = agentCancel
	s.mu.Unlock()

	// ── Step 8: wait for agentapi to be ready ─────────────────────────────────
	agentapiPort := os.Getenv("AGENTAPI_PORT")
	if agentapiPort == "" {
		agentapiPort = "8080"
	}
	agentapiURL := fmt.Sprintf("http://localhost:%s", agentapiPort)

	log.Printf("[PROVISIONER] Waiting for agentapi to be ready at %s", agentapiURL)
	if err := waitForAgentAPI(ctx, agentapiURL, 120); err != nil {
		s.setStatus(StatusError, fmt.Sprintf("agentapi not ready: %v", err))
		_ = cmd.Process.Kill()
		return
	}
	log.Printf("[PROVISIONER] agentapi is ready")

	// ── Step 9: send initial message ─────────────────────────────────────────
	if settings.InitialMessage != "" {
		log.Printf("[PROVISIONER] Sending initial message")
		agentType := settings.Session.AgentType
		waitSec := 2
		if v := os.Getenv("INITIAL_MESSAGE_WAIT_SECOND"); v != "" {
			if _, err := fmt.Sscanf(v, "%d", &waitSec); err != nil {
				log.Printf("[PROVISIONER] Warning: invalid INITIAL_MESSAGE_WAIT_SECOND=%q: %v", v, err)
			}
		}
		sendInitialMessage(ctx, agentapiURL, settings.InitialMessage, agentType, waitSec)
	}

	// ── Step 10: mark ready and supervise ────────────────────────────────────
	s.setStatus(StatusReady, "")

	// ── Step 11: launch claude-posts subprocess if SlackParams provided ───────
	if settings.SlackParams != nil && settings.SlackParams.Channel != "" {
		go s.runClaudePosts(ctx, settings.SlackParams)
	}

	// ── Step 12: start files sync goroutine ───────────────────────────────────
	// Syncs managedFilePaths → Kubernetes Secret agentapi-agent-files-{userID}.
	// Runs in-process instead of as a sidecar so that UserID is always set
	// (stock pool pods have empty UserID at pod creation time).
	go s.runFilesSync(ctx, settings.Session.UserID)

	// Supervise: if agentapi exits, report error so K8s restarts the Pod.
	go func() {
		if err := cmd.Wait(); err != nil {
			s.setStatus(StatusError, fmt.Sprintf("agent process exited: %v", err))
		} else {
			s.setStatus(StatusError, "agent process exited with code 0")
		}
	}()
}

// saveSessionMemory runs `agentapi-proxy client memory save-session` to persist
// the current session's conversation to the memory backend.
// AGENTAPI_KEY is read from the session env file if not already set in the
// process environment (session pods write the personal API key there).
func saveSessionMemory() {
	// Use a fresh context so that the save is not cancelled by the already-done
	// parent context.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "agentapi-proxy", "client", "memory", "save-session")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Build environment: start from current process env, then overlay session
	// env file values so that AGENTAPI_KEY (and any other keys written there)
	// are available even when the pod's container env doesn't have them set.
	env := os.Environ()
	if envMap := loadEnvFile(sessionEnvFile); len(envMap) > 0 {
		env = mergeEnv(env, envMap)
		log.Printf("[PROVISIONER] Loaded %d env vars from %s for memory save", len(envMap), sessionEnvFile)
	}
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		log.Printf("[PROVISIONER] Warning: memory save-session failed: %v", err)
	}
}

// runClaudePosts starts the claude-posts binary as a subprocess, forwarding
// agent output (history.jsonl) to Slack. It waits for the history file to
// appear before launching, mirroring the sidecar's shell loop.
// The subprocess is tied to ctx: when ctx is cancelled the goroutine exits.
func (s *Server) runClaudePosts(ctx context.Context, params *sessionsettings.SlackParams) {
	const historyFile = "/opt/claude-agentapi/history.jsonl"
	const claudePostsBin = "/usr/local/bin/claude-posts"

	log.Printf("[CLAUDE_POSTS] Waiting for history file %s", historyFile)
	for {
		select {
		case <-ctx.Done():
			log.Printf("[CLAUDE_POSTS] Context cancelled while waiting for history file")
			return
		case <-time.After(time.Second):
		}
		if _, err := os.Stat(historyFile); err == nil {
			break
		}
	}
	log.Printf("[CLAUDE_POSTS] History file found, starting claude-posts")

	cmd := exec.CommandContext(ctx, claudePostsBin, "--file", historyFile)
	cmd.Env = append(os.Environ(),
		"SLACK_BOT_TOKEN="+params.BotToken,
		"SLACK_CHANNEL_ID="+params.Channel,
		"SLACK_THREAD_TS="+params.ThreadTS,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// ctx cancellation causes an expected error; don't log it as fatal.
		if ctx.Err() != nil {
			log.Printf("[CLAUDE_POSTS] Exited due to context cancellation")
		} else {
			log.Printf("[CLAUDE_POSTS] Exited with error: %v", err)
		}
	} else {
		log.Printf("[CLAUDE_POSTS] Exited normally")
	}
}

// managedFilePaths lists the absolute paths that runFilesSync watches and persists
// to the agentapi-agent-files-{userID} Secret.  Order determines the index used as
// the Secret key prefix (0, 1, …).
var managedFilePaths = []string{
	"/home/agentapi/.codex/auth.json",
	"/home/agentapi/.claude/.credentials.json",
}

// writeFiles writes each ManagedFile to its path (mode 0600), creating parent
// directories as needed.  It is called once at provision time to restore files
// delivered via the SessionSettings payload.
func writeFiles(files []sessionsettings.ManagedFile) error {
	var firstErr error
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f.Path), 0755); err != nil {
			log.Printf("[PROVISIONER] Warning: failed to create dir for %s: %v", f.Path, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := os.WriteFile(f.Path, []byte(f.Content), 0600); err != nil {
			log.Printf("[PROVISIONER] Warning: failed to write %s: %v", f.Path, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		log.Printf("[PROVISIONER] Restored managed file: %s", f.Path)
	}
	return firstErr
}

// writeCredentials writes the given JSON string to ~/.codex/auth.json (mode 0600).
// Deprecated: used only for backward compatibility with old SessionSettings that
// do not have the Files field.  New sessions use writeFiles instead.
func writeCredentials(credentialsJSON string) error {
	codexDir := filepath.Join(os.Getenv("HOME"), ".codex")
	if codexDir == "/.codex" {
		codexDir = "/home/agentapi/.codex"
	}
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return fmt.Errorf("failed to create .codex directory: %w", err)
	}
	destPath := filepath.Join(codexDir, "auth.json")
	if err := os.WriteFile(destPath, []byte(credentialsJSON), 0600); err != nil {
		return fmt.Errorf("failed to write auth.json: %w", err)
	}
	return nil
}

// runFilesSync watches the paths listed in managedFilePaths for changes and syncs
// all of them to the Kubernetes Secret agentapi-agent-files-{userID} using the
// in-cluster k8s client.
// The goroutine is tied to ctx: when ctx is cancelled the loop exits.
func (s *Server) runFilesSync(ctx context.Context, userID string) {
	const syncInterval = 10 * time.Second

	if userID == "" {
		log.Printf("[FILES_SYNC] No user ID set, skipping files sync")
		return
	}

	secretName := "agentapi-agent-files-" + sanitizeCredentialSecretName(userID)

	// Read namespace from the in-cluster service-account namespace file.
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		log.Printf("[FILES_SYNC] Not running in a k8s cluster (namespace file not found): %v, skipping", err)
		return
	}
	namespace := strings.TrimSpace(string(nsBytes))

	log.Printf("[FILES_SYNC] Starting: watching %v -> Secret %s/%s (interval: %s)", managedFilePaths, namespace, secretName, syncInterval)

	k8sCfg, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("[FILES_SYNC] Failed to get in-cluster config: %v, skipping", err)
		return
	}
	clientset, err := kubernetes.NewForConfig(k8sCfg)
	if err != nil {
		log.Printf("[FILES_SYNC] Failed to create k8s client: %v, skipping", err)
		return
	}

	// Track last hash per file path to detect changes.
	lastHashes := make(map[string]string, len(managedFilePaths))

	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[FILES_SYNC] Context cancelled, stopping")
			return
		case <-ticker.C:
			// Read each file exactly once to avoid TOCTOU races between the
			// hash-check pass and the content-collection pass.
			type fileSnapshot struct {
				data []byte
				hash string
			}
			snapshots := make(map[string]fileSnapshot, len(managedFilePaths))
			for _, path := range managedFilePaths {
				data, hash, err := readFileWithHash(path)
				if err != nil {
					// File may not exist yet; not an error.
					continue
				}
				snapshots[path] = fileSnapshot{data: data, hash: hash}
			}

			changed := false
			for _, path := range managedFilePaths {
				snap, ok := snapshots[path]
				if !ok {
					continue
				}
				if snap.hash != lastHashes[path] {
					changed = true
				}
			}
			if !changed {
				continue
			}

			// Build the full snapshot of all managed files from the single read above.
			files := make([]sessionsettings.ManagedFile, 0, len(snapshots))
			for _, path := range managedFilePaths {
				snap, ok := snapshots[path]
				if !ok {
					continue
				}
				files = append(files, sessionsettings.ManagedFile{
					Path:    path,
					Content: string(snap.data),
				})
			}

			secretData := sessionsettings.FilesToSecretData(files)
			log.Printf("[FILES_SYNC] Files changed, syncing %d file(s) to Secret %s", len(files), secretName)
			if err := upsertFilesSecret(ctx, clientset, namespace, secretName, secretData); err != nil {
				log.Printf("[FILES_SYNC] ERROR: failed to upsert Secret: %v", err)
				// Do not update lastHashes so the next tick retries the sync.
			} else {
				log.Printf("[FILES_SYNC] Successfully synced to Secret %s", secretName)
				// Update hashes only after a successful sync so failed upserts are retried.
				for _, path := range managedFilePaths {
					if snap, ok := snapshots[path]; ok {
						lastHashes[path] = snap.hash
					}
				}
			}
		}
	}
}

// readFileWithHash reads a file and returns its content and hex-encoded SHA256 hash.
func readFileWithHash(path string) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	return data, fmt.Sprintf("%x", sum), nil
}

// upsertFilesSecret creates or updates the agentapi-agent-files-{userID} Kubernetes
// Secret with the provided index-based file data produced by FilesToSecretData.
func upsertFilesSecret(ctx context.Context, clientset kubernetes.Interface, namespace, name string, data map[string][]byte) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "agentapi-agent-files",
				"app.kubernetes.io/managed-by": "agentapi-proxy",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}

	_, err := clientset.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if k8serrors.IsAlreadyExists(err) {
		_, err = clientset.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
		return err
	}
	return err
}

// sanitizeCredentialSecretName converts a userID into a valid Kubernetes Secret
// name component (lowercase alphanumeric and dashes only, max 50 chars).
func sanitizeCredentialSecretName(s string) string {
	s = strings.ToLower(s)
	re := regexp.MustCompile(`[^a-z0-9-]`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = s[:50]
	}
	return s
}

// runOtelcol starts the OpenTelemetry Collector binary as a subprocess.
// It is used when otelcol runs in in-process mode (OtelCollectorInProcess=true)
// instead of as a Kubernetes sidecar. This allows otelcol to be started
// after user context is established, so metrics labels (user_id, session_id,
// etc.) are correctly set even when using the stock inventory feature.
// The subprocess is tied to ctx: when ctx is cancelled the goroutine exits.
func (s *Server) runOtelcol(ctx context.Context, cfg *sessionsettings.OtelCollectorConfig) {
	const otelcolBin = "/usr/local/bin/otelcol"
	const configPath = "/tmp/otelcol-config.yaml"

	scrapeInterval := cfg.ScrapeInterval
	if scrapeInterval == "" {
		scrapeInterval = "15s"
	}
	claudeCodePort := cfg.ClaudeCodePort
	if claudeCodePort == 0 {
		claudeCodePort = 9464
	}
	exporterPort := cfg.ExporterPort
	if exporterPort == 0 {
		exporterPort = 9090
	}

	// Generate otelcol config with label values already substituted.
	// Using Go fmt.Sprintf rather than otelcol ${env:VAR} expansion so that
	// the correct values are used regardless of the container's own env vars
	// (which may be unset in stock sessions at startup time).
	otelConfig := fmt.Sprintf(`receivers:
  prometheus:
    config:
      scrape_configs:
        - job_name: 'claude-code'
          scrape_interval: %s
          static_configs:
            - targets: ['localhost:%d']

processors:
  resource:
    attributes:
      - key: user_id
        action: delete
      - key: session_id
        action: delete
  transform:
    error_mode: ignore
    metric_statements:
      - context: datapoint
        statements:
          # Rename claude-code's native labels
          - set(attributes["claude_user_id"], attributes["user_id"]) where attributes["user_id"] != nil
          - set(attributes["claude_session_id"], attributes["session_id"]) where attributes["session_id"] != nil
          - delete_key(attributes, "user_id")
          - delete_key(attributes, "session_id")
          # Remove user_email label to prevent it from being scraped by Prometheus
          - delete_key(attributes, "user_email")
          # Add agentapi labels
          - set(attributes["agentapi_session_id"], "%s")
          - set(attributes["agentapi_user_id"], "%s")
          - set(attributes["agentapi_team_id"], "%s")
          - set(attributes["agentapi_schedule_id"], "%s")
          - set(attributes["agentapi_webhook_id"], "%s")
          - set(attributes["agentapi_agent_type"], "%s")

exporters:
  prometheus:
    endpoint: "0.0.0.0:%d"
    resource_to_telemetry_conversion:
      enabled: false

service:
  pipelines:
    metrics:
      receivers: [prometheus]
      processors: [resource, transform]
      exporters: [prometheus]`,
		scrapeInterval, claudeCodePort,
		cfg.SessionID, cfg.UserID, cfg.TeamID,
		cfg.ScheduleID, cfg.WebhookID, cfg.AgentType,
		exporterPort)

	if err := os.WriteFile(configPath, []byte(otelConfig), 0o600); err != nil {
		log.Printf("[OTELCOL] Failed to write config to %s: %v", configPath, err)
		return
	}
	log.Printf("[OTELCOL] Config written to %s, starting otelcol subprocess", configPath)

	cmd := exec.CommandContext(ctx, otelcolBin, "--config="+configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			log.Printf("[OTELCOL] Exited due to context cancellation")
		} else {
			log.Printf("[OTELCOL] Exited with error: %v", err)
		}
	} else {
		log.Printf("[OTELCOL] Exited normally")
	}
}

// buildAgentCommand returns the executable and arguments for the agent
// process, mirroring the logic in buildClaudeStartCommand().
func (s *Server) buildAgentCommand(settings *sessionsettings.SessionSettings, envMap map[string]string) (string, []string) {
	agentType := settings.Session.AgentType

	agentapiPort := os.Getenv("AGENTAPI_PORT")
	if agentapiPort == "" {
		agentapiPort = "8080"
	}

	switch agentType {
	case "claude-agentapi":
		args := []string{"--output-file", "/opt/claude-agentapi/history.jsonl"}
		if claudeArgs := os.Getenv("CLAUDE_ARGS"); claudeArgs != "" {
			args = append(args, strings.Fields(claudeArgs)...)
		}
		return "claude-agentapi", args

	case "codex-agentapi":
		return "bunx", []string{"@takutakahashi/codex-agentapi"}

	default:
		// Default: agentapi server wrapping claude
		claudeCmd := "claude"
		if claudeArgs := os.Getenv("CLAUDE_ARGS"); claudeArgs != "" {
			claudeCmd = claudeCmd + " " + claudeArgs
		}
		return "agentapi", []string{
			"server",
			"--allowed-hosts", "*",
			"--allowed-origins", "*",
			"--port", agentapiPort,
			"--",
			"sh", "-c", claudeCmd,
		}
	}
}

// waitForAgentAPI polls agentapiURL/status until it responds 200 or the
// context is cancelled.  maxRetries × 0.5 s = total wait time.
func waitForAgentAPI(ctx context.Context, agentapiURL string, maxRetries int) error {
	client := &http.Client{Timeout: 3 * time.Second}
	statusURL := agentapiURL + "/status"

	for i := 0; i < maxRetries; i++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled")
		default:
		}

		resp, err := client.Get(statusURL)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("agentapi not ready after %d retries", maxRetries)
}

// agentStatusResponse is the minimal shape of agentapi's /status response.
type agentStatusResponse struct {
	Status string `json:"status"`
}

// agentMessagesResponse is the minimal shape of agentapi's /messages response.
type agentMessagesResponse struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// sendInitialMessage sends an initial message to agentapi after it has
// started, replicating the logic of initialMessageSenderScript.
func sendInitialMessage(ctx context.Context, agentapiURL, message, agentType string, waitSec int) {
	client := &http.Client{Timeout: 5 * time.Second}

	// Check for existing user messages (idempotency / Pod restart).
	if count := countUserMessages(client, agentapiURL); count > 0 {
		log.Printf("[PROVISIONER] User messages already exist (%d), skipping initial message", count)
		return
	}

	// Wait for Claude to be ready (strategy depends on agentType).
	if agentType == "" {
		// Default agentapi: wait for running→stable transition OR stable + non-empty message.
		waitForDefaultAgentReady(ctx, client, agentapiURL)
	} else {
		// claude-agentapi / codex-agentapi: just wait for stable.
		waitForStable(ctx, client, agentapiURL, 60)
	}

	// Configured delay before sending.
	log.Printf("[PROVISIONER] Waiting %d second(s) before sending initial message", waitSec)
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Duration(waitSec) * time.Second):
	}

	// Double-check race condition.
	if count := countUserMessages(client, agentapiURL); count > 0 {
		log.Printf("[PROVISIONER] User messages appeared during wait (%d), skipping", count)
		return
	}

	// Send.
	payload := map[string]string{"content": message, "type": "user"}
	body, _ := json.Marshal(payload)

	for attempt := 1; attempt <= 5; attempt++ {
		log.Printf("[PROVISIONER] Initial message send attempt %d/5", attempt)

		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, agentapiURL+"/message", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				log.Printf("[PROVISIONER] Initial message sent successfully")
				return
			}
			log.Printf("[PROVISIONER] Send failed (HTTP %d), attempt %d/5", resp.StatusCode, attempt)
		} else {
			log.Printf("[PROVISIONER] Send error: %v, attempt %d/5", err, attempt)
		}

		if attempt < 5 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
	}
	log.Printf("[PROVISIONER] ERROR: Failed to send initial message after 5 attempts")
}

// waitForDefaultAgentReady uses the two-phase strategy from the original shell
// script: first tries to catch "running→stable" within 10 s, then falls back
// to "stable + non-empty message" for up to 60 s.
func waitForDefaultAgentReady(ctx context.Context, client *http.Client, agentapiURL string) {
	log.Printf("[PROVISIONER] Waiting for agent ready (phase 1: running→stable, 10s timeout)")

	sawRunning := false
	for i := 0; i < 10; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}

		st := getAgentStatus(client, agentapiURL)
		if st == "running" {
			sawRunning = true
		}
		if sawRunning && st == "stable" {
			if countNonEmptyMessages(client, agentapiURL) > 0 {
				log.Printf("[PROVISIONER] Phase 1: running→stable transition detected")
				return
			}
		}
	}

	// Phase 2 fallback.
	log.Printf("[PROVISIONER] Phase 1 timed out (sawRunning=%v), falling back to stable+non-empty check", sawRunning)
	for i := 0; i < 60; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}

		if getAgentStatus(client, agentapiURL) == "stable" && countNonEmptyMessages(client, agentapiURL) > 0 {
			log.Printf("[PROVISIONER] Phase 2: agent stable with non-empty messages")
			return
		}
	}
	log.Printf("[PROVISIONER] WARNING: agent not fully ready after 70s, sending anyway")
}

// waitForStable polls until agentapi reports "stable" or the context is done.
func waitForStable(ctx context.Context, client *http.Client, agentapiURL string, maxRetries int) {
	for i := 0; i < maxRetries; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
		if getAgentStatus(client, agentapiURL) == "stable" {
			return
		}
	}
	log.Printf("[PROVISIONER] WARNING: agent not stable after %d retries", maxRetries)
}

// getAgentStatus fetches /status and returns the "status" field value.
func getAgentStatus(client *http.Client, agentapiURL string) string {
	resp, err := client.Get(agentapiURL + "/status")
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

	var sr agentStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return ""
	}
	return sr.Status
}

// countUserMessages returns the number of messages with role "user".
func countUserMessages(client *http.Client, agentapiURL string) int {
	resp, err := client.Get(agentapiURL + "/messages")
	if err != nil {
		return 0
	}
	defer func() { _ = resp.Body.Close() }()

	var mr agentMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return 0
	}
	count := 0
	for _, m := range mr.Messages {
		if m.Role == "user" {
			count++
		}
	}
	return count
}

// countNonEmptyMessages returns the number of messages with non-empty content.
func countNonEmptyMessages(client *http.Client, agentapiURL string) int {
	resp, err := client.Get(agentapiURL + "/messages")
	if err != nil {
		return 0
	}
	defer func() { _ = resp.Body.Close() }()

	var mr agentMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return 0
	}
	count := 0
	for _, m := range mr.Messages {
		if m.Content != "" {
			count++
		}
	}
	return count
}

// fetchAndInjectMemory fetches session memory from the proxy and appends it to
// CLAUDE.md, replicating the bash memory injection in buildClaudeStartCommand.
func (s *Server) fetchAndInjectMemory() {
	memoryKeyFlags := os.Getenv("MEMORY_KEY_FLAGS")
	if memoryKeyFlags == "" {
		return
	}

	proxyHost := os.Getenv("AGENTAPI_PROXY_SERVICE_HOST")
	proxyPort := os.Getenv("AGENTAPI_PROXY_SERVICE_PORT_HTTP")
	if proxyHost == "" || proxyPort == "" {
		log.Printf("[PROVISIONER] AGENTAPI_PROXY_SERVICE_HOST or PORT not set, skipping memory fetch")
		return
	}
	proxyEndpoint := fmt.Sprintf("http://%s:%s", proxyHost, proxyPort)

	scope := os.Getenv("AGENTAPI_SCOPE")
	if scope == "" {
		scope = "user"
	}

	args := []string{
		"client", "memory", "list",
		"--scope", scope,
		"--union",
		"--format", "markdown",
		"--endpoint", proxyEndpoint,
	}

	// Append memory key flags (e.g. "--key foo --key bar").
	args = append(args, strings.Fields(memoryKeyFlags)...)

	if scope == "team" {
		if teamID := os.Getenv("AGENTAPI_TEAM_ID"); teamID != "" {
			args = append(args, "--team-id", teamID)
		}
	}

	log.Printf("[PROVISIONER] Fetching session memory (keys: %s)", memoryKeyFlags)
	out, err := exec.Command("agentapi-proxy", args...).Output()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		log.Printf("[PROVISIONER] No memory found for this session (non-fatal)")
		return
	}

	f, err := os.OpenFile(claudeMDPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("[PROVISIONER] Warning: failed to open CLAUDE.md for memory injection: %v", err)
		return
	}
	defer func() { _ = f.Close() }()

	_, _ = fmt.Fprintf(f, "\n---\n\n%s\n", bytes.TrimSpace(out))
	log.Printf("[PROVISIONER] Memory injected into CLAUDE.md")
}

// loadEnvFile reads a KEY=VALUE env file and returns the entries as a map.
// Lines starting with '#' and blank lines are ignored.
func loadEnvFile(path string) map[string]string {
	result := make(map[string]string)
	f, err := os.Open(path)
	if err != nil {
		return result
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := line[:idx]
		val := unquoteValue(line[idx+1:])
		result[key] = val
	}
	return result
}

// unquoteValue removes surrounding single or double quotes from a shell value.
func unquoteValue(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// writeWebhookPayloadFile writes the webhook payload JSON string to
// webhookPayloadPath.  It is a no-op when the file already exists, which is
// the case for non-stock sessions where a read-only Kubernetes Secret volume
// provides the file.  For stock sessions the volume is absent, so the
// provisioner writes the file itself to ensure the agent always finds the
// payload at the same well-known path.
func writeWebhookPayloadFile(payload string) {
	writeWebhookPayloadFileToPath(webhookPayloadPath, payload)
}

// writeWebhookPayloadFileToPath is the path-parameterised implementation
// shared by writeWebhookPayloadFile and unit tests.
func writeWebhookPayloadFileToPath(path, payload string) {
	if _, err := os.Stat(path); err == nil {
		// File already present (non-stock session: Secret volume mount).
		log.Printf("[PROVISIONER] Webhook payload file already exists at %s, skipping write", path)
		return
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to create webhook payload dir %s: %v", dir, err)
		return
	}

	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to write webhook payload file %s: %v", path, err)
		return
	}

	log.Printf("[PROVISIONER] Wrote webhook payload to %s (%d bytes)", path, len(payload))
}

// mergeEnv takes the current os.Environ() slice and overlays envMap entries.
// The envMap values take precedence (session-specific overrides).
func mergeEnv(base []string, overlay map[string]string) []string {
	// Build a map from base env (last occurrence wins if duplicated).
	merged := make(map[string]string, len(base)+len(overlay))
	for _, kv := range base {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		merged[kv[:idx]] = kv[idx+1:]
	}
	for k, v := range overlay {
		merged[k] = v
	}

	result := make([]string, 0, len(merged))
	for k, v := range merged {
		result = append(result, k+"="+v)
	}
	sort.Strings(result)
	return result
}
