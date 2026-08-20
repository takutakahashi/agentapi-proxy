'use client'

import { use, useEffect, useState } from 'react'
import { notFound } from 'next/navigation'
import { createCurrentDeploymentAgentAPIProxyClient } from '@/lib/agentapi-proxy-client'
import { AdminSettingsDocument, AdminSettingsSections } from '@/types/admin-settings'
import { getAdminSection, AdminField } from '../config'
import { useToast } from '@/contexts/ToastContext'
import {
  BedrockSettings,
  EnvVarsSettings,
  FieldGroup,
  FieldRow,
  MarketplaceSettings,
  MCPServerSettings,
  PluginSettings,
  SelectField,
  SettingsPageHeader,
  SettingsSubsection,
  TextField,
  ToggleSwitch,
} from '@/components/settings'
import { AlertTriangle } from 'lucide-react'
import { APIMCPServerConfig, BedrockConfig, MarketplaceConfig } from '@/types/settings'

function getValue(section: Record<string, unknown>, path: string): unknown {
  return path.split('.').reduce<unknown>((value, key) => value && typeof value === 'object' ? (value as Record<string, unknown>)[key] : undefined, section)
}

function setValue(section: Record<string, unknown>, path: string, value: unknown): Record<string, unknown> {
  const copy = structuredClone(section)
  const parts = path.split('.')
  let current = copy
  parts.slice(0, -1).forEach((part) => {
    if (!current[part] || typeof current[part] !== 'object') current[part] = {}
    current = current[part] as Record<string, unknown>
  })
  current[parts[parts.length - 1]] = value
  return copy
}

function recordValue<T>(value: unknown): Record<string, T> {
  try {
    const parsed = typeof value === 'string' ? JSON.parse(value) : value
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed as Record<string, T> : {}
  } catch { return {} }
}

function stringArrayValue(value: unknown): string[] {
  if (Array.isArray(value)) return value.filter((item): item is string => typeof item === 'string')
  if (typeof value === 'string') return value.split('\n').map((item) => item.trim()).filter(Boolean)
  return []
}

function AgentsSettingsFields({ section, onChange }: { section: Record<string, unknown>; onChange: (section: Record<string, unknown>) => void }) {
  const marketplaces = recordValue<MarketplaceConfig>(section.marketplaces)
  const mcpServers = recordValue<APIMCPServerConfig>(section.mcp_servers)
  const envVars = recordValue<string>(section.env_vars ?? section.environment_variables)
  const enabledPlugins = stringArrayValue(section.enabled_plugins)
  const bedrockValue = recordValue<unknown>(section.bedrock)
  const bedrock: BedrockConfig = {
    enabled: typeof bedrockValue.enabled === 'boolean' ? bedrockValue.enabled : Boolean(section.bedrock_enabled),
    model: typeof bedrockValue.model === 'string' ? bedrockValue.model : typeof section.bedrock_model === 'string' ? section.bedrock_model : '',
  }
  const update = (key: string, value: unknown) => onChange({ ...section, [key]: value })
  const updateBedrock = (value: BedrockConfig) => {
    const legacyRoleArn = typeof bedrockValue.role_arn === 'string' ? bedrockValue.role_arn : section.bedrock_role_arn
    const next: Record<string, unknown> = { ...section, bedrock: { ...value, ...(typeof legacyRoleArn === 'string' && legacyRoleArn ? { role_arn: legacyRoleArn } : {}) } }
    delete next.bedrock_enabled; delete next.bedrock_model; delete next.bedrock_role_arn
    onChange(next)
  }
  const updateEnvVars = (updates: Record<string, string>) => {
    const next = { ...envVars }
    Object.entries(updates).forEach(([key, value]) => value === '' ? delete next[key] : next[key] = value)
    const updated: Record<string, unknown> = { ...section, env_vars: next }
    delete updated.environment_variables
    onChange(updated)
  }

  return <div>
    <SettingsSubsection title="AI プロバイダ" description="全ユーザーの既定となる認証方式と Bedrock モデル">
      <div className="space-y-6">
        <div>
          <label className="mb-2 block text-sm font-medium text-gray-900 dark:text-white">既定の認証モード</label>
          <div className="grid gap-3 sm:grid-cols-2">
            {[['oauth', 'OAuth'], ['bedrock', 'AWS Bedrock']].map(([value, label]) => (
              <label key={value} className={`cursor-pointer rounded-lg border p-4 text-sm ${section.auth_mode === value ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20' : 'border-gray-200 dark:border-gray-700'}`}>
                <input type="radio" name="admin-agent-auth-mode" value={value} checked={section.auth_mode === value} onChange={() => update('auth_mode', value)} className="mr-2" />
                {label}
              </label>
            ))}
          </div>
        </div>
        <BedrockSettings config={bedrock} onChange={updateBedrock} />
      </div>
    </SettingsSubsection>
    <SettingsSubsection title="Marketplace" description="プラグインの既定の配布元">
      <MarketplaceSettings marketplaces={marketplaces} onChange={(value) => update('marketplaces', value)} />
    </SettingsSubsection>
    <SettingsSubsection title="Plugins" description="既定で有効にするプラグイン">
      <PluginSettings enabledPlugins={enabledPlugins} availableMarketplaces={Object.keys(marketplaces)} onChange={(value) => update('enabled_plugins', value)} />
    </SettingsSubsection>
    <SettingsSubsection title="MCP サーバー" description="既定で利用できる Model Context Protocol サーバー">
      <MCPServerSettings servers={mcpServers} onChange={(value) => update('mcp_servers', value)} />
    </SettingsSubsection>
    <SettingsSubsection title="環境変数" description="全セッションに渡す既定の環境変数">
      <EnvVarsSettings envVarKeys={Object.keys(envVars)} onChange={updateEnvVars} />
    </SettingsSubsection>
  </div>
}

interface TeamRoleRule {
  role: string
  permissions: string[]
  env_file?: string
}

interface TeamRoleRow extends TeamRoleRule {
  team: string
}

function parseTeamRoleRows(value: unknown): { rows: TeamRoleRow[]; error?: string } {
  if (value === undefined || value === null || value === '') return { rows: [] }
  try {
    let parsed = value
    for (let depth = 0; depth < 2 && typeof parsed === 'string'; depth++) parsed = JSON.parse(parsed)
    const normalizePermissions = (raw: unknown): string[] => {
      if (Array.isArray(raw)) return raw.filter((item): item is string => typeof item === 'string').map((item) => item.trim()).filter(Boolean)
      if (typeof raw === 'string') return raw.split(/[\n,]/).map((item) => item.trim()).filter(Boolean)
      if (raw && typeof raw === 'object') return Object.entries(raw as Record<string, unknown>).flatMap(([key, item]) => {
        const clean = (text: string) => text.replace(/^\s*-?\s*["']?/, '').replace(/["']?\s*$/, '')
        if (item === true) return [clean(key)]
        return typeof item === 'string' ? [`${clean(key)}:${clean(item)}`] : []
      })
      return []
    }
    if (Array.isArray(parsed)) {
      return { rows: parsed.map((rawRule) => {
        const rule = rawRule && typeof rawRule === 'object' ? rawRule as Record<string, unknown> : {}
        return { team: typeof rule.team === 'string' ? rule.team : '', role: typeof rule.role === 'string' ? rule.role : 'user', permissions: normalizePermissions(rule.permissions), env_file: typeof rule.env_file === 'string' ? rule.env_file : typeof rule.envFile === 'string' ? rule.envFile : '' }
      }) }
    }
    if (!parsed || typeof parsed !== 'object') throw new Error('mapping must be an object')
    return {
      rows: Object.entries(parsed as Record<string, unknown>).map(([team, rawRule]) => {
        const rule = rawRule && typeof rawRule === 'object' ? rawRule as Record<string, unknown> : { role: rawRule }
        return {
          team,
          role: typeof rule.role === 'string' ? rule.role : 'user',
          permissions: normalizePermissions(rule.permissions),
          env_file: typeof rule.env_file === 'string' ? rule.env_file : typeof rule.envFile === 'string' ? rule.envFile : '',
        }
      }),
    }
  } catch {
    return { rows: [], error: '既存のJSONを読み込めません。JSONを修正してからフォーム編集に切り替えてください。' }
  }
}

function serializeTeamRoleRows(rows: TeamRoleRow[]): string {
  const mapping = Object.fromEntries(rows.filter((row) => row.team.trim()).map(({ team, role, permissions, env_file }) => [
    team.trim(),
    { role: role.trim() || 'user', permissions, ...(env_file?.trim() ? { env_file: env_file.trim() } : {}) },
  ]))
  return JSON.stringify(mapping, null, 2)
}

function TeamRoleMappingField({ value, onChange }: { value: unknown; onChange: (value: unknown) => void }) {
  const parsed = parseTeamRoleRows(value)
  const inputClass = 'w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-900 dark:text-white'

  if (parsed.error) return <div className="mt-2 space-y-2"><p className="text-xs text-red-600 dark:text-red-400">{parsed.error}</p><textarea rows={6} className={inputClass} value={typeof value === 'string' ? value : JSON.stringify(value, null, 2)} onChange={(event) => onChange(event.target.value)} /></div>

  return <TeamRoleMappingBuilder initialRows={parsed.rows} onChange={onChange} />
}

function TeamRoleMappingBuilder({ initialRows, onChange }: { initialRows: TeamRoleRow[]; onChange: (value: unknown) => void }) {
  const [rows, setRows] = useState(initialRows)
  const inputClass = 'w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-900 dark:text-white'
  const updateRows = (next: TeamRoleRow[]) => {
    setRows(next)
    onChange(serializeTeamRoleRows(next))
  }
  const normalizedTeams = rows.map((row) => row.team.trim()).filter(Boolean)
  const hasDuplicate = new Set(normalizedTeams).size !== normalizedTeams.length

  return <div className="mt-2 space-y-3">
    {rows.map((row, index) => <div key={index} className="rounded-lg border border-gray-200 p-4 dark:border-gray-700">
      <div className="grid gap-3 md:grid-cols-2">
        <div><label className="text-xs font-medium text-gray-600 dark:text-gray-400">Organization / Team</label><input className={inputClass} value={row.team} placeholder="example-org/platform" onChange={(event) => updateRows(rows.map((item, itemIndex) => itemIndex === index ? { ...item, team: event.target.value } : item))} /></div>
        <div><label className="text-xs font-medium text-gray-600 dark:text-gray-400">Role</label><input className={inputClass} list="admin-team-role-options" value={row.role} placeholder="user または admin" onChange={(event) => updateRows(rows.map((item, itemIndex) => itemIndex === index ? { ...item, role: event.target.value } : item))} /></div>
        <div><label className="text-xs font-medium text-gray-600 dark:text-gray-400">Permissions</label><textarea rows={2} className={inputClass} value={row.permissions.join('\n')} placeholder="1行に1つ（例: session:access）" onChange={(event) => updateRows(rows.map((item, itemIndex) => itemIndex === index ? { ...item, permissions: event.target.value.split(/[\n,]/).map((item) => item.trim()).filter(Boolean) } : item))} /></div>
        <div><label className="text-xs font-medium text-gray-600 dark:text-gray-400">Env File（任意）</label><input className={inputClass} value={row.env_file || ''} placeholder="admin.env" onChange={(event) => updateRows(rows.map((item, itemIndex) => itemIndex === index ? { ...item, env_file: event.target.value } : item))} /></div>
      </div>
      <div className="mt-3 text-right"><button type="button" className="text-sm text-red-600 hover:text-red-700 dark:text-red-400" onClick={() => updateRows(rows.filter((_, itemIndex) => itemIndex !== index))}>このTeamを削除</button></div>
    </div>)}
    <datalist id="admin-team-role-options"><option value="user" /><option value="admin" /></datalist>
    <button type="button" className="rounded-md border border-blue-600 px-3 py-2 text-sm font-medium text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950/30" onClick={() => updateRows([...rows, { team: '', role: 'user', permissions: [] }])}>＋ Teamを追加</button>
    {hasDuplicate && <p className="text-xs text-red-600 dark:text-red-400">同じ Organization / Team が複数あります。最後の設定だけが保存されます。</p>}
    {rows.length === 0 && <p className="text-xs text-gray-500">Team mapping はまだありません。「Teamを追加」から作成できます。</p>}
  </div>
}

function Field({ field, value, configured, onChange }: { field: AdminField; value: unknown; configured: boolean; onChange: (value: unknown) => void }) {
  if (field.type === 'toggle') {
    return <ToggleSwitch checked={Boolean(value)} onChange={onChange} label={field.label} />
  }
  if (field.type === 'team-role-mapping') return <TeamRoleMappingField value={value} onChange={onChange} />
  if (field.type === 'textarea') {
    return <textarea
      rows={4}
      className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-transparent focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800 dark:text-white"
      value={typeof value === 'string' ? value : ''}
      placeholder={field.placeholder}
      onChange={(event) => onChange(event.target.value)}
    />
  }
  if (field.type === 'select') {
    return <SelectField
      value={typeof value === 'string' ? value : ''}
      onChange={onChange}
      options={field.options ?? []}
      className="w-full"
    />
  }

  const isSecret = field.type === 'secret'
  return <div>
    <TextField
      type={isSecret ? 'password' : field.type === 'number' ? 'number' : 'text'}
      value={typeof value === 'string' || typeof value === 'number' ? String(value) : ''}
      placeholder={isSecret && configured ? '設定済み（変更時のみ入力）' : field.placeholder}
      onChange={(next) => onChange(field.type === 'number' ? (next === '' ? '' : Number(next)) : next)}
    />
    {isSecret && configured && <p className="mt-1 text-xs text-emerald-600 dark:text-emerald-400">現在の値が設定されています</p>}
  </div>
}

export default function AdminSectionPage({ params }: { params: Promise<{ section: string }> }) {
  const { section: sectionID } = use(params)
  const definition = getAdminSection(sectionID)
  const [document, setDocument] = useState<AdminSettingsDocument | null>(null)
  const [sections, setSections] = useState<AdminSettingsSections>({})
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { showToast } = useToast()

  useEffect(() => {
    createCurrentDeploymentAgentAPIProxyClient().getAdminSettings().then((data) => { setDocument(data); setSections(data.sections || {}) }).catch(() => setError('設定を読み込めませんでした'))
  }, [])

  if (!definition) notFound()
  if (!document && !error) return (
    <div className="flex items-center justify-center py-16">
      <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-blue-600" />
    </div>
  )

  const section = sections[sectionID] || {}
  const changed = document ? JSON.stringify(sections) !== JSON.stringify(document.sections) : false
  const save = async () => {
    if (!document) return
    setSaving(true); setError(null)
    try {
      const saved = await createCurrentDeploymentAgentAPIProxyClient().updateAdminSettings({ base_version: document.version, sections })
      setDocument(saved); setSections(saved.sections); showToast(`設定をversion ${saved.version}として保存しました`, 'success')
    } catch (err) {
      setError(err instanceof Error && err.message.includes('409') ? '別の管理者が更新しました。再読み込みしてください。' : '設定の保存に失敗しました')
    } finally { setSaving(false) }
  }

  return (
    <>
      <SettingsPageHeader
        title={definition.title}
        description={definition.description}
        action={
          <span className="whitespace-nowrap rounded-full bg-gray-100 px-3 py-1 text-xs text-gray-600 dark:bg-gray-800 dark:text-gray-300">
            version {document?.version || 0}
          </span>
        }
      />

      {error && (
        <div className="mb-5 rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-800 dark:bg-red-900/20">
          <p className="text-sm text-red-600 dark:text-red-400">{error}</p>
        </div>
      )}

      {sectionID === 'agents' ? (
        <AgentsSettingsFields
          section={section}
          onChange={(value) => setSections((previous) => ({ ...previous, agents: value }))}
        />
      ) : (
        <FieldGroup>
          {definition.fields.map((field) => (
            <FieldRow
              key={field.path}
              label={field.label}
              description={field.description}
              control={field.type === 'toggle' ? (
                <Field
                  field={field}
                  value={getValue(section, field.path)}
                  configured={Boolean(document?.secret_configured[`${sectionID}.${field.path}`])}
                  onChange={(value) => setSections((previous) => ({ ...previous, [sectionID]: setValue(previous[sectionID] || {}, field.path, value) }))}
                />
              ) : undefined}
            >
              {field.type !== 'toggle' && (
                <Field
                  field={field}
                  value={getValue(section, field.path)}
                  configured={Boolean(document?.secret_configured[`${sectionID}.${field.path}`])}
                  onChange={(value) => setSections((previous) => ({ ...previous, [sectionID]: setValue(previous[sectionID] || {}, field.path, value) }))}
                />
              )}
            </FieldRow>
          ))}
        </FieldGroup>
      )}

      {changed && (
        <div className="sticky bottom-0 z-30 -mx-4 mt-8 border-t border-gray-200 bg-white/90 px-4 py-3 backdrop-blur dark:border-gray-700 dark:bg-gray-900/90">
          <div className="flex flex-wrap items-center justify-end gap-3">
            <div className="mr-auto flex items-center gap-2 text-sm text-amber-600 dark:text-amber-400">
              <AlertTriangle className="h-4 w-4 flex-shrink-0" />
              <span>未保存の変更があります</span>
            </div>
            <button
              type="button"
              onClick={() => document && setSections(document.sections)}
              disabled={saving}
              className="rounded-md border border-gray-300 px-4 py-1.5 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-100 disabled:opacity-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-800"
            >
              破棄
            </button>
            <button
              type="button"
              onClick={save}
              disabled={saving}
              className="flex items-center gap-2 rounded-md bg-blue-600 px-5 py-1.5 text-sm font-semibold text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {saving && <span className="h-3.5 w-3.5 animate-spin rounded-full border-b-2 border-white" />}
              {saving ? '保存中...' : '新しい version として保存'}
            </button>
          </div>
        </div>
      )}
    </>
  )
}
