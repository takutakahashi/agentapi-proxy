import {
  Bell,
  Cable,
  Cpu,
  Database,
  Github,
  Layers,
  Lock,
  type LucideIcon,
  MessageSquare,
  Boxes,
  History,
  ShieldCheck,
  Sparkles,
} from 'lucide-react'

export type AdminFieldType = 'text' | 'secret' | 'number' | 'toggle' | 'textarea' | 'select' | 'team-role-mapping'

export interface AdminField {
  path: string
  label: string
  description?: string
  type: AdminFieldType
  placeholder?: string
  options?: { label: string; value: string }[]
}

export interface AdminSection {
  id: string
  title: string
  description: string
  /** サイドバーのグループ見出し */
  group: string
  icon: LucideIcon
  fields: AdminField[]
}

export const adminSections: AdminSection[] = [
  {
    id: 'agents', title: 'AI・Agent', description: '全ユーザーのAI認証、MCP、Plugin、環境変数の既定値',
    group: 'AI とエージェント', icon: Sparkles,
    fields: [
      { path: 'auth_mode', label: '既定の認証Mode', type: 'select', options: [{ label: 'OAuth', value: 'oauth' }, { label: 'AWS Bedrock', value: 'bedrock' }] },
      { path: 'bedrock_enabled', label: 'AWS Bedrock', type: 'toggle' },
      { path: 'bedrock_model', label: 'Bedrock Model', type: 'text' },
      { path: 'bedrock_role_arn', label: 'Bedrock Role ARN', type: 'text' },
      { path: 'mcp_servers', label: 'MCP Servers (JSON)', type: 'textarea', placeholder: '{"github":{"type":"http","url":"https://..."}}' },
      { path: 'marketplaces', label: 'Plugin Marketplaces (JSON)', type: 'textarea' },
      { path: 'enabled_plugins', label: 'Enabled Plugins', type: 'textarea', description: '1行に1つ plugin@marketplace を指定します。' },
      { path: 'environment_variables', label: '共通Environment Variables (JSON)', type: 'textarea' },
    ],
  },
  {
    id: 'github', title: 'GitHub', description: 'GitHub OAuth、Enterprise、GitHub App の共通設定',
    group: '認証と権限', icon: Github,
    fields: [
      { path: 'oauth.enabled', label: 'GitHub OAuth', type: 'toggle' },
      { path: 'oauth.client_id', label: 'OAuth Client ID', type: 'text' },
      { path: 'oauth.client_secret', label: 'OAuth Client Secret', type: 'secret', description: '空欄のまま保存すると現在の値を維持します。' },
      { path: 'oauth.scope', label: 'OAuth Scope', type: 'text', placeholder: 'read:user read:org project' },
      { path: 'oauth.allowed_redirect_uris', label: '許可するRedirect URI', type: 'textarea' },
      { path: 'enterprise.enabled', label: 'GitHub Enterprise', type: 'toggle' },
      { path: 'enterprise.base_url', label: 'Enterprise Base URL', type: 'text' },
      { path: 'enterprise.api_url', label: 'Enterprise API URL', type: 'text' },
      { path: 'app.id', label: 'GitHub App ID', type: 'text' },
      { path: 'app.installation_id', label: 'Installation ID', type: 'text' },
      { path: 'app.private_key', label: 'GitHub App Private Key', type: 'secret' },
      { path: 'app.repository_restriction', label: '対象リポジトリに制限', type: 'toggle' },
    ],
  },
  {
    id: 'authentication', title: '認証・認可', description: 'ログイン方式とGitHub Teamベースの権限設定',
    group: '認証と権限', icon: ShieldCheck,
    fields: [
      { path: 'static.enabled', label: 'Static API Key認証', type: 'toggle' },
      { path: 'static.header_name', label: 'API Key Header', type: 'text', placeholder: 'X-API-Key' },
      { path: 'allow_users_without_team', label: 'Team未所属ユーザーを許可', type: 'toggle' },
      { path: 'default_role', label: '既定Role', type: 'text', placeholder: 'user' },
      { path: 'default_permissions', label: '既定Permissions', type: 'textarea', description: '1行に1つ指定します。' },
      { path: 'team_role_mapping', label: 'Team Role Mapping', type: 'team-role-mapping', description: 'GitHub TeamごとにRoleとPermissionを設定します。' },
    ],
  },
  {
    id: 'slack', title: 'Slack', description: 'システム共通Slack Appの資格情報と動作設定',
    group: '連携', icon: MessageSquare,
    fields: [
      { path: 'bot_token', label: 'Bot Token', type: 'secret', placeholder: 'xoxb-...' },
      { path: 'app_token', label: 'App Token', type: 'secret', placeholder: 'xapp-...' },
      { path: 'signing_secret', label: 'Signing Secret', type: 'secret' },
      { path: 'cleanup_enabled', label: 'Slackbot Session Cleanup', type: 'toggle' },
      { path: 'session_ttl', label: 'Session TTL', type: 'text', placeholder: '72h' },
      { path: 'cleanup_check_interval', label: 'Cleanup間隔', type: 'text', placeholder: '1h' },
      { path: 'cleanup_dry_run', label: 'Cleanup Dry-run', type: 'toggle' },
    ],
  },
  {
    id: 'notifications', title: '通知', description: 'Web Pushと通知リンクの設定',
    group: '連携', icon: Bell,
    fields: [
      { path: 'base_url', label: '通知Base URL', type: 'text' },
      { path: 'vapid_public_key', label: 'VAPID Public Key', type: 'text' },
      { path: 'vapid_private_key', label: 'VAPID Private Key', type: 'secret' },
      { path: 'vapid_contact_email', label: 'VAPID Contact Email', type: 'text' },
      { path: 'webhook_base_url', label: 'Webhook Base URL', type: 'text' },
      { path: 'github_enterprise_host', label: 'Webhook GitHub Enterprise Host', type: 'text' },
    ],
  },
  {
    id: 'workers', title: 'Workers', description: 'スケジュール実行とウォームプール',
    group: '実行基盤', icon: Cpu,
    fields: [
      { path: 'schedule.enabled', label: 'Schedule Worker', type: 'toggle' },
      { path: 'schedule.check_interval', label: 'Schedule確認間隔', type: 'text', placeholder: '30s' },
      { path: 'stock.enabled', label: 'Stock Inventory Worker', type: 'toggle' },
      { path: 'stock.check_interval', label: 'Stock確認間隔', type: 'text', placeholder: '30s' },
      { path: 'stock.target_count', label: '事前起動セッション数', type: 'number' },
      { path: 'stock.docker_enabled', label: 'Docker対応Stock', type: 'toggle' },
    ],
  },
  {
    id: 'sessions', title: 'セッション', description: '全セッションに適用する実行環境の既定値',
    group: '実行基盤', icon: Boxes,
    fields: [
      { path: 'image', label: 'Session Image', type: 'text' },
      { path: 'cpu_request', label: 'CPU Request', type: 'text', placeholder: '500m' },
      { path: 'cpu_limit', label: 'CPU Limit', type: 'text', placeholder: '2' },
      { path: 'memory_request', label: 'Memory Request', type: 'text', placeholder: '512Mi' },
      { path: 'memory_limit', label: 'Memory Limit', type: 'text', placeholder: '4Gi' },
      { path: 'pvc_enabled', label: 'Session PVC', type: 'toggle' },
      { path: 'pvc_storage_class', label: 'Storage Class', type: 'text' },
      { path: 'pvc_size', label: 'PVC Size', type: 'text', placeholder: '10Gi' },
      { path: 'pod_start_timeout', label: 'Pod Start Timeout (秒)', type: 'number' },
      { path: 'pod_stop_timeout', label: 'Pod Stop Timeout (秒)', type: 'number' },
      { path: 'claude_args', label: 'Claude Code起動引数', type: 'text' },
      { path: 'otel_enabled', label: 'OpenTelemetry Collector', type: 'toggle' },
    ],
  },
  {
    id: 'storage', title: 'ストレージ', description: 'KV、使用量、Redis、セッション永続化',
    group: '実行基盤', icon: Database,
    fields: [
      { path: 'backend', label: 'KV Backend', type: 'select', options: [{ label: 'Kubernetes', value: 'kubernetes' }, { label: 'libSQL', value: 'libsql' }] },
      { path: 'database_url', label: 'Database URL', type: 'secret' },
      { path: 'database_auth_token', label: 'Database Auth Token', type: 'secret' },
      { path: 'usage_enabled', label: 'Usage保存', type: 'toggle' },
      { path: 'redis_enabled', label: 'Redis', type: 'toggle' },
      { path: 'redis_address', label: 'External Redis Address', type: 'text' },
      { path: 'redis_password', label: 'Redis Password', type: 'secret' },
      { path: 'redis_tls_enabled', label: 'Redis TLS', type: 'toggle' },
      { path: 'session_persistence_backend', label: 'Session Persistence', type: 'select', options: [{ label: 'Disabled', value: '' }, { label: 'Volume', value: 'volume' }, { label: 'S3', value: 's3' }] },
      { path: 'session_persistence_bucket', label: 'S3 Bucket', type: 'text' },
    ],
  },
  {
    id: 'integrations', title: '外部連携', description: 'Google、Todoist、SCIAの共通設定',
    group: '連携', icon: Cable,
    fields: [
      { path: 'scia_enabled', label: 'SCIA', type: 'toggle' },
      { path: 'google_client_id', label: 'Google Client ID', type: 'text' },
      { path: 'google_client_secret', label: 'Google Client Secret', type: 'secret' },
      { path: 'google_scopes', label: 'Google Scopes', type: 'textarea' },
      { path: 'todoist_enabled', label: 'Todoist', type: 'toggle' },
      { path: 'todoist_client_id', label: 'Todoist Client ID', type: 'text' },
      { path: 'todoist_client_secret', label: 'Todoist Client Secret', type: 'secret' },
    ],
  },
  {
    id: 'security', title: 'セキュリティ', description: '保存データの暗号化とネットワーク制御',
    group: 'セキュリティ', icon: Lock,
    fields: [
      { path: 'encryption_key', label: 'Encryption Key', type: 'secret' },
      { path: 'network_filter_image', label: 'Network Filter Image', type: 'text' },
      { path: 'session_control_enabled', label: 'Session Control', type: 'toggle' },
      { path: 'direct_runtime_enabled', label: 'Direct Session Runtime', type: 'toggle' },
    ],
  },
]

export const getAdminSection = (id: string) => adminSections.find((section) => section.id === id)

/** 設定セクション以外のページ。サイドバーには同じ体裁で並ぶ */
export interface AdminNavExtra {
  href: string
  title: string
  group: string
  icon: LucideIcon
}

export const adminNavExtras: AdminNavExtra[] = [
  { href: '/admin/pools', title: 'Session Pools', group: '実行基盤', icon: Layers },
  { href: '/admin/history', title: '変更履歴', group: '運用', icon: History },
]
