'use client'

import { useState, useEffect, useCallback } from 'react'
import {
  SessionProfile,
  CreateSessionProfileRequest,
  UpdateSessionProfileRequest,
  CredentialSource,
} from '../../types/session_profile'
import { SandboxPolicy } from '../../types/sandbox_policy'
import { LogicalSessionPool } from '../../types/session_pool'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import type { APIMCPServerConfig } from '../../types/settings'
import { MCPServerSettings } from '../../components/settings/MCPServerSettings'

interface SessionProfileFormModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess: () => void
  editingProfile?: SessionProfile | null
}

type KeyValuePair = { key: string; value: string }
const SUPPORTED_AGENT_TYPES = new Set(['auto', 'claude-legacy', 'claude-acp', 'codex-acp', 'pi-ollama', 'cursor'])

const normalizeAgentType = (value?: string): string => {
  return value && SUPPORTED_AGENT_TYPES.has(value) ? value : ''
}

export default function SessionProfileFormModal({
  isOpen,
  onClose,
  onSuccess,
  editingProfile,
}: SessionProfileFormModalProps) {
  const { getScopeParams } = useTeamScope()

  // Basic fields
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [isDefault, setIsDefault] = useState(false)

  // Config fields
  const [envPairs, setEnvPairs] = useState<KeyValuePair[]>([{ key: '', value: '' }])
  const [tagPairs, setTagPairs] = useState<KeyValuePair[]>([{ key: '', value: '' }])
  const [pool, setPool] = useState('')
  const [availablePools, setAvailablePools] = useState<LogicalSessionPool[]>([])
  const [agentType, setAgentType] = useState('')
  const [mcpServers, setMcpServers] = useState<Record<string, APIMCPServerConfig>>({})

  // Docker / DinD fields
  const [dockerEnabled, setDockerEnabled] = useState(false)
  const [dockerRegistries, setDockerRegistries] = useState<Array<{ server: string; username: string; password: string; secretName: string; insecure: boolean }>>([])

  // Network sandbox fields. Profiles always run with sandbox enabled; no selected
  // policy means count mode so traffic is recorded but not blocked.
  const [sandboxPolicyId, setSandboxPolicyId] = useState('')
  const [sandboxPolicies, setSandboxPolicies] = useState<SandboxPolicy[]>([])

  // Session TTL
  const [sessionTTL, setSessionTTL] = useState('')
  const [unsyncedFilePaths, setUnsyncedFilePaths] = useState('')
  const [credentialSource, setCredentialSource] = useState<CredentialSource | ''>('')

  const addDockerRegistry = () => setDockerRegistries(prev => [...prev, { server: '', username: '', password: '', secretName: '', insecure: false }])
  const removeDockerRegistry = (index: number) => setDockerRegistries(prev => prev.filter((_, i) => i !== index))
  const updateDockerRegistry = (index: number, field: string, value: string | boolean) =>
    setDockerRegistries(prev => prev.map((r, i) => i === index ? { ...r, [field]: value } : r))

  // UI state
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showAdvanced, setShowAdvanced] = useState(false)

  const isEditing = !!editingProfile

  useEffect(() => {
    if (!isOpen) return
    const fetchConfigOptions = async () => {
      const client = createAgentAPIProxyClientFromStorage()
      try {
        const response = await client.getSandboxPolicies({ ...getScopeParams() })
        setSandboxPolicies(response.sandbox_policies || [])
      } catch {
        setSandboxPolicies([])
      }
      try {
        setAvailablePools(await client.getAvailableSessionPools())
      } catch {
        setAvailablePools([])
      }
    }
    fetchConfigOptions()
  }, [isOpen, getScopeParams])

  // Initialize form when editing
  useEffect(() => {
    if (editingProfile) {
      setShowAdvanced(false)
      setName(editingProfile.name)
      setDescription(editingProfile.description ?? '')
      setIsDefault(editingProfile.is_default ?? false)

      const cfg = editingProfile.config
      setPool(cfg?.pool ?? '')
      if (cfg?.pool) setShowAdvanced(true)
      setAgentType(normalizeAgentType(cfg?.params?.agent_type))
      setMcpServers(cfg?.mcp_servers ?? {})
      if (cfg?.mcp_servers && Object.keys(cfg.mcp_servers).length > 0) {
        setShowAdvanced(true)
      }

      if (cfg?.environment && Object.keys(cfg.environment).length > 0) {
        setEnvPairs(Object.entries(cfg.environment).map(([key, value]) => ({ key, value })))
        setShowAdvanced(true)
      } else {
        setEnvPairs([{ key: '', value: '' }])
      }

      if (cfg?.tags && Object.keys(cfg.tags).length > 0) {
        setTagPairs(Object.entries(cfg.tags).map(([key, value]) => ({ key, value })))
        setShowAdvanced(true)
      } else {
        setTagPairs([{ key: '', value: '' }])
      }

      if (cfg?.params?.agent_type) {
        setShowAdvanced(true)
      }

      // Initialize docker from profile params
      const docker = cfg?.params?.docker
      if (docker) {
        setDockerEnabled(docker.enabled)
        setDockerRegistries((docker.registries ?? []).map(r => ({
          server: r.server ?? '',
          username: r.username ?? '',
          password: r.password ?? '',
          secretName: r.secret_name ?? '',
          insecure: r.insecure ?? false,
        })))
        if (docker.enabled) setShowAdvanced(true)
      } else {
        setDockerEnabled(false)
        setDockerRegistries([])
      }

      // Initialize network sandbox from profile params and legacy top-level policy ID
      const sandbox = cfg?.params?.sandbox
      const policyId = sandbox?.policy_id ?? cfg?.sandbox_policy_id ?? ''
      setSandboxPolicyId(policyId)
      if (sandbox?.enabled || sandbox?.count_mode || policyId) setShowAdvanced(true)

      // Initialize session_ttl from profile config
      const ttl = cfg?.session_ttl ?? ''
      setSessionTTL(ttl)
      if (ttl) setShowAdvanced(true)

      const paths = cfg?.unsynced_file_paths ?? cfg?.params?.unsynced_file_paths ?? []
      setUnsyncedFilePaths(paths.join('\n'))
      if (paths.length > 0) setShowAdvanced(true)

      const source = cfg?.params?.credential_source ?? ''
      setCredentialSource(source)
    } else {
      // Reset form
      setName('')
      setDescription('')
      setIsDefault(false)
      setEnvPairs([{ key: '', value: '' }])
      setTagPairs([{ key: '', value: '' }])
      setPool('')
      setAgentType('')
      setMcpServers({})
      setDockerEnabled(false)
      setDockerRegistries([])
      setSandboxPolicyId('')
      setSessionTTL('')
      setUnsyncedFilePaths('')
      setCredentialSource('')
      setShowAdvanced(false)
    }
    setError(null)
  }, [editingProfile, isOpen])

  // Keyboard handler
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    },
    [onClose]
  )

  useEffect(() => {
    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown)
    }
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, handleKeyDown])

  // Key-value pair helpers
  const updatePair = (
    pairs: KeyValuePair[],
    setPairs: React.Dispatch<React.SetStateAction<KeyValuePair[]>>,
    index: number,
    field: 'key' | 'value',
    val: string
  ) => {
    const next = [...pairs]
    next[index] = { ...next[index], [field]: val }
    setPairs(next)
  }

  const addPair = (setPairs: React.Dispatch<React.SetStateAction<KeyValuePair[]>>) => {
    setPairs((prev) => [...prev, { key: '', value: '' }])
  }

  const removePair = (
    pairs: KeyValuePair[],
    setPairs: React.Dispatch<React.SetStateAction<KeyValuePair[]>>,
    index: number
  ) => {
    if (pairs.length === 1) {
      setPairs([{ key: '', value: '' }])
    } else {
      setPairs((prev) => prev.filter((_, i) => i !== index))
    }
  }

  const pairsToRecord = (pairs: KeyValuePair[]): Record<string, string> => {
    const record: Record<string, string> = {}
    pairs.forEach(({ key, value }) => {
      if (key.trim()) record[key.trim()] = value
    })
    return record
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)

    if (!name.trim()) {
      setError('名前は必須です')
      return
    }

    setIsSubmitting(true)
    try {
      const client = createAgentAPIProxyClientFromStorage()

      const environment = pairsToRecord(envPairs)
      const tags = pairsToRecord(tagPairs)
      const parsedUnsyncedFilePaths = unsyncedFilePaths
        .split('\n')
        .map(path => path.trim())
        .filter(Boolean)

      // Build docker config if enabled
      let dockerConfig: { enabled: boolean; registries?: { server?: string; username?: string; password?: string; secret_name?: string; insecure?: boolean }[] } | undefined
      if (dockerEnabled) {
        const regs = dockerRegistries
          .filter(r => r.server || r.username || r.secretName)
          .map(r => ({
            ...(r.server ? { server: r.server } : {}),
            ...(r.secretName ? { secret_name: r.secretName } : {}),
            ...(r.username && !r.secretName ? { username: r.username, password: r.password } : {}),
            ...(r.insecure ? { insecure: true } : {}),
          }))
        dockerConfig = { enabled: true, ...(regs.length > 0 ? { registries: regs } : {}) }
      }

      const sandboxConfig = {
        enabled: true,
        ...(sandboxPolicyId ? { policy_id: sandboxPolicyId } : {}),
        ...(!sandboxPolicyId ? { count_mode: true } : {}),
      }

      // Build params if any param is set
      const params = {
        ...(agentType.trim() ? { agent_type: agentType.trim() } : {}),
        sandbox: sandboxConfig,
        ...(dockerConfig ? { docker: dockerConfig } : {}),
        ...(credentialSource ? { credential_source: credentialSource } : {}),
      }

      const config = {
        ...(Object.keys(environment).length > 0 ? { environment } : {}),
        ...(Object.keys(tags).length > 0 ? { tags } : {}),
        ...(pool.trim() ? { pool: pool.trim() } : {}),
        ...(Object.keys(mcpServers).length > 0 ? { mcp_servers: mcpServers } : {}),
        params,
        ...(sandboxPolicyId ? { sandbox_policy_id: sandboxPolicyId } : {}),
        ...(sessionTTL.trim() ? { session_ttl: sessionTTL.trim() } : {}),
        ...(parsedUnsyncedFilePaths.length > 0 ? { unsynced_file_paths: parsedUnsyncedFilePaths } : {}),
      }

      if (isEditing && editingProfile) {
        const updateData: UpdateSessionProfileRequest = {
          name: name.trim(),
          description: description.trim() || undefined,
          is_default: isDefault,
          config: Object.keys(config).length > 0 ? config : undefined,
        }
        await client.updateSessionProfile(editingProfile.id, updateData)
      } else {
        const scopeParams = getScopeParams()
        const createData: CreateSessionProfileRequest = {
          name: name.trim(),
          description: description.trim() || undefined,
          is_default: isDefault,
          ...(Object.keys(config).length > 0 ? { config } : {}),
          ...scopeParams,
        }
        await client.createSessionProfile(createData)
      }

      onSuccess()
    } catch (err) {
      console.error('Failed to save session profile:', err)
      setError(err instanceof Error ? err.message : 'セッションプロファイルの保存に失敗しました')
    } finally {
      setIsSubmitting(false)
    }
  }

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-50 overflow-y-auto">
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black bg-opacity-50 transition-opacity"
        onClick={onClose}
      />

      {/* Modal */}
      <div className="relative min-h-full flex items-center justify-center p-4">
        <div
          className="relative bg-white dark:bg-gray-800 rounded-xl shadow-2xl w-full max-w-2xl"
          onClick={(e) => e.stopPropagation()}
        >
          {/* Header */}
          <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
            <div className="flex items-center gap-2">
              <div className="w-7 h-7 rounded-lg bg-indigo-100 dark:bg-indigo-900/30 flex items-center justify-center">
                <svg className="w-4 h-4 text-indigo-600 dark:text-indigo-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
                </svg>
              </div>
              <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
                {isEditing ? 'プロファイルを編集' : '新しいプロファイル'}
              </h2>
            </div>
            <button
              onClick={onClose}
              className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>

          {/* Form */}
          <form onSubmit={handleSubmit} className="px-6 py-5 space-y-5 max-h-[70vh] overflow-y-auto overflow-x-hidden">
            {/* Error */}
            {error && (
              <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
                <p className="text-sm text-red-700 dark:text-red-400">{error}</p>
              </div>
            )}

            {/* Name */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                名前 <span className="text-red-500">*</span>
              </label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="例: my-profile"
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 dark:focus:ring-blue-400"
                required
              />
            </div>

            {/* Description */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                説明
              </label>
              <textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="このプロファイルの用途を説明してください"
                rows={2}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y"
              />
            </div>

            {/* Credential source */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                認証情報の配布元
              </label>
              <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
                Codex の <code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">auth.json</code> など、セッションへ配布する認証情報を選択します。
              </p>
              <select
                value={credentialSource}
                onChange={(e) => setCredentialSource(e.target.value as CredentialSource | '')}
                className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
              >
                <option value="">デフォルト（個人: 作成者 / チーム: 配布なし）</option>
                <option value="session_user">セッションを作成したユーザー</option>
                <option value="team">セッションのチーム</option>
                <option value="none">配布しない</option>
              </select>
            </div>

            {/* is_default */}
            <div>
              <label className="flex items-start gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={isDefault}
                  onChange={(e) => setIsDefault(e.target.checked)}
                  className="mt-0.5 h-4 w-4 rounded border-gray-300 dark:border-gray-600 text-blue-600 focus:ring-blue-500 cursor-pointer"
                />
                <div>
                  <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
                    デフォルトプロファイルに設定する
                  </span>
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                    セッション作成時にこのプロファイルをデフォルトとして使用します。
                  </p>
                </div>
              </label>
            </div>

            {/* Config Section */}
            <div>
              <button
                type="button"
                onClick={() => setShowAdvanced(!showAdvanced)}
                className="flex items-center gap-1 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200"
              >
                <svg
                  className={`w-4 h-4 transition-transform ${showAdvanced ? 'rotate-90' : ''}`}
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                </svg>
                設定（Config）
              </button>

              {showAdvanced && (
                <div className="mt-4 space-y-5 pl-4 border-l-2 border-gray-200 dark:border-gray-700">
                  {/* Session Runner Pool */}
                  <div>
                    <label htmlFor="session-profile-pool" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      Session Runner Pool
                    </label>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
                      このプロファイルで作成するセッションの実行先Poolを指定します。空欄の場合は自動選択されます。
                    </p>
                    <select
                      id="session-profile-pool"
                      value={pool}
                      onChange={(e) => setPool(e.target.value)}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 dark:focus:ring-blue-400"
                    >
                      <option value="">自動選択</option>
                      {pool && !availablePools.some((availablePool) => availablePool.name === pool) && (
                        <option value={pool}>{pool}（現在の設定）</option>
                      )}
                      {availablePools.map((availablePool) => (
                        <option key={availablePool.name} value={availablePool.name}>
                          {availablePool.name}
                        </option>
                      ))}
                    </select>
                    {availablePools.length === 0 && (
                      <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
                        選択可能なPool候補がありません。
                      </p>
                    )}
                  </div>

                  {/* Environment Variables */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                      環境変数
                    </label>
                    <div className="space-y-2">
                      {envPairs.map((pair, idx) => (
                        <div key={idx} className="rounded border border-gray-200 dark:border-gray-600 p-2 space-y-1.5 bg-gray-50 dark:bg-gray-900/40">
                          <input
                            type="text"
                            value={pair.key}
                            onChange={(e) => updatePair(envPairs, setEnvPairs, idx, 'key', e.target.value)}
                            placeholder="KEY"
                            className="w-full px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500"
                          />
                          <div className="flex items-center gap-2">
                            <input
                              type="text"
                              value={pair.value}
                              onChange={(e) => updatePair(envPairs, setEnvPairs, idx, 'value', e.target.value)}
                              placeholder="value"
                              className="flex-1 min-w-0 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500"
                            />
                            <button
                              type="button"
                              onClick={() => removePair(envPairs, setEnvPairs, idx)}
                              className="shrink-0 p-1 text-gray-400 hover:text-red-500 dark:hover:text-red-400"
                            >
                              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                              </svg>
                            </button>
                          </div>
                        </div>
                      ))}
                      <button
                        type="button"
                        onClick={() => addPair(setEnvPairs)}
                        className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1"
                      >
                        <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                        </svg>
                        追加
                      </button>
                    </div>
                  </div>

                  {/* Tags */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                      タグ
                    </label>
                    <div className="space-y-2">
                      {tagPairs.map((pair, idx) => (
                        <div key={idx} className="rounded border border-gray-200 dark:border-gray-600 p-2 space-y-1.5 bg-gray-50 dark:bg-gray-900/40">
                          <input
                            type="text"
                            value={pair.key}
                            onChange={(e) => updatePair(tagPairs, setTagPairs, idx, 'key', e.target.value)}
                            placeholder="key"
                            className="w-full px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500"
                          />
                          <div className="flex items-center gap-2">
                            <input
                              type="text"
                              value={pair.value}
                              onChange={(e) => updatePair(tagPairs, setTagPairs, idx, 'value', e.target.value)}
                              placeholder="value"
                              className="flex-1 min-w-0 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500"
                            />
                            <button
                              type="button"
                              onClick={() => removePair(tagPairs, setTagPairs, idx)}
                              className="shrink-0 p-1 text-gray-400 hover:text-red-500 dark:hover:text-red-400"
                            >
                              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                              </svg>
                            </button>
                          </div>
                        </div>
                      ))}
                      <button
                        type="button"
                        onClick={() => addPair(setTagPairs)}
                        className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1"
                      >
                        <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                        </svg>
                        追加
                      </button>
                    </div>
                  </div>

                  {/* Agent Type */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                      エージェントタイプ
                    </label>
                    <div className="space-y-2">
                      {([
                        { value: '', label: 'デフォルト', description: 'agent_type を送信しない' },
                        { value: 'auto', label: '自動選択', description: 'Codex auth.json があれば Codex ACP、それ以外は Claude ACP' },
                        { value: 'claude-legacy', label: 'Claude Legacy', description: 'agent_type=claude-legacy を送信' },
                        { value: 'claude-acp', label: 'Claude ACP', description: 'agent_type=claude-acp を送信' },
                        { value: 'codex-acp', label: 'Codex ACP', description: 'agent_type=codex-acp を送信' },
                        { value: 'pi-ollama', label: 'Pi Ollama', description: 'agent_type=pi-ollama を送信' },
                        { value: 'cursor', label: 'Cursor ACP', description: 'agent_type=cursor を送信' },
                      ]).map(({ value: v, label, description }) => (
                        <label key={v} className="flex items-start cursor-pointer group">
                          <input
                            type="radio"
                            name="profile-agent-type"
                            value={v}
                            checked={agentType === v}
                            onChange={() => setAgentType(v)}
                            className="mt-0.5 w-3.5 h-3.5 text-blue-600 border-gray-300 dark:border-gray-600 focus:ring-blue-500"
                          />
                          <span className="ml-2">
                            <span className="block text-xs font-medium text-gray-600 dark:text-gray-400 group-hover:text-gray-800 dark:group-hover:text-gray-200">
                              {label}
                            </span>
                            <span className="block text-xs text-gray-400 dark:text-gray-500 mt-0.5">
                              {description}
                            </span>
                          </span>
                        </label>
                      ))}
                    </div>
                  </div>

                  {/* MCP Servers */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      MCP サーバー
                    </label>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
                      個人・チーム設定の MCP サーバーを継承します。同じ名前のサーバーは、このプロファイルの設定で上書きされます。
                    </p>
                    <MCPServerSettings servers={mcpServers} onChange={setMcpServers} />
                  </div>

                  {/* Network Sandbox */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                      sandbox policy
                    </label>
                    <select
                      value={sandboxPolicyId}
                      onChange={(e) => setSandboxPolicyId(e.target.value)}
                      className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                    >
                      <option value="">ポリシーなし（count mode）</option>
                      {sandboxPolicies.map((policy) => (
                        <option key={policy.id} value={policy.id}>
                          {policy.name}{policy.scope === 'team' ? ' [チーム]' : ''}
                        </option>
                      ))}
                    </select>
                    {sandboxPolicies.length === 0 && (
                      <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
                        適用できる sandbox policy がありません。count mode で起動します。
                      </p>
                    )}
                  </div>

                  {/* Docker in Docker (DinD) */}
                  <div>
                    <label className="flex items-start gap-2 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={dockerEnabled}
                        onChange={(e) => setDockerEnabled(e.target.checked)}
                        className="mt-0.5 h-4 w-4 rounded border-gray-300 dark:border-gray-600 text-blue-600 focus:ring-blue-500 cursor-pointer"
                      />
                      <div>
                        <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
                          Docker in Docker (DinD) を有効にする
                        </span>
                        <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                          セッション Pod に docker:dind サイドカーを追加し、<code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded text-xs">DOCKER_HOST</code> を自動設定します。
                        </p>
                      </div>
                    </label>

                    {dockerEnabled && (
                      <div className="mt-3 ml-6 space-y-3">
                        <div className="flex items-center justify-between">
                          <span className="text-xs font-medium text-gray-600 dark:text-gray-400">認証済みレジストリ（任意）</span>
                          <button
                            type="button"
                            onClick={addDockerRegistry}
                            className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-700"
                          >
                            + 追加
                          </button>
                        </div>
                        {dockerRegistries.length === 0 && (
                          <p className="text-xs text-gray-400 dark:text-gray-500">
                            認証不要な場合は追加不要です。
                          </p>
                        )}
                        {dockerRegistries.map((registry, index) => (
                          <div key={index} className="border border-gray-200 dark:border-gray-600 rounded-lg p-3 space-y-2">
                            <div className="flex items-center justify-between">
                              <span className="text-xs font-medium text-gray-600 dark:text-gray-400">レジストリ #{index + 1}</span>
                              <button
                                type="button"
                                onClick={() => removeDockerRegistry(index)}
                                className="text-xs text-red-500 hover:text-red-600"
                              >
                                削除
                              </button>
                            </div>
                            <div>
                              <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">サーバー（空 = Docker Hub）</label>
                              <input
                                type="text"
                                value={registry.server}
                                onChange={e => updateDockerRegistry(index, 'server', e.target.value)}
                                placeholder="ghcr.io"
                                className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                              />
                            </div>
                            <div className="flex items-center gap-2">
                              <input
                                type="checkbox"
                                id={`insecure-${index}`}
                                checked={registry.insecure}
                                onChange={e => updateDockerRegistry(index, 'insecure', e.target.checked)}
                                className="w-3 h-3 rounded"
                              />
                              <label htmlFor={`insecure-${index}`} className="text-xs text-gray-500 dark:text-gray-400">HTTP（insecure）レジストリ</label>
                            </div>
                            <div>
                              <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">K8s Secret 名（docker config JSON）</label>
                              <input
                                type="text"
                                value={registry.secretName}
                                onChange={e => updateDockerRegistry(index, 'secretName', e.target.value)}
                                placeholder="my-registry-secret"
                                className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                              />
                            </div>
                            {!registry.secretName && (
                              <>
                                <div>
                                  <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">ユーザー名</label>
                                  <input
                                    type="text"
                                    value={registry.username}
                                    onChange={e => updateDockerRegistry(index, 'username', e.target.value)}
                                    placeholder="myuser"
                                    className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                                  />
                                </div>
                                <div>
                                  <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">パスワード / アクセストークン</label>
                                  <input
                                    type="password"
                                    value={registry.password}
                                    onChange={e => updateDockerRegistry(index, 'password', e.target.value)}
                                    placeholder="••••••••"
                                    className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                                  />
                                </div>
                              </>
                            )}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>

                  {/* セッション TTL */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      セッション自動削除 TTL
                    </label>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
                      最後のメッセージからこの時間が経過するとセッションを自動削除します。例: <code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">24h</code>、<code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">168h</code>（空欄 = 自動削除なし / グローバル設定に従う）
                    </p>
                    <input
                      type="text"
                      value={sessionTTL}
                      onChange={e => setSessionTTL(e.target.value)}
                      placeholder="例: 24h、72h、168h"
                      className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                    />
                  </div>

                  {/* 同期しないファイルパス */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      同期しないファイルパス
                    </label>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
                      変更を保存先へ同期しない管理ファイルのパスを 1 行に 1 つ指定します。
                    </p>
                    <textarea
                      value={unsyncedFilePaths}
                      onChange={e => setUnsyncedFilePaths(e.target.value)}
                      placeholder="/home/agentapi/.codex/auth.json"
                      rows={3}
                      className="w-full px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white font-mono resize-y"
                    />
                  </div>
                </div>
              )}
            </div>
          </form>

          {/* Footer */}
          <div className="flex items-center justify-end gap-3 px-6 py-4 border-t border-gray-200 dark:border-gray-700">
            <button
              type="button"
              onClick={onClose}
              disabled={isSubmitting}
              className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-600 transition-colors disabled:opacity-50"
            >
              キャンセル
            </button>
            <button
              type="submit"
              onClick={handleSubmit}
              disabled={isSubmitting}
              className="px-4 py-2 text-sm text-white bg-blue-600 hover:bg-blue-700 rounded-lg transition-colors disabled:opacity-50 flex items-center gap-2"
            >
              {isSubmitting && (
                <svg className="w-4 h-4 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                </svg>
              )}
              {isEditing ? '保存' : '作成'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
