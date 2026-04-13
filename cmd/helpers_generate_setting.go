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
// Input types – SlackBot mode (--input)
// ---------------------------------------------------------------------------

// generateSettingInput is the top-level input structure for the generate-setting command
// when using the --input (SlackBot) mode.
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

// generateSettingParams holds session-creation parameters (SlackBot mode).
type generateSettingParams struct {
	AgentType string `json:"agent_type,omitempty"`
	Oneshot   bool   `json:"oneshot,omitempty"`
}

// ---------------------------------------------------------------------------
// Input types – Schedule mode (--schedule)
// ---------------------------------------------------------------------------

// generateSettingSchedule represents a Schedule configuration.
// Matches the JSON schema used by POST /schedules.
type generateSettingSchedule struct {
	Name          string                          `json:"name,omitempty"`
	UserID        string                          `json:"user_id,omitempty"`
	Scope         string                          `json:"scope,omitempty"` // "user" or "team"
	TeamID        string                          `json:"team_id,omitempty"`
	Teams         []string                        `json:"teams,omitempty"`
	CronExpr      string                          `json:"cron_expr,omitempty"`
	ScheduledAt   string                          `json:"scheduled_at,omitempty"` // ISO8601 string
	Timezone      string                          `json:"timezone,omitempty"`
	SessionConfig *generateSettingScheduleSession `json:"session_config,omitempty"`
}

// generateSettingScheduleSession is the session_config block inside a Schedule.
type generateSettingScheduleSession struct {
	Environment map[string]string              `json:"environment,omitempty"`
	Tags        map[string]string              `json:"tags,omitempty"`
	Params      *generateSettingScheduleParams `json:"params,omitempty"`
}

// generateSettingScheduleParams holds session-creation parameters (Schedule mode).
type generateSettingScheduleParams struct {
	Message                  string `json:"message,omitempty"`
	AgentType                string `json:"agent_type,omitempty"`
	Oneshot                  bool   `json:"oneshot,omitempty"`
	InitialMessageWaitSecond *int   `json:"initial_message_wait_second,omitempty"`
}

// ---------------------------------------------------------------------------
// Settings type (shared between all modes)
// ---------------------------------------------------------------------------

// generateSettingSettings represents user or team settings that influence the session.
// Applied in order (index 0 = lowest priority, last entry = highest priority).
// Typically: base → teams… → user.
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
	generateSettingInputPath    string
	generateSettingSchedulePath string
	generateSettingUserSetting  string
	generateSettingTeamSettings []string
	generateSettingOutputFmt    string
	generateSettingVerbose      bool
)

// ---------------------------------------------------------------------------
// Cobra command definition
// ---------------------------------------------------------------------------

var generateSettingCmd = &cobra.Command{
	Use:   "generate-setting",
	Short: "SlackBot / Schedule 設定から session settings JSON を組み立てて出力する",
	Long: `SlackBot または Schedule の設定をもとに、実際のセッション起動時と同じ手順で
session settings を組み立て、その過程をログに出しながら最終的な JSON / YAML を標準出力します。

手順ログには以下が含まれます:
  1. team settings の適用 (--team-setting の順で低→高優先度)
  2. user settings の適用 (--user-setting)
  3. Bedrock 設定を env に入れる際の判断基準
  4. session_config.environment の上書き適用 (最高優先度)

■ Schedule モード (--schedule)

  スケジュール設定 JSON を直接指定します。
  --team-setting と --user-setting で対応する settings ファイルを指定してください。

  スケジュール JSON の形式:
  {
    "name": "daily-review",
    "user_id": "alice",
    "scope": "user",
    "teams": ["myorg/backend-team"],
    "cron_expr": "0 9 * * 1-5",
    "timezone": "Asia/Tokyo",
    "session_config": {
      "environment": { "MY_VAR": "value" },
      "tags": { "repo": "myorg/myrepo" },
      "params": {
        "agent_type": "claude-agentapi",
        "message": "Run daily checks",
        "oneshot": true
      }
    }
  }

■ SlackBot モード (--input、デフォルト)

  SlackBot 設定と settings を一つの JSON にまとめて指定します。
  settings 配列と --team-setting / --user-setting フラグの両方が使えます。

  {
    "slackbot": {
      "user_id": "alice",
      "scope": "user",
      "teams": ["myorg/backend-team"],
      "session_config": {
        "environment": { "MY_VAR": "value" },
        "initial_message_template": "Hello!",
        "params": { "agent_type": "claude-agentapi", "oneshot": false }
      }
    },
    "settings": [...]
  }

■ settings ファイルの形式 (--user-setting / --team-setting)

  {
    "name": "alice",
    "auth_mode": "bedrock",
    "bedrock": {
      "enabled": true,
      "model": "anthropic.claude-3-5-sonnet-20241022-v2:0",
      "role_arn": "arn:aws:iam::123456789012:role/bedrock-role"
    },
    "env_vars": { "MY_KEY": "my_value" }
  }

使用例:
  # Schedule モード
  agentapi-proxy helpers generate-setting \
    --schedule schedule.json \
    --team-setting team-settings.json \
    --user-setting user-settings.json

  # 複数チーム設定 (--team-setting は繰り返し可)
  agentapi-proxy helpers generate-setting \
    --schedule schedule.json \
    --team-setting base-team.json \
    --team-setting backend-team.json \
    --user-setting alice-settings.json

  # SlackBot モード (既存)
  agentapi-proxy helpers generate-setting \
    --input slackbot-config.json \
    --team-setting team-settings.json \
    --user-setting user-settings.json

  # YAML 形式で出力
  agentapi-proxy helpers generate-setting --schedule schedule.json --format yaml`,
	RunE: runGenerateSetting,
}

func init() {
	generateSettingCmd.Flags().StringVarP(&generateSettingInputPath, "input", "i", "",
		"SlackBot モード: 入力 JSON ファイルパス (\"-\" で標準入力)。--schedule 未指定時はデフォルト stdin")
	generateSettingCmd.Flags().StringVar(&generateSettingSchedulePath, "schedule", "",
		"Schedule モード: スケジュール設定 JSON ファイルパス (指定時は --input より優先)")
	generateSettingCmd.Flags().StringVar(&generateSettingUserSetting, "user-setting", "",
		"ユーザー settings JSON ファイルパス (最高優先度で適用)")
	generateSettingCmd.Flags().StringArrayVar(&generateSettingTeamSettings, "team-setting", nil,
		"チーム settings JSON ファイルパス (複数回指定可、指定順で低→高優先度)")
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
	if generateSettingOutputFmt != "json" && generateSettingOutputFmt != "yaml" {
		return fmt.Errorf("invalid format %q: must be json or yaml", generateSettingOutputFmt)
	}

	if generateSettingSchedulePath != "" {
		return runGenerateSettingSchedule()
	}
	return runGenerateSettingSlackBot(cmd)
}

// ---------------------------------------------------------------------------
// Schedule mode
// ---------------------------------------------------------------------------

func runGenerateSettingSchedule() error {
	log.Printf("[GENERATE-SETTING] モード: Schedule")
	log.Printf("[GENERATE-SETTING] ▶ スケジュール設定 JSON を読み込んでいます: %s", generateSettingSchedulePath)

	data, err := os.ReadFile(generateSettingSchedulePath)
	if err != nil {
		return fmt.Errorf("スケジュール設定の読み込みに失敗しました: %w", err)
	}

	var sched generateSettingSchedule
	if err := json.Unmarshal(data, &sched); err != nil {
		return fmt.Errorf("スケジュール設定 JSON の解析に失敗しました: %w", err)
	}

	// Log schedule summary
	log.Printf("[GENERATE-SETTING] スケジュール情報:")
	log.Printf("[GENERATE-SETTING]   name=%q", sched.Name)
	log.Printf("[GENERATE-SETTING]   user_id=%q scope=%q team_id=%q", sched.UserID, sched.Scope, sched.TeamID)
	if len(sched.Teams) > 0 {
		log.Printf("[GENERATE-SETTING]   teams=%v", sched.Teams)
	}
	if sched.CronExpr != "" {
		log.Printf("[GENERATE-SETTING]   cron_expr=%q timezone=%q", sched.CronExpr, sched.Timezone)
	}
	if sched.ScheduledAt != "" {
		log.Printf("[GENERATE-SETTING]   scheduled_at=%q", sched.ScheduledAt)
	}

	// Collect settings from flags
	settingsList, err := loadSettingsFromFlags(nil)
	if err != nil {
		return err
	}

	// Build env map
	env := make(map[string]string)
	applyAllSettings(env, settingsList)

	// Apply session_config.environment (highest priority)
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ▶ 手順 3: session_config.environment の適用 (最高優先度)")
	var sessionEnv map[string]string
	var agentType, initialMessage string
	var oneshot bool
	if sched.SessionConfig != nil {
		sessionEnv = sched.SessionConfig.Environment
		if sched.SessionConfig.Params != nil {
			agentType = sched.SessionConfig.Params.AgentType
			oneshot = sched.SessionConfig.Params.Oneshot
			initialMessage = sched.SessionConfig.Params.Message
		}
	}
	applySessionEnv(env, sessionEnv)

	// Build SessionSettings
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ▶ 手順 4: SessionSettings の組み立て")

	scope := sched.Scope
	if scope == "" {
		scope = "user"
	}

	result := &sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{
			UserID:    sched.UserID,
			Scope:     scope,
			TeamID:    sched.TeamID,
			AgentType: agentType,
			Oneshot:   oneshot,
			Teams:     sched.Teams,
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
	result.Startup = buildStartupConfig(agentType)
	logSessionSettingsSummary(result)

	return outputSessionSettings(result, generateSettingOutputFmt)
}

// ---------------------------------------------------------------------------
// SlackBot mode
// ---------------------------------------------------------------------------

func runGenerateSettingSlackBot(cmd *cobra.Command) error {
	log.Printf("[GENERATE-SETTING] モード: SlackBot")

	// Determine input source
	inputPath := generateSettingInputPath
	if inputPath == "" {
		inputPath = "-" // default to stdin
	}

	log.Printf("[GENERATE-SETTING] ▶ 入力 JSON を読み込んでいます: %s", func() string {
		if inputPath == "-" {
			return "stdin"
		}
		return inputPath
	}())

	inputData, err := readGenerateSettingInput(inputPath)
	if err != nil {
		return fmt.Errorf("入力の読み込みに失敗しました: %w", err)
	}

	var input generateSettingInput
	if err := json.Unmarshal(inputData, &input); err != nil {
		return fmt.Errorf("入力 JSON の解析に失敗しました: %w", err)
	}

	if input.SlackBot == nil {
		return fmt.Errorf("入力 JSON に \"slackbot\" フィールドが必要です (--schedule を使うと Schedule モードになります)")
	}

	bot := input.SlackBot
	log.Printf("[GENERATE-SETTING] SlackBot: user_id=%q scope=%q team_id=%q teams=%v",
		bot.UserID, bot.Scope, bot.TeamID, bot.Teams)
	log.Printf("[GENERATE-SETTING] 入力内 settings エントリ数: %d", len(input.Settings))

	// Collect settings: embedded + flags (flags are higher priority)
	settingsList, err := loadSettingsFromFlags(input.Settings)
	if err != nil {
		return err
	}

	// Build env map
	env := make(map[string]string)
	applyAllSettings(env, settingsList)

	// Apply session_config.environment (highest priority)
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ▶ 手順 3: SlackBot session_config.environment の適用 (最高優先度)")
	var sessionEnv map[string]string
	var agentType, initialMessage string
	var oneshot bool
	if bot.SessionConfig != nil {
		sessionEnv = bot.SessionConfig.Environment
		initialMessage = bot.SessionConfig.InitialMessageTemplate
		if bot.SessionConfig.Params != nil {
			agentType = bot.SessionConfig.Params.AgentType
			oneshot = bot.SessionConfig.Params.Oneshot
		}
	}
	applySessionEnv(env, sessionEnv)

	// Build SessionSettings
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ▶ 手順 4: SessionSettings の組み立て")

	scope := bot.Scope
	if scope == "" {
		scope = "user"
	}

	result := &sessionsettings.SessionSettings{
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
	result.Startup = buildStartupConfig(agentType)
	logSessionSettingsSummary(result)

	return outputSessionSettings(result, generateSettingOutputFmt)
}

// ---------------------------------------------------------------------------
// Settings loading helpers
// ---------------------------------------------------------------------------

// loadSettingsFromFlags builds the final ordered settings list.
// Order (lowest → highest priority):
//  1. embedded settings (from --input JSON's settings array)
//  2. --team-setting files (in the order specified)
//  3. --user-setting file
func loadSettingsFromFlags(embedded []*generateSettingSettings) ([]*generateSettingSettings, error) {
	var list []*generateSettingSettings

	// 1. Embedded settings (lowest priority)
	list = append(list, embedded...)

	// 2. --team-setting files
	for _, path := range generateSettingTeamSettings {
		s, err := loadSettingsFile(path)
		if err != nil {
			return nil, fmt.Errorf("--team-setting %q の読み込みに失敗しました: %w", path, err)
		}
		log.Printf("[GENERATE-SETTING]   --team-setting %q を読み込みました (name=%q)", path, s.Name)
		list = append(list, s)
	}

	// 3. --user-setting file (highest among settings)
	if generateSettingUserSetting != "" {
		s, err := loadSettingsFile(generateSettingUserSetting)
		if err != nil {
			return nil, fmt.Errorf("--user-setting %q の読み込みに失敗しました: %w", generateSettingUserSetting, err)
		}
		log.Printf("[GENERATE-SETTING]   --user-setting %q を読み込みました (name=%q)", generateSettingUserSetting, s.Name)
		list = append(list, s)
	}

	return list, nil
}

// loadSettingsFile reads and parses a settings JSON file.
func loadSettingsFile(path string) (*generateSettingSettings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s generateSettingSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("JSON 解析エラー: %w", err)
	}
	return &s, nil
}

// applyAllSettings applies the full ordered settings list to the env map with logging.
func applyAllSettings(env map[string]string, list []*generateSettingSettings) {
	total := len(list)
	if total == 0 {
		log.Printf("[GENERATE-SETTING]")
		log.Printf("[GENERATE-SETTING] ▶ 手順 1: settings の適用 (設定なし)")
		printBedrockCriteria()
		return
	}

	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ▶ 手順 1: settings の適用 (優先度: 低→高、計 %d 件)", total)
	printBedrockCriteria()

	for idx, s := range list {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("<settings[%d]>", idx)
		}
		log.Printf("[GENERATE-SETTING]   [%d/%d] settings 適用: name=%q", idx+1, total, name)
		applySettingsToEnv(env, s, generateSettingVerbose)
	}
}

// applySessionEnv applies session_config.environment to the env map (highest priority).
func applySessionEnv(env map[string]string, sessionEnv map[string]string) {
	if len(sessionEnv) == 0 {
		log.Printf("[GENERATE-SETTING]   (session_config.environment は空です)")
		return
	}
	keys := sortedKeys(sessionEnv)
	for _, k := range keys {
		v := sessionEnv[k]
		if old, exists := env[k]; exists && old != v {
			log.Printf("[GENERATE-SETTING]   上書き: %s=%q (旧値: %q)", k, v, old)
		} else {
			log.Printf("[GENERATE-SETTING]   設定: %s=%q", k, v)
		}
		env[k] = v
	}
}

// buildStartupConfig returns the startup command config for the given agent type.
func buildStartupConfig(agentType string) sessionsettings.StartupConfig {
	switch agentType {
	case "claude-agentapi":
		log.Printf("[GENERATE-SETTING]   startup.command: [claude-agentapi]")
		return sessionsettings.StartupConfig{
			Command: []string{"claude-agentapi"},
		}
	case "claude-acp":
		// acp-server bridges claude-agent-acp (ACP over stdio) to the agentapi HTTP interface.
		// Port is determined at runtime via AGENTAPI_PORT env var.
		log.Printf("[GENERATE-SETTING]   startup.command: [agentapi-proxy acp-server -- bunx @agentclientprotocol/claude-agent-acp]")
		return sessionsettings.StartupConfig{
			Command: []string{"agentapi-proxy"},
			Args:    []string{"acp-server", "--", "bunx", "@agentclientprotocol/claude-agent-acp"},
		}
	default:
		log.Printf("[GENERATE-SETTING]   startup.command: [agentapi server --allowed-hosts * --allowed-origins *]")
		return sessionsettings.StartupConfig{
			Command: []string{"agentapi", "server"},
			Args:    []string{"--allowed-hosts", "*", "--allowed-origins", "*"},
		}
	}
}

// logSessionSettingsSummary logs a summary of the built SessionSettings.
func logSessionSettingsSummary(s *sessionsettings.SessionSettings) {
	log.Printf("[GENERATE-SETTING]   session.user_id=%q scope=%q agent_type=%q oneshot=%v",
		s.Session.UserID, s.Session.Scope, s.Session.AgentType, s.Session.Oneshot)
	log.Printf("[GENERATE-SETTING]   env 変数数: %d", len(s.Env))
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ✓ SessionSettings の組み立て完了")
}

// ---------------------------------------------------------------------------
// Bedrock judgment logic (mirrors expandSettingsToEnv in kubernetes_session_manager.go)
// ---------------------------------------------------------------------------

// printBedrockCriteria logs the Bedrock judgment criteria that govern how Bedrock
// settings are translated into environment variables.
func printBedrockCriteria() {
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING]   ┌─ Bedrock 設定を env に入れる際の判断基準 ─────────────────────────────────┐")
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
	log.Printf("[GENERATE-SETTING]   │  優先度: --team-setting(複数、順)→ --user-setting → session_config.environment")
	log.Printf("[GENERATE-SETTING]   │")
	log.Printf("[GENERATE-SETTING]   └────────────────────────────────────────────────────────────────────────────┘")
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
		log.Printf("[GENERATE-SETTING]     auth_mode=\"\" (未設定) → auth 関連 env vars を変更しない")
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
		// yaml.Unmarshal returns map[interface{}]interface{} in some cases; normalize.
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
