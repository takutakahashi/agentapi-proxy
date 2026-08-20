import {
  Bell,
  Blocks,
  Bot,
  FolderKey,
  Github,
  KeyRound,
  LucideIcon,
  Plug,
  Server,
  Sparkles,
  Store,
  Terminal,
  Ticket,
} from 'lucide-react'
import { SettingsData } from '@/types/settings'

export type SettingsScopeKind = 'personal' | 'team'

export interface SettingsNavItem {
  /** ルート末尾のセグメント */
  slug: string
  label: string
  icon: LucideIcon
  group: string
  scopes: SettingsScopeKind[]
  /**
   * このページが編集する SettingsData のフィールド。
   * サイドバーの未保存ドットと保存バーの変更ページ名に使う。
   * 専用 API で即時反映するページは空にする。
   */
  fields: (keyof SettingsData)[]
}

/**
 * 設定ページの一覧。1 機能 = 1 項目 = 1 ページ。
 * 並び順がそのままサイドバーの並び順になる。
 */
export const settingsNavItems: SettingsNavItem[] = [
  {
    slug: 'notifications',
    label: '通知',
    icon: Bell,
    group: '',
    scopes: ['personal'],
    fields: ['slack_user_id', 'notification_channels'],
  },
  {
    slug: 'ai-providers',
    label: 'AI プロバイダ',
    icon: Sparkles,
    group: 'AI とエージェント',
    scopes: ['personal', 'team'],
    fields: ['bedrock', 'claude_code_oauth_token', 'auth_mode', 'preferred_team_id'],
  },
  {
    slug: 'agents',
    label: 'エージェント',
    icon: Bot,
    group: 'AI とエージェント',
    scopes: ['personal', 'team'],
    fields: ['default_agent_type'],
  },
  {
    slug: 'codex-auth',
    label: 'Codex 認証',
    icon: KeyRound,
    group: 'AI とエージェント',
    scopes: ['personal', 'team'],
    fields: [],
  },
  {
    slug: 'mcp-servers',
    label: 'MCP サーバー',
    icon: Plug,
    group: 'セッション環境',
    scopes: ['personal', 'team'],
    fields: ['mcp_servers'],
  },
  {
    slug: 'env-vars',
    label: '環境変数',
    icon: Terminal,
    group: 'セッション環境',
    scopes: ['personal', 'team'],
    fields: ['env_vars'],
  },
  {
    slug: 'files',
    label: 'セッションファイル',
    icon: FolderKey,
    group: 'セッション環境',
    scopes: ['personal'],
    fields: [],
  },
  {
    slug: 'marketplaces',
    label: 'Marketplace',
    icon: Store,
    group: '拡張機能',
    scopes: ['personal', 'team'],
    fields: ['marketplaces'],
  },
  {
    slug: 'plugins',
    label: 'Plugins',
    icon: Blocks,
    group: '拡張機能',
    scopes: ['personal', 'team'],
    fields: ['enabled_plugins'],
  },
  {
    slug: 'api-tokens',
    label: 'API トークン',
    icon: Ticket,
    group: 'アクセス',
    scopes: ['personal', 'team'],
    fields: [],
  },
  {
    slug: 'github-app',
    label: 'GitHub App 認証',
    icon: Github,
    group: 'アクセス',
    scopes: ['team'],
    fields: ['github_app_installation_id'],
  },
  {
    slug: 'session-managers',
    label: 'セッションマネージャー',
    icon: Server,
    group: 'アクセス',
    scopes: ['personal', 'team'],
    fields: ['external_session_managers'],
  },
]

/** スコープを選んだときに最初に開くページ */
export const DEFAULT_SETTINGS_SLUG = 'ai-providers'

export const navItemsForScope = (scope: SettingsScopeKind): SettingsNavItem[] =>
  settingsNavItems.filter((item) => item.scopes.includes(scope))

/** スコープのルート（末尾に slug を足して使う） */
export const scopeBasePath = (scope: SettingsScopeKind, teamId?: string): string =>
  scope === 'personal' ? '/settings/personal' : `/settings/team/${encodeURIComponent(teamId ?? '')}`

export const settingsHref = (scope: SettingsScopeKind, slug: string, teamId?: string): string =>
  `${scopeBasePath(scope, teamId)}/${slug}`

/** 変更されたフィールドを含むページのラベル一覧 */
export const pagesForDirtyFields = (
  scope: SettingsScopeKind,
  dirtyFields: (keyof SettingsData)[]
): string[] => {
  const dirty = new Set(dirtyFields)
  return navItemsForScope(scope)
    .filter((item) => item.fields.some((field) => dirty.has(field)))
    .map((item) => item.label)
}
