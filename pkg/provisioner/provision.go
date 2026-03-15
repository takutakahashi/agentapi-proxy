package provisioner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const (
	provisionTempSettings = "/tmp/provision-settings.yaml"
	sessionEnvFile        = "/home/agentapi/.session/env"
	memoriesDir           = "/memories"
	workdirRepoPath       = "/home/agentapi/workdir/repo"
)

// runProvision executes the full provisioning sequence and then supervises
// the agentapi subprocess.
//
// Sequence:
//  1. Write received settings to a temp YAML file
//  2. Run sessionsettings.Setup() (write-pem, clone-repo, compile, sync-extra)
//  3. Load the generated session env file
//  4. Fetch memory from the proxy and save to /memories/session-memory.md
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

	// ── Step 2: run setup ─────────────────────────────────────────────────────
	// Override CompileOptions.InputPath to use the temp file written above,
	// not the default /session-settings/settings.yaml (which is no longer mounted).
	compileOpts := sessionsettings.DefaultCompileOptions()
	compileOpts.InputPath = provisionTempSettings

	opts := sessionsettings.SetupOptions{
		InputPath:                 provisionTempSettings,
		CompileOptions:            compileOpts,
		CredentialsFile:           "/credentials-config/credentials.json",
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

	// ── Step 6: build and start the agent subprocess ──────────────────────────
	agentCmd, agentArgs := s.buildAgentCommand(settings, envMap)
	log.Printf("[PROVISIONER] Starting agent: %s %v", agentCmd, agentArgs)

	cmd := exec.CommandContext(ctx, agentCmd, agentArgs...)
	cmd.Env = mergeEnv(os.Environ(), envMap)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		s.setStatus(StatusError, fmt.Sprintf("failed to start agent: %v", err))
		return
	}
	log.Printf("[PROVISIONER] Agent process started (pid %d)", cmd.Process.Pid)

	// ── Step 7: wait for agentapi to be ready ─────────────────────────────────
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

	// ── Step 8: send initial message ─────────────────────────────────────────
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

	// ── Step 9: mark ready and supervise ─────────────────────────────────────
	s.setStatus(StatusReady, "")

	// ── Step 10: launch claude-posts subprocess if SlackParams provided ───────
	if settings.SlackParams != nil && settings.SlackParams.Channel != "" {
		go s.runClaudePosts(ctx, settings.SlackParams)
	}

	// Supervise: if agentapi exits, report error so K8s restarts the Pod.
	go func() {
		if err := cmd.Wait(); err != nil {
			s.setStatus(StatusError, fmt.Sprintf("agent process exited: %v", err))
		} else {
			s.setStatus(StatusError, "agent process exited with code 0")
		}
	}()
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

// fetchAndInjectMemory fetches session memory from the proxy and saves it to
// the /memories directory as a markdown file, so that the agent can access it
// via Claude Code's native memory feature.
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

	// Ensure the /memories directory exists.
	if err := os.MkdirAll(memoriesDir, 0o755); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to create %s: %v", memoriesDir, err)
		return
	}

	memoryFilePath := memoriesDir + "/session-memory.md"
	if err := os.WriteFile(memoryFilePath, append(bytes.TrimSpace(out), '\n'), 0o644); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to write memory to %s: %v", memoryFilePath, err)
		return
	}
	log.Printf("[PROVISIONER] Memory saved to %s", memoryFilePath)
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
