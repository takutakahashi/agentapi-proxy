package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Input types
// ---------------------------------------------------------------------------

// generateSettingInput is the top-level input structure for the generate-setting command.
// It contains the SlackBot configuration and optional user/team settings, ordered from
// lowest priority to highest priority.
type generateSettingInput struct {
	SlackBot *generateSettingSlackBot   `json:"slackbot"`
	Settings []*generateSettingSettings `json:"settings,omitempty"`
}

// generateSettingSlackBot represents the relevant SlackBot configuration.
type generateSettingSlackBot struct {
	UserID        string                        `json:"user_id,omitempty"`
	Scope         string                        `json:"scope,omitempty"` // "user" or "team"
	TeamID        string                        `json:"team_id,omitempty"`
	Teams         []string                      `json:"teams,omitempty"` // GitHub team slugs, e.g. ["myorg/backend"]
	SessionConfig *generateSettingSessionConfig `json:"session_config,omitempty"`
}

// generateSettingSessionConfig is the session configuration embedded in a SlackBot.
type generateSettingSessionConfig struct {
	Environment            map[string]string      `json:"environment,omitempty"`
	Tags                   map[string]string      `json:"tags,omitempty"`
	InitialMessageTemplate string                 `json:"initial_message_template,omitempty"`
	Params                 *generateSettingParams `json:"params,omitempty"`
}

// generateSettingParams holds session-creation parameters.
type generateSettingParams struct {
	AgentType string `json:"agent_type,omitempty"`
	Oneshot   bool   `json:"oneshot,omitempty"`
}

// generateSettingSettings represents user or team settings that influence the session.
// Entries in the settings array are applied in order (index 0 = lowest priority,
// last entry = highest priority). Typically: base → teams… → user.
type generateSettingSettings struct {
	Name                 string                  `json:"name,omitempty"`
	AuthMode             string                  `json:"auth_mode,omitempty"` // "bedrock", "oauth", or "" (empty = no-op)
	Bedrock              *generateSettingBedrock `json:"bedrock,omitempty"`
	EnvVars              map[string]string       `json:"env_vars,omitempty"`
	ClaudeCodeOAuthToken string                  `json:"claude_code_oauth_token,omitempty"`
}

// generateSettingBedrock holds AWS Bedrock credential settings.
type generateSettingBedrock struct {
	Enabled         bool   `json:"enabled"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

// ---------------------------------------------------------------------------
// Command flags
// ---------------------------------------------------------------------------

var (
	generateSettingInputPath string
	generateSettingOutputFmt string
	generateSettingVerbose   bool
)

// ---------------------------------------------------------------------------
// Cobra command definition
// ---------------------------------------------------------------------------

var generateSettingCmd = &cobra.Command{
	Use:   "generate-setting",
	Short: "SlackBot 設定から session settings JSON を組み立てて出力する",
	Long: `SlackBot の設定をもとに、実際のセッション起動時と同じ手順で session settings を組み立て、
その過程をログに出しながら最終的な JSON / YAML を標準出力します。

手順ログには以下が含まれます:
  1. settings エントリの適用 (base → team(s) → user の優先度順)
  2. Bedrock 設定を env に入れる際の判断基準
  3. SlackBot の session_config.environment の上書き適用

入力 JSON の形式:
  {
    "slackbot": {
      "user_id": "alice",
      "scope": "user",
      "teams": ["myorg/backend-team"],
      "session_config": {
        "environment": { "MY_VAR": "my_value" },
        "initial_message_template": "Hello!",
        "params": { "agent_type": "claude-agentapi", "oneshot": false }
      }
    },
    "settings": [
      {
        "name": "myorg/backend-team",
        "auth_mode": "bedrock",
        "bedrock": {
          "enabled": true,
          "model": "anthropic.claude-3-5-sonnet-20241022-v2:0",
          "role_arn": "arn:aws:iam::123456789012:role/bedrock-role"
        },
        "env_vars": { "TEAM_VAR": "team_value" }
      },
      {
        "name": "alice",
        "auth_mode": "bedrock",
        "bedrock": {
          "enabled": true,
          "access_key_id": "AKIAIOSFODNN7EXAMPLE",
          "secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
        }
      }
    ]
  }

settings エントリは配列の先頭が最低優先度、末尾が最高優先度です。
後から適用されたエントリが前のエントリの値を上書きします。

使用例:
  # ファイルから読み込み
  agentapi-proxy helpers generate-setting --input config.json

  # 標準入力から読み込み
  cat config.json | agentapi-proxy helpers generate-setting

  # YAML 形式で出力
  agentapi-proxy helpers generate-setting --input config.json --format yaml`,
	RunE: runGenerateSetting,
}

func init() {
	generateSettingCmd.Flags().StringVarP(&generateSettingInputPath, "input", "i", "-",
		"入力 JSON ファイルパス (\"-\" で標準入力)")
	generateSettingCmd.Flags().StringVar(&generateSettingOutputFmt, "format", "json",
		"出力形式 (json または yaml)")
	generateSettingCmd.Flags().BoolVarP(&generateSettingVerbose, "verbose", "v", false,
		"詳細ログを出力する")

	HelpersCmd.AddCommand(generateSettingCmd)
}

// ---------------------------------------------------------------------------
// Main logic
// ---------------------------------------------------------------------------

func runGenerateSetting(cmd *cobra.Command, args []string) error {
	// Validate format flag
	if generateSettingOutputFmt != "json" && generateSettingOutputFmt != "yaml" {
		return fmt.Errorf("invalid format %q: must be json or yaml", generateSettingOutputFmt)
	}

	// --- Step 0: Read input ---
	log.Printf("[GENERATE-SETTING] ▶ 入力 JSON を読み込んでいます...")

	inputData, err := readGenerateSettingInput(generateSettingInputPath)
	if err != nil {
		return fmt.Errorf("入力の読み込みに失敗しました: %w", err)
	}

	var input generateSettingInput
	if err := json.Unmarshal(inputData, &input); err != nil {
		return fmt.Errorf("入力 JSON の解析に失敗しました: %w", err)
	}

	if input.SlackBot == nil {
		return fmt.Errorf("入力 JSON に \"slackbot\" フィールドが必要です")
	}

	bot := input.SlackBot
	log.Printf("[GENERATE-SETTING] SlackBot: user_id=%q scope=%q team_id=%q teams=%v",
		bot.UserID, bot.Scope, bot.TeamID, bot.Teams)
	log.Printf("[GENERATE-SETTING] settings エントリ数: %d", len(input.Settings))

	// --- Build env map step by step ---
	env := make(map[string]string)

	// --- Step 1: Apply settings in order (lowest priority first) ---
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ▶ 手順 1: settings の適用 (優先度: 低→高)")
	printBedrockCriteria()

	for idx, s := range input.Settings {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("<settings[%d]>", idx)
		}
		log.Printf("[GENERATE-SETTING]   [%d/%d] settings 適用: name=%q", idx+1, len(input.Settings), name)
		applySettingsToEnv(env, s, generateSettingVerbose)
	}

	// --- Step 2: Apply SlackBot session_config.environment (highest priority) ---
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ▶ 手順 2: SlackBot session_config.environment の適用 (最高優先度)")
	if bot.SessionConfig != nil && len(bot.SessionConfig.Environment) > 0 {
		keys := sortedKeys(bot.SessionConfig.Environment)
		for _, k := range keys {
			v := bot.SessionConfig.Environment[k]
			if old, exists := env[k]; exists && old != v {
				log.Printf("[GENERATE-SETTING]   上書き: %s=%q (旧値: %q)", k, v, old)
			} else {
				log.Printf("[GENERATE-SETTING]   設定: %s=%q", k, v)
			}
			env[k] = v
		}
	} else {
		log.Printf("[GENERATE-SETTING]   (session_config.environment は空です)")
	}

	// --- Step 3: Build SessionSettings struct ---
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ▶ 手順 3: SessionSettings の組み立て")

	agentType := ""
	oneshot := false
	initialMessage := ""
	if bot.SessionConfig != nil {
		initialMessage = bot.SessionConfig.InitialMessageTemplate
		if bot.SessionConfig.Params != nil {
			agentType = bot.SessionConfig.Params.AgentType
			oneshot = bot.SessionConfig.Params.Oneshot
		}
	}

	scope := bot.Scope
	if scope == "" {
		scope = "user"
	}

	settings := &sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{
			UserID:    bot.UserID,
			Scope:     scope,
			TeamID:    bot.TeamID,
			AgentType: agentType,
			Oneshot:   oneshot,
			Teams:     bot.Teams,
		},
		Env:            env,
		InitialMessage: initialMessage,
		Claude: sessionsettings.ClaudeConfig{
			ClaudeJSON: map[string]interface{}{
				"hasCompletedOnboarding":        true,
				"bypassPermissionsModeAccepted": true,
			},
		},
	}

	// Determine startup command from agent type
	if agentType == "claude-agentapi" {
		settings.Startup = sessionsettings.StartupConfig{
			Command: []string{"claude-agentapi"},
		}
		log.Printf("[GENERATE-SETTING]   startup.command: [claude-agentapi]")
	} else {
		settings.Startup = sessionsettings.StartupConfig{
			Command: []string{"agentapi", "server"},
			Args:    []string{"--allowed-hosts", "*", "--allowed-origins", "*"},
		}
		log.Printf("[GENERATE-SETTING]   startup.command: [agentapi server --allowed-hosts * --allowed-origins *]")
	}

	log.Printf("[GENERATE-SETTING]   session.user_id=%q scope=%q agent_type=%q oneshot=%v",
		settings.Session.UserID, settings.Session.Scope, settings.Session.AgentType, settings.Session.Oneshot)
	log.Printf("[GENERATE-SETTING]   env 変数数: %d", len(env))
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ✓ SessionSettings の組み立て完了")

	// --- Output ---
	return outputSessionSettings(settings, generateSettingOutputFmt)
}

// ---------------------------------------------------------------------------
// Bedrock judgment logic (mirrors expandSettingsToEnv in kubernetes_session_manager.go)
// ---------------------------------------------------------------------------

// printBedrockCriteria logs the Bedrock judgment criteria that govern how Bedrock
// settings are translated into environment variables.
func printBedrockCriteria() {
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING]   ┌─ Bedrock 設定を env に入れる際の判断基準 ────────────────────────────────┐")
	log.Printf("[GENERATE-SETTING]   │")
	log.Printf("[GENERATE-SETTING]   │  auth_mode の値によって以下のように判断します:")
	log.Printf("[GENERATE-SETTING]   │")
	log.Printf("[GENERATE-SETTING]   │  \"bedrock\" の場合:")
	log.Printf("[GENERATE-SETTING]   │    → CLAUDE_CODE_USE_BEDROCK=1 を設定")
	log.Printf("[GENERATE-SETTING]   │    → Bedrock 資格情報を env に追加 (各フィールドは空でない場合のみ)")
	log.Printf("[GENERATE-SETTING]   │      - model           → ANTHROPIC_MODEL")
	log.Printf("[GENERATE-SETTING]   │      - access_key_id   → AWS_ACCESS_KEY_ID")
	log.Printf("[GENERATE-SETTING]   │      - secret_access_key → AWS_SECRET_ACCESS_KEY")
	log.Printf("[GENERATE-SETTING]   │      - role_arn        → AWS_ROLE_ARN")
	log.Printf("[GENERATE-SETTING]   │      - profile         → AWS_PROFILE")
	log.Printf("[GENERATE-SETTING]   │    ★ 空値フィールドはスキップ: 高優先度の設定を上書きしないため")
	log.Printf("[GENERATE-SETTING]   │")
	log.Printf("[GENERATE-SETTING]   │  \"oauth\" の場合:")
	log.Printf("[GENERATE-SETTING]   │    → CLAUDE_CODE_USE_BEDROCK=0 を設定")
	log.Printf("[GENERATE-SETTING]   │    → 全 AWS 資格情報をクリア (空文字列で上書き)")
	log.Printf("[GENERATE-SETTING]   │    → claude_code_oauth_token があれば CLAUDE_CODE_OAUTH_TOKEN を設定")
	log.Printf("[GENERATE-SETTING]   │")
	log.Printf("[GENERATE-SETTING]   │  \"\" (空 = 未設定) の場合:")
	log.Printf("[GENERATE-SETTING]   │    → auth 関連の env vars を一切変更しない")
	log.Printf("[GENERATE-SETTING]   │    ★ 重要: これにより team の Bedrock 設定を user 設定が上書きしない")
	log.Printf("[GENERATE-SETTING]   │       例) team: auth_mode=bedrock  user: auth_mode=\"\"")
	log.Printf("[GENERATE-SETTING]   │           → team の CLAUDE_CODE_USE_BEDROCK=1 が保持される")
	log.Printf("[GENERATE-SETTING]   │")
	log.Printf("[GENERATE-SETTING]   │  優先度: settings 配列の先頭が最低、末尾が最高 (後勝ち)")
	log.Printf("[GENERATE-SETTING]   │  設定後に SlackBot session_config.environment が最高優先度で適用")
	log.Printf("[GENERATE-SETTING]   │")
	log.Printf("[GENERATE-SETTING]   └───────────────────────────────────────────────────────────────────────────┘")
	log.Printf("[GENERATE-SETTING]")
}

// applySettingsToEnv applies a single settings entry to the env map.
// This mirrors the expandSettingsToEnv logic in kubernetes_session_manager.go.
func applySettingsToEnv(env map[string]string, s *generateSettingSettings, verbose bool) {
	if s == nil {
		return
	}

	// 1. Custom env vars (always applied regardless of auth_mode)
	if len(s.EnvVars) > 0 {
		keys := sortedKeys(s.EnvVars)
		for _, k := range keys {
			v := s.EnvVars[k]
			if verbose {
				if old, exists := env[k]; exists && old != v {
					log.Printf("[GENERATE-SETTING]     env_vars: %s=%q (旧値: %q)", k, v, old)
				} else {
					log.Printf("[GENERATE-SETTING]     env_vars: %s=%q", k, v)
				}
			}
			env[k] = v
		}
		log.Printf("[GENERATE-SETTING]     env_vars: %d 件を適用しました", len(s.EnvVars))
	}

	// 2. Auth mode and credentials
	switch s.AuthMode {
	case "bedrock":
		log.Printf("[GENERATE-SETTING]     auth_mode=bedrock → CLAUDE_CODE_USE_BEDROCK=1 を設定")
		env["CLAUDE_CODE_USE_BEDROCK"] = "1"

		if s.Bedrock != nil {
			setBedrockField(env, "ANTHROPIC_MODEL", s.Bedrock.Model, "model")
			setBedrockField(env, "AWS_ACCESS_KEY_ID", s.Bedrock.AccessKeyID, "access_key_id")
			setBedrockField(env, "AWS_SECRET_ACCESS_KEY", s.Bedrock.SecretAccessKey, "secret_access_key")
			setBedrockField(env, "AWS_ROLE_ARN", s.Bedrock.RoleARN, "role_arn")
			setBedrockField(env, "AWS_PROFILE", s.Bedrock.Profile, "profile")
		} else {
			log.Printf("[GENERATE-SETTING]     bedrock フィールドが nil のため Bedrock 資格情報はスキップ")
		}

	case "oauth":
		log.Printf("[GENERATE-SETTING]     auth_mode=oauth → CLAUDE_CODE_USE_BEDROCK=0、AWS 資格情報をクリア")
		env["CLAUDE_CODE_USE_BEDROCK"] = "0"
		env["ANTHROPIC_MODEL"] = ""
		env["AWS_ACCESS_KEY_ID"] = ""
		env["AWS_SECRET_ACCESS_KEY"] = ""
		env["AWS_ROLE_ARN"] = ""
		env["AWS_PROFILE"] = ""
		if s.ClaudeCodeOAuthToken != "" {
			log.Printf("[GENERATE-SETTING]     claude_code_oauth_token → CLAUDE_CODE_OAUTH_TOKEN を設定")
			env["CLAUDE_CODE_OAUTH_TOKEN"] = s.ClaudeCodeOAuthToken
		}

	case "":
		log.Printf("[GENERATE-SETTING]     auth_mode=\" \" (未設定) → auth 関連 env vars を変更しない")
		// Do not touch any auth-related env vars.
		// This ensures that a team's Bedrock settings are preserved when a user
		// settings entry has no auth_mode configured.
		if s.ClaudeCodeOAuthToken != "" {
			log.Printf("[GENERATE-SETTING]     claude_code_oauth_token → CLAUDE_CODE_OAUTH_TOKEN を設定 (auth_mode 無関係)")
			env["CLAUDE_CODE_OAUTH_TOKEN"] = s.ClaudeCodeOAuthToken
		}

	default:
		log.Printf("[GENERATE-SETTING]     auth_mode=%q は未知の値です。auth 関連 env vars を変更しません", s.AuthMode)
	}
}

// setBedrockField sets envKey=value if value is non-empty; otherwise logs that it is skipped.
func setBedrockField(env map[string]string, envKey, value, fieldName string) {
	if value != "" {
		log.Printf("[GENERATE-SETTING]       %s → %s=%q", fieldName, envKey, maskSecret(envKey, value))
		env[envKey] = value
	} else {
		log.Printf("[GENERATE-SETTING]       %s が空 → %s をスキップ (既存値を保持)", fieldName, envKey)
	}
}

// maskSecret masks sensitive values in log output.
func maskSecret(envKey, value string) string {
	sensitiveKeys := []string{
		"AWS_SECRET_ACCESS_KEY",
		"AWS_ACCESS_KEY_ID",
		"CLAUDE_CODE_OAUTH_TOKEN",
	}
	for _, k := range sensitiveKeys {
		if envKey == k && len(value) > 4 {
			return value[:4] + strings.Repeat("*", len(value)-4)
		}
	}
	return value
}

// ---------------------------------------------------------------------------
// Output helpers
// ---------------------------------------------------------------------------

// outputSessionSettings serializes the SessionSettings and writes it to stdout.
func outputSessionSettings(s *sessionsettings.SessionSettings, format string) error {
	// Marshal to YAML first (the canonical internal format)
	yamlBytes, err := sessionsettings.MarshalYAML(s)
	if err != nil {
		return fmt.Errorf("YAML へのシリアライズに失敗しました: %w", err)
	}

	switch format {
	case "yaml":
		fmt.Print(string(yamlBytes))
		return nil

	case "json":
		// Convert YAML → generic map → JSON for a clean JSON representation
		var generic interface{}
		if err := yaml.Unmarshal(yamlBytes, &generic); err != nil {
			return fmt.Errorf("YAML の解析に失敗しました: %w", err)
		}
		// yaml.Unmarshal returns map[interface{}]interface{} on older versions; normalize.
		normalized := normalizeYAMLValue(generic)
		jsonBytes, err := json.MarshalIndent(normalized, "", "  ")
		if err != nil {
			return fmt.Errorf("JSON へのシリアライズに失敗しました: %w", err)
		}
		fmt.Println(string(jsonBytes))
		return nil

	default:
		return fmt.Errorf("不明な出力形式: %q", format)
	}
}

// normalizeYAMLValue converts map[interface{}]interface{} (produced by yaml.v3 in some cases)
// to map[string]interface{} so that json.Marshal can handle it correctly.
func normalizeYAMLValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(val))
		for k, v2 := range val {
			m[fmt.Sprintf("%v", k)] = normalizeYAMLValue(v2)
		}
		return m
	case map[string]interface{}:
		for k, v2 := range val {
			val[k] = normalizeYAMLValue(v2)
		}
		return val
	case []interface{}:
		for i, v2 := range val {
			val[i] = normalizeYAMLValue(v2)
		}
		return val
	default:
		return val
	}
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

// readGenerateSettingInput reads input data from a file or stdin.
func readGenerateSettingInput(path string) ([]byte, error) {
	if path == "-" || path == "" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

// sortedKeys returns the keys of a string map in sorted order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
