package provisioner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestConfigureTelemetry(t *testing.T) {
	env := map[string]string{
		"OTEL_RESOURCE_ATTRIBUTES": "deployment.environment=dev,agentapi_session_id=untrusted",
	}
	settings := &sessionsettings.SessionSettings{
		Telemetry: &sessionsettings.TelemetryConfig{
			Enabled:    true,
			SessionID:  "session-1",
			UserID:     "user-1",
			TeamID:     "org/team",
			ScheduleID: "schedule-1",
			WebhookID:  "-",
			AgentType:  "claude",
		},
	}

	configureTelemetry(settings, env)

	if got := env["CLAUDE_CODE_ENABLE_TELEMETRY"]; got != "1" {
		t.Fatalf("CLAUDE_CODE_ENABLE_TELEMETRY = %q, want 1", got)
	}
	if got := env["OTEL_METRICS_EXPORTER"]; got != "prometheus" {
		t.Fatalf("OTEL_METRICS_EXPORTER = %q, want prometheus", got)
	}
	want := "deployment.environment=dev," +
		"agentapi_session_id=session-1," +
		"agentapi_user_id=user-1," +
		"agentapi_team_id=org%2Fteam," +
		"agentapi_schedule_id=schedule-1," +
		"agentapi_webhook_id=-," +
		"agentapi_agent_type=claude"
	if got := env["OTEL_RESOURCE_ATTRIBUTES"]; got != want {
		t.Fatalf("OTEL_RESOURCE_ATTRIBUTES = %q, want %q", got, want)
	}
}

func TestConfigureTelemetryDisabled(t *testing.T) {
	env := map[string]string{}
	configureTelemetry(&sessionsettings.SessionSettings{}, env)
	if len(env) != 0 {
		t.Fatalf("disabled telemetry mutated environment: %#v", env)
	}
}

// TestWriteWebhookPayloadFile_WritesWhenAbsent verifies that
// writeWebhookPayloadFile creates the file with the given payload when
// webhookPayloadPath does not exist (stock-session case).
func TestWriteWebhookPayloadFile_WritesWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opt", "webhook", "payload.json")

	// Override the package-level constant for the duration of this test.
	origPath := webhookPayloadPath
	// webhookPayloadPath is a const, so we use a helper that accepts the path.
	// Call the internal logic directly via the exported-for-test variant.
	// Since the function is unexported, we test it by temporarily overriding
	// the path via a wrapper test helper.
	_ = origPath // suppress unused warning

	writeWebhookPayloadFileToPath(path, `{"event":"push"}`)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to be created at %s, got error: %v", path, err)
	}
	if string(got) != `{"event":"push"}` {
		t.Errorf("expected payload %q, got %q", `{"event":"push"}`, string(got))
	}
}

func TestBuildAgentCommandCursor(t *testing.T) {
	t.Setenv("AGENTAPI_PORT", "9000")

	cmd, args := (&Server{}).buildAgentCommand(&sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{AgentType: "cursor"},
	}, nil)

	if cmd != "agentapi-proxy" {
		t.Fatalf("command = %q, want agentapi-proxy", cmd)
	}
	want := []string{"acp-server", "--port", "9000", "--auto-approve", "--raw-json-log", "--", "agent", "acp"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestBuildAgentCommandPiOllama(t *testing.T) {
	t.Setenv("AGENTAPI_PORT", "9000")
	origCommandPath := piOllamaCommandPath
	piOllamaCommandPath = filepath.Join(t.TempDir(), "pi-ollama-pi")
	t.Cleanup(func() { piOllamaCommandPath = origCommandPath })
	env := map[string]string{"OPENAI_API_KEY": "openai-key"}

	cmd, args := (&Server{}).buildAgentCommand(&sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{AgentType: "pi-ollama"},
	}, env)

	if cmd != "agentapi-proxy" {
		t.Fatalf("command = %q, want agentapi-proxy", cmd)
	}
	want := []string{"acp-server", "--port", "9000", "--auto-approve", "--", "npx", "-y", "pi-acp"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	if _, ok := env["OLLAMA_API_KEY"]; ok {
		t.Fatalf("OLLAMA_API_KEY should not be synthesized")
	}
	if got := env["PI_ACP_PI_COMMAND"]; got != piOllamaCommandPath {
		t.Fatalf("PI_ACP_PI_COMMAND = %q", got)
	}
	content, err := os.ReadFile(piOllamaCommandPath)
	if err != nil {
		t.Fatalf("failed to read pi-ollama wrapper: %v", err)
	}
	if !strings.Contains(string(content), `exec pi --model "$model" "$@"`) {
		t.Fatalf("wrapper does not pass model args to pi: %s", string(content))
	}
}

func TestBuildAgentCommandPiOllamaPreservesExplicitModel(t *testing.T) {
	t.Setenv("AGENTAPI_PORT", "9000")
	env := map[string]string{
		"PI_ACP_PI_COMMAND": "/custom/pi-wrapper",
		"PI_OLLAMA_MODEL":   "ollama-cloud/gpt-oss:120b",
	}

	_, _ = (&Server{}).buildAgentCommand(&sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{AgentType: "pi-ollama"},
	}, env)

	if got := env["PI_ACP_PI_COMMAND"]; got != "/custom/pi-wrapper" {
		t.Fatalf("PI_ACP_PI_COMMAND = %q", got)
	}
}

func TestSymlinkCodexSkillsForPiCreatesLink(t *testing.T) {
	dir := t.TempDir()
	codexPath := filepath.Join(dir, ".codex", "skills")
	piPath := filepath.Join(dir, ".pi", "agent", "skills")

	if err := symlinkCodexSkillsForPi(codexPath, piPath); err != nil {
		t.Fatalf("symlinkCodexSkillsForPi: %v", err)
	}
	target, err := os.Readlink(piPath)
	if err != nil {
		t.Fatalf("read pi skills symlink: %v", err)
	}
	if target != codexPath {
		t.Fatalf("pi skills symlink target = %q, want %q", target, codexPath)
	}
	if _, err := os.Stat(codexPath); err != nil {
		t.Fatalf("codex skills dir should exist: %v", err)
	}
}

func TestSymlinkCodexSkillsForPiIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	codexPath := filepath.Join(dir, ".codex", "skills")
	piPath := filepath.Join(dir, ".pi", "agent", "skills")

	if err := symlinkCodexSkillsForPi(codexPath, piPath); err != nil {
		t.Fatalf("first symlinkCodexSkillsForPi: %v", err)
	}
	if err := symlinkCodexSkillsForPi(codexPath, piPath); err != nil {
		t.Fatalf("second symlinkCodexSkillsForPi: %v", err)
	}
}

func TestSymlinkCodexSkillsForPiReplacesEmptyDir(t *testing.T) {
	dir := t.TempDir()
	codexPath := filepath.Join(dir, ".codex", "skills")
	piPath := filepath.Join(dir, ".pi", "agent", "skills")
	if err := os.MkdirAll(piPath, 0o755); err != nil {
		t.Fatalf("mkdir pi skills: %v", err)
	}

	if err := symlinkCodexSkillsForPi(codexPath, piPath); err != nil {
		t.Fatalf("symlinkCodexSkillsForPi: %v", err)
	}
	target, err := os.Readlink(piPath)
	if err != nil {
		t.Fatalf("read pi skills symlink: %v", err)
	}
	if target != codexPath {
		t.Fatalf("pi skills symlink target = %q, want %q", target, codexPath)
	}
}

func TestSymlinkCodexSkillsForPiPreservesNonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	codexPath := filepath.Join(dir, ".codex", "skills")
	piPath := filepath.Join(dir, ".pi", "agent", "skills")
	if err := os.MkdirAll(piPath, 0o755); err != nil {
		t.Fatalf("mkdir pi skills: %v", err)
	}
	existingPath := filepath.Join(piPath, "existing.md")
	if err := os.WriteFile(existingPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing skill: %v", err)
	}

	if err := symlinkCodexSkillsForPi(codexPath, piPath); err == nil {
		t.Fatal("expected error for non-empty pi skills dir")
	}
	if got, err := os.ReadFile(existingPath); err != nil || string(got) != "existing" {
		t.Fatalf("existing skill was not preserved, got %q err=%v", string(got), err)
	}
}

func TestWritePiAgentInstructionsCopiesClaudeMD(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, ".claude", "CLAUDE.md")
	destPath := filepath.Join(dir, ".pi", "agent", "AGENTS.md")
	content := "# Instructions\n\nUse the repo conventions.\n"
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := writePiAgentInstructions(sourcePath, destPath); err != nil {
		t.Fatalf("writePiAgentInstructions: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read Pi AGENTS.md: %v", err)
	}
	if string(got) != content {
		t.Fatalf("Pi AGENTS.md = %q, want %q", string(got), content)
	}
}

func TestWritePiAgentInstructionsSkipsMissingSource(t *testing.T) {
	destPath := filepath.Join(t.TempDir(), ".pi", "agent", "AGENTS.md")

	if err := writePiAgentInstructions("/does/not/exist/CLAUDE.md", destPath); err != nil {
		t.Fatalf("writePiAgentInstructions should skip missing source: %v", err)
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatalf("Pi AGENTS.md should not be created, stat err=%v", err)
	}
}

func TestWritePiMCPServersMergesSettings(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), ".pi", "agent", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	existing := map[string]interface{}{
		"defaultProvider": "ollama-cloud",
		"theme":           "dark",
	}
	existingData, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(settingsPath, existingData, 0o644); err != nil {
		t.Fatalf("write existing settings: %v", err)
	}

	servers := map[string]interface{}{
		"github": map[string]interface{}{
			"type":    "stdio",
			"command": "github-mcp-server",
			"args":    []interface{}{"stdio"},
			"env": map[string]interface{}{
				"GITHUB_TOKEN": "token",
			},
		},
	}
	if err := writePiMCPServers(settingsPath, servers); err != nil {
		t.Fatalf("writePiMCPServers: %v", err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if got["defaultProvider"] != "ollama-cloud" || got["theme"] != "dark" {
		t.Fatalf("existing settings not preserved: %#v", got)
	}
	gotServers, ok := got["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcpServers = %T, want map", got["mcpServers"])
	}
	if _, ok := gotServers["github"]; !ok {
		t.Fatalf("github MCP server missing: %#v", gotServers)
	}
}

func TestPiMCPServersPrefersCodexThenClaude(t *testing.T) {
	claudeServers := map[string]interface{}{"claude": map[string]interface{}{"type": "http", "url": "https://claude.example"}}
	codexServers := map[string]interface{}{"codex": map[string]interface{}{"type": "http", "url": "https://codex.example"}}

	got := piMCPServers(&sessionsettings.SessionSettings{
		Claude: sessionsettings.ClaudeConfig{MCPServers: claudeServers},
		Codex:  sessionsettings.CodexConfig{MCPServers: codexServers},
	})
	if !reflect.DeepEqual(got, codexServers) {
		t.Fatalf("piMCPServers should prefer Codex MCP servers, got %#v", got)
	}

	got = piMCPServers(&sessionsettings.SessionSettings{
		Claude: sessionsettings.ClaudeConfig{MCPServers: claudeServers},
	})
	if !reflect.DeepEqual(got, claudeServers) {
		t.Fatalf("piMCPServers should fall back to Claude MCP servers, got %#v", got)
	}
}

func TestSyncedManagedFilePathsExcludesUnsyncedPaths(t *testing.T) {
	got := syncedManagedFilePaths([]string{
		" /home/agentapi/.codex/auth.json ",
		"",
	})
	want := []string{"/home/agentapi/.claude/.credentials.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("syncedManagedFilePaths() = %#v, want %#v", got, want)
	}
}

// TestWriteWebhookPayloadFile_SkipsWhenPresent verifies that
// writeWebhookPayloadFile does NOT overwrite an existing file (non-stock case:
// Secret volume mount already provides the file as read-only).
func TestWriteWebhookPayloadFile_SkipsWhenPresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.json")

	existing := `{"original":"content"}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	writeWebhookPayloadFileToPath(path, `{"should":"not-overwrite"}`)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(got) != existing {
		t.Errorf("expected original content %q to be preserved, got %q", existing, string(got))
	}
}

// TestWriteWebhookPayloadFile_CreatesParentDirs verifies that parent
// directories are created as needed.
func TestWriteWebhookPayloadFile_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "payload.json")

	writeWebhookPayloadFileToPath(path, `{"nested":true}`)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist at %s: %v", path, err)
	}
}

// TestMergeHooksIntoSettingsFile_ManagedOverwritesCompiled verifies that when
// a managed settings.json has overwritten the compiled one, mergeHooksIntoSettingsFile
// restores the compiled hooks without losing the managed file's other settings.
func TestMergeHooksIntoSettingsFile_ManagedOverwritesCompiled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Simulate: managed settings.json (no hooks — user's saved version)
	managed := map[string]interface{}{
		"autoUpdatesChannel": "stable",
		"theme":              "dark",
	}
	managedData, _ := json.MarshalIndent(managed, "", "  ")
	if err := os.WriteFile(path, managedData, 0644); err != nil {
		t.Fatalf("write managed file: %v", err)
	}

	// Compiled settings with Stop hook for oneshot deletion
	compiledSettings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "agentapi-proxy client delete-session --confirm",
						},
					},
				},
			},
		},
	}

	if err := mergeHooksIntoSettingsFile(path, compiledSettings); err != nil {
		t.Fatalf("mergeHooksIntoSettingsFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// User settings must be preserved
	if result["theme"] != "dark" {
		t.Errorf("expected theme=dark to be preserved, got %v", result["theme"])
	}

	// Compiled hooks must be present
	hooks, ok := result["hooks"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected hooks to be map, got %T", result["hooks"])
	}
	stopHooks, ok := hooks["Stop"]
	if !ok {
		t.Fatal("expected Stop hook to be present after merge")
	}
	stopList, ok := stopHooks.([]interface{})
	if !ok || len(stopList) == 0 {
		t.Fatalf("expected Stop hooks list, got %T: %v", stopHooks, stopHooks)
	}
}

// TestMergeHooksIntoSettingsFile_PreservesExistingHooks verifies that hooks
// already in the managed settings.json are preserved when no compiled hook
// overrides them.
func TestMergeHooksIntoSettingsFile_PreservesExistingHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Managed file has a Notification hook
	managed := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Notification": []interface{}{
				map[string]interface{}{"hooks": []interface{}{
					map[string]interface{}{"type": "command", "command": "echo notif"},
				}},
			},
		},
	}
	managedData, _ := json.MarshalIndent(managed, "", "  ")
	if err := os.WriteFile(path, managedData, 0644); err != nil {
		t.Fatalf("write managed file: %v", err)
	}

	// Compiled settings only has a Stop hook
	compiledSettings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "agentapi-proxy client delete-session --confirm"},
					},
				},
			},
		},
	}

	if err := mergeHooksIntoSettingsFile(path, compiledSettings); err != nil {
		t.Fatalf("mergeHooksIntoSettingsFile: %v", err)
	}

	data, _ := os.ReadFile(path)
	var result map[string]interface{}
	_ = json.Unmarshal(data, &result)

	hooks := result["hooks"].(map[string]interface{})
	if _, ok := hooks["Notification"]; !ok {
		t.Error("expected Notification hook to be preserved from managed file")
	}
	if _, ok := hooks["Stop"]; !ok {
		t.Error("expected Stop hook from compiled settings to be added")
	}
}

// TestMergeHooksIntoSettingsFile_NoHooksInCompiled verifies that when
// compiledSettings has no hooks, the function is a no-op.
func TestMergeHooksIntoSettingsFile_NoHooksInCompiled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	original := `{"theme":"light"}`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	compiledSettings := map[string]interface{}{"autoUpdatesChannel": "stable"}
	if err := mergeHooksIntoSettingsFile(path, compiledSettings); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should be unchanged
	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("expected file unchanged, got %q", string(got))
	}
}

func TestBuildCodexRequirementsTOML(t *testing.T) {
	hooksJSON := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "agentapi-proxy client memory save-session >> /tmp/memory-save-session.log 2>&1",
						},
					},
				},
			},
			"Notification": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "agentapi-proxy client send-notification",
						},
					},
				},
			},
		},
	}

	got, err := buildCodexRequirementsTOML(hooksJSON)
	if err != nil {
		t.Fatalf("buildCodexRequirementsTOML: %v", err)
	}

	mustContain := []string{
		"[hooks]",
		"managed_dir = \"/etc/codex\"",
		"[[hooks.Stop]]",
		"[[hooks.Stop.hooks]]",
		"type = \"command\"",
		"command = \"agentapi-proxy client memory save-session >> /tmp/memory-save-session.log 2>&1\"",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Fatalf("expected TOML to contain %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Notification") {
		t.Fatalf("Notification hooks should not be written for Codex, got:\n%s", got)
	}
}

func TestWriteCodexRequirementsToPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "etc", "codex", "requirements.toml")
	content := "[hooks]\nmanaged_dir = \"/etc/codex\"\n"

	if err := writeCodexRequirementsToPath(path, content); err != nil {
		t.Fatalf("writeCodexRequirementsToPath: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read requirements: %v", err)
	}
	if string(got) != content {
		t.Fatalf("expected %q, got %q", content, string(got))
	}
}
