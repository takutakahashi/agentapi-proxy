'use client'

import { useState, useCallback, useEffect } from 'react'
import { SandboxPolicy, CreateSandboxPolicyRequest, UpdateSandboxPolicyRequest } from '../../types/sandbox_policy'
import { createAgentAPIProxyClientFromStorage, AgentAPIProxyError } from '../../lib/agentapi-proxy-client'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import TopBar from '../components/TopBar'
import NavigationTabs from '../components/NavigationTabs'
import TagFilterSidebar from '../components/TagFilterSidebar'
import CopyableResourceId from '../components/CopyableResourceId'

// ---- Session Domain Import Page ----
interface SessionDomainImportPanelProps {
  onClose: () => void
  policy: SandboxPolicy
  onImported: () => void
}

function SessionDomainImportPanel({ onClose, policy, onImported }: SessionDomainImportPanelProps) {
  const [domains, setDomains] = useState<{ allowed: string[]; denied: string[]; ignored: string[]; updated_at?: string } | null>(null)
  const [loading, setLoading] = useState(false)
  const [selectedDomains, setSelectedDomains] = useState<Set<string>>(new Set())
  // Track domains already registered in the policy (pre-existing + just added this session)
  const [registeredDomains, setRegisteredDomains] = useState<Set<string>>(new Set())
  // Track locally ignored domains (synced from server on fetch)
  const [ignoredDomains, setIgnoredDomains] = useState<Set<string>>(new Set())
  const [showHidden, setShowHidden] = useState(false)
  const [activeTab, setActiveTab] = useState<'denied' | 'allowed'>('denied')
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)

  const fetchDomains = useCallback(async () => {
    setLoading(true)
    setDomains(null)
    setSelectedDomains(new Set())
    setError(null)
    setSuccess(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const result = await client.getSandboxPolicyDomains(policy.id)
      setDomains(result)
      setIgnoredDomains(new Set(result.ignored ?? []))
    } catch (err) {
      if (err instanceof AgentAPIProxyError && err.status === 501) {
        setError('ドメイン収集機能が利用できません')
      } else {
        setError('ドメインデータの取得に失敗しました')
      }
    } finally {
      setLoading(false)
    }
  }, [policy.id])

  useEffect(() => {
    setActiveTab('denied')
    setShowHidden(false)
    // Seed registered set from the policy's current domain list
    const isAllowlist = (policy.allowed_domains?.length ?? 0) > 0 || (policy.denied_domains?.length ?? 0) === 0
    setRegisteredDomains(new Set(isAllowlist ? (policy.allowed_domains ?? []) : (policy.denied_domains ?? [])))
    fetchDomains()
  }, [fetchDomains, policy.allowed_domains, policy.denied_domains])

  const toggleDomain = (domain: string) => {
    setSelectedDomains((prev) => {
      const next = new Set(prev)
      if (next.has(domain)) next.delete(domain)
      else next.add(domain)
      return next
    })
  }

  const handleAddToPolicy = async () => {
    if (selectedDomains.size === 0) return
    setIsSaving(true)
    setError(null)
    setSuccess(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const toAdd = Array.from(selectedDomains)
      const isAllowlist = (policy.allowed_domains?.length ?? 0) > 0 || (policy.denied_domains?.length ?? 0) === 0
      const update: UpdateSandboxPolicyRequest = isAllowlist
        ? { allowed_domains: [...new Set([...(policy.allowed_domains ?? []), ...toAdd])] }
        : { denied_domains: (policy.denied_domains ?? []).filter((d) => !toAdd.includes(d)) }
      await client.updateSandboxPolicy(policy.id, update)
      setSuccess(`${toAdd.length} 件のドメインをポリシーに追加しました`)
      setSelectedDomains(new Set())
      setRegisteredDomains((prev) => new Set([...prev, ...toAdd]))
      onImported()
    } catch (err) {
      setError(err instanceof AgentAPIProxyError ? err.message : 'ポリシーの更新に失敗しました')
    } finally {
      setIsSaving(false)
    }
  }

  const handleHideDomain = async (domain: string) => {
    const newIgnored = new Set(ignoredDomains)
    newIgnored.add(domain)
    setIgnoredDomains(newIgnored)
    setSelectedDomains((prev) => {
      const next = new Set(prev)
      next.delete(domain)
      return next
    })
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.updateIgnoredSandboxPolicyDomains(policy.id, Array.from(newIgnored))
    } catch {
      setIgnoredDomains((prev) => {
        const reverted = new Set(prev)
        reverted.delete(domain)
        return reverted
      })
      setError('非表示リストの更新に失敗しました')
    }
  }

  const handleUnhideDomain = async (domain: string) => {
    const newIgnored = new Set(ignoredDomains)
    newIgnored.delete(domain)
    setIgnoredDomains(newIgnored)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.updateIgnoredSandboxPolicyDomains(policy.id, Array.from(newIgnored))
    } catch {
      setIgnoredDomains((prev) => new Set([...prev, domain]))
      setError('非表示リストの更新に失敗しました')
    }
  }

  const currentDomains = (activeTab === 'denied' ? (domains?.denied ?? []) : (domains?.allowed ?? []))
    .filter((d) => !registeredDomains.has(d) && !ignoredDomains.has(d))

  const ignoredDomainsForTab = (activeTab === 'denied' ? (domains?.denied ?? []) : (domains?.allowed ?? []))
    .filter((d) => !registeredDomains.has(d) && ignoredDomains.has(d))

  const tabs = [
    { id: 'denied' as const, label: 'ブロック済み', count: domains?.denied.length ?? 0, tone: 'red' },
    { id: 'allowed' as const, label: '許可済み', count: domains?.allowed.length ?? 0, tone: 'green' },
  ]

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <button
            type="button"
            onClick={onClose}
            className="mb-3 inline-flex items-center gap-1.5 text-sm font-medium text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200"
          >
            <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
            </svg>
            一覧へ戻る
          </button>
          <h1 className="text-xl font-bold text-gray-900 dark:text-white">検出ドメインを確認</h1>
          <p className="mt-1 break-words text-sm text-gray-500 dark:text-gray-400">
            ポリシー: {policy.name}
            {!loading && domains?.updated_at && ` / 最終更新: ${new Date(domains.updated_at).toLocaleString('ja-JP')}`}
          </p>
        </div>
        <button
          onClick={fetchDomains}
          disabled={loading}
          className="inline-flex w-full items-center justify-center rounded-lg border border-blue-200 bg-white px-4 py-2 text-sm font-medium text-blue-600 transition-colors hover:bg-blue-50 disabled:opacity-50 dark:border-blue-800 dark:bg-gray-800 dark:text-blue-400 dark:hover:bg-blue-900/20 sm:w-auto"
        >
          再取得
        </button>
      </div>

      <div className="grid gap-5 lg:grid-cols-[220px_minmax(0,1fr)]">
        <aside className="lg:sticky lg:top-20 lg:self-start">
          <div className="rounded-lg border border-gray-200 bg-white p-2 dark:border-gray-700 dark:bg-gray-800">
            <nav className="grid grid-cols-2 gap-2 lg:grid-cols-1" aria-label="検出ドメイン">
              {tabs.map((tab) => (
                <button
                  key={tab.id}
                  type="button"
                  onClick={() => {
                    setActiveTab(tab.id)
                    setShowHidden(false)
                  }}
                  className={`min-w-0 rounded-md px-3 py-2 text-left transition-colors ${
                    activeTab === tab.id
                      ? tab.tone === 'red'
                        ? 'bg-red-50 text-red-700 dark:bg-red-900/30 dark:text-red-200'
                        : 'bg-green-50 text-green-700 dark:bg-green-900/30 dark:text-green-200'
                      : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-100'
                  }`}
                >
                  <span className="block truncate text-sm font-medium">{tab.label}</span>
                  <span className="mt-0.5 block text-xs opacity-75">{tab.count} 件</span>
                </button>
              ))}
            </nav>
          </div>
        </aside>

        <section className="min-w-0 rounded-lg border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
          <div className="space-y-4 p-5 md:p-6">
            {error && (
              <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
                <p className="text-sm text-red-700 dark:text-red-400">{error}</p>
              </div>
            )}
            {success && (
              <div className="p-3 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg">
                <p className="text-sm text-green-700 dark:text-green-400">{success}</p>
              </div>
            )}

            {loading && (
              <div className="flex items-center justify-center py-12 gap-2 text-gray-400">
                <svg className="animate-spin w-5 h-5" fill="none" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                  <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z" />
                </svg>
                <span className="text-sm">セッションのアクセスログを収集中...</span>
              </div>
            )}

            {!loading && domains && domains.allowed.length === 0 && domains.denied.length === 0 && (
              <p className="text-sm text-gray-400 text-center py-10">
                まだドメインが収集されていません。このポリシーを使用したセッションが起動されると自動的に収集されます。
              </p>
            )}

            {!loading && domains && (domains.allowed.length > 0 || domains.denied.length > 0) && (
              <>
                <div>
                  <h2 className="text-base font-semibold text-gray-900 dark:text-white">
                    {activeTab === 'denied' ? 'ブロック済みドメイン' : '許可済みドメイン'}
                  </h2>
                  <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
                    まだポリシーに反映していない検出ドメインを確認できます。
                  </p>
                </div>

                {currentDomains.length === 0 && ignoredDomainsForTab.length === 0 ? (
                  <p className="text-sm text-gray-400 text-center py-8">
                    {activeTab === 'denied' ? 'ブロックされたドメインはありません' : '許可されたドメインはありません'}
                  </p>
                ) : (
                  <div className="space-y-1 max-h-[55vh] overflow-y-auto">
                    {currentDomains.map((domain) => (
                      <div key={domain} className="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-gray-50 dark:hover:bg-gray-700">
                        <input
                          type="checkbox"
                          checked={selectedDomains.has(domain)}
                          onChange={() => toggleDomain(domain)}
                          className="rounded border-gray-300 dark:border-gray-600 text-blue-600 shrink-0"
                        />
                        <span className={`min-w-0 flex-1 truncate font-mono text-sm ${
                          activeTab === 'denied' ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'
                        }`}>
                          {domain}
                        </span>
                        <button
                          onClick={() => handleHideDomain(domain)}
                          title="このドメインを非表示にする"
                          className="shrink-0 text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 px-1.5 py-0.5 rounded border border-gray-200 dark:border-gray-600 hover:border-gray-400 dark:hover:border-gray-400 transition-colors"
                        >
                          非表示
                        </button>
                      </div>
                    ))}
                    {ignoredDomainsForTab.length > 0 && (
                      <>
                        <button
                          onClick={() => setShowHidden((v) => !v)}
                          className="w-full text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 py-1.5 text-center transition-colors"
                        >
                          {showHidden ? '非表示ドメインを隠す' : `非表示のドメイン ${ignoredDomainsForTab.length} 件を表示`}
                        </button>
                        {showHidden && ignoredDomainsForTab.map((domain) => (
                          <div key={domain} className="flex items-center gap-2 px-2 py-1.5 rounded bg-gray-50 dark:bg-gray-700/50 opacity-60">
                            <span className="min-w-0 flex-1 truncate font-mono text-sm text-gray-400 dark:text-gray-500 line-through">
                              {domain}
                            </span>
                            <button
                              onClick={() => handleUnhideDomain(domain)}
                              title="このドメインを再表示する"
                              className="shrink-0 text-xs text-blue-500 hover:text-blue-700 dark:hover:text-blue-300 px-1.5 py-0.5 rounded border border-blue-200 dark:border-blue-700 hover:border-blue-400 transition-colors"
                            >
                              再表示
                            </button>
                          </div>
                        ))}
                      </>
                    )}
                  </div>
                )}

                {selectedDomains.size > 0 && (
                  <div className="border-t border-gray-200 pt-4 dark:border-gray-700">
                    <button
                      onClick={handleAddToPolicy}
                      disabled={isSaving}
                      className="w-full px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white rounded-lg transition-colors disabled:cursor-not-allowed"
                    >
                      {isSaving ? '追加中...' : `選択した ${selectedDomains.size} 件をポリシーに追加`}
                    </button>
                    <p className="text-xs text-gray-400 mt-1 text-center">
                      {(policy.allowed_domains?.length ?? 0) > 0 ? '許可リストに追加されます' : '拒否リストから除外されます'}
                    </p>
                  </div>
                )}
              </>
            )}
          </div>

          <div className="flex items-center justify-end px-5 py-4 border-t border-gray-200 dark:border-gray-700">
            <button onClick={onClose} className="w-full px-4 py-2 text-sm text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-600 transition-colors sm:w-auto">
              一覧へ戻る
            </button>
          </div>
        </section>
      </div>
    </div>
  )
}

// ---- Settings Page ----
interface SandboxPolicySettingsPanelProps {
  onClose: () => void
  onSuccess: () => void
  editing?: SandboxPolicy | null
}

function SandboxPolicySettingsPanel({ onClose, onSuccess, editing }: SandboxPolicySettingsPanelProps) {
  const { getScopeParams } = useTeamScope()
  const [activeTab, setActiveTab] = useState<'basic' | 'domains' | 'evaluation'>('basic')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [mode, setMode] = useState<'allowlist' | 'denylist'>('allowlist')
  const [domains, setDomains] = useState('')
  const [countMode, setCountMode] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (editing) {
      setName(editing.name)
      setDescription(editing.description ?? '')
      if (editing.allowed_domains && editing.allowed_domains.length > 0) {
        setMode('allowlist')
        setDomains(editing.allowed_domains.join('\n'))
      } else if (editing.denied_domains && editing.denied_domains.length > 0) {
        setMode('denylist')
        setDomains(editing.denied_domains.join('\n'))
      } else {
        setMode('allowlist')
        setDomains('')
      }
      setCountMode(editing.count_mode ?? false)
    } else {
      setName('')
      setDescription('')
      setMode('allowlist')
      setDomains('')
      setCountMode(false)
    }
    setError(null)
    setActiveTab('basic')
  }, [editing])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) { setError('名前は必須です'); return }
    setIsSubmitting(true)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const domainList = domains.split('\n').map((d) => d.trim()).filter(Boolean)
      if (editing) {
        const data: UpdateSandboxPolicyRequest = {
          name: name.trim(),
          description: description.trim() || undefined,
          ...(mode === 'allowlist' ? { allowed_domains: domainList, denied_domains: [] } : { denied_domains: domainList, allowed_domains: [] }),
          count_mode: countMode,
        }
        await client.updateSandboxPolicy(editing.id, data)
      } else {
        const scopeParams = getScopeParams()
        const data: CreateSandboxPolicyRequest = {
          name: name.trim(),
          description: description.trim() || undefined,
          ...(mode === 'allowlist' ? { allowed_domains: domainList } : { denied_domains: domainList }),
          count_mode: countMode,
          scope: (scopeParams as { scope?: 'user' | 'team' }).scope ?? 'user',
          ...(scopeParams as { team_id?: string }).team_id ? { team_id: (scopeParams as { team_id?: string }).team_id } : {},
        }
        await client.createSandboxPolicy(data)
      }
      onSuccess()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存に失敗しました')
    } finally {
      setIsSubmitting(false)
    }
  }

  const tabs = [
    {
      id: 'basic' as const,
      label: '基本設定',
      description: '名前と説明',
    },
    {
      id: 'domains' as const,
      label: 'ドメイン',
      description: mode === 'allowlist' ? '許可リスト' : '拒否リスト',
    },
    {
      id: 'evaluation' as const,
      label: '評価',
      description: countMode ? 'カウントのみ' : '通常適用',
    },
  ]

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <button
            type="button"
            onClick={onClose}
            className="mb-3 inline-flex items-center gap-1.5 text-sm font-medium text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200"
          >
            <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
            </svg>
            一覧へ戻る
          </button>
          <h1 className="text-xl font-bold text-gray-900 dark:text-white">
            {editing ? 'サンドボックスポリシーを編集' : '新しいサンドボックスポリシー'}
          </h1>
          <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
            ネットワークフィルターの対象ドメインと適用方法を設定します。
          </p>
        </div>
        <div className="grid grid-cols-2 gap-2 sm:flex sm:shrink-0 sm:items-center">
          <button type="button" onClick={onClose} disabled={isSubmitting} className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors disabled:opacity-50">
            キャンセル
          </button>
          <button type="submit" form="sandbox-policy-settings-form" disabled={isSubmitting} className="px-4 py-2 text-sm text-white bg-blue-600 hover:bg-blue-700 rounded-lg transition-colors disabled:opacity-50">
            {isSubmitting ? '保存中...' : editing ? '保存' : '作成'}
          </button>
        </div>
      </div>

      <div className="grid gap-5 lg:grid-cols-[220px_minmax(0,1fr)]">
        <aside className="lg:sticky lg:top-20 lg:self-start">
          <div className="rounded-lg border border-gray-200 bg-white p-2 dark:border-gray-700 dark:bg-gray-800">
            <nav className="grid grid-cols-3 gap-2 lg:grid-cols-1" aria-label="サンドボックスポリシー設定">
              {tabs.map((tab) => (
                <button
                  key={tab.id}
                  type="button"
                  onClick={() => setActiveTab(tab.id)}
                  className={`min-w-0 rounded-md px-3 py-2 text-left transition-colors ${
                    activeTab === tab.id
                      ? 'bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-200'
                      : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-100'
                  }`}
                >
                  <span className="block truncate text-sm font-medium">{tab.label}</span>
                  <span className="mt-0.5 block truncate text-xs opacity-75">{tab.description}</span>
                </button>
              ))}
            </nav>
          </div>
        </aside>

        <form id="sandbox-policy-settings-form" onSubmit={handleSubmit} className="min-w-0 rounded-lg border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
          {error && (
            <div className="mx-5 mt-5 rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-800 dark:bg-red-900/20">
              <p className="text-sm text-red-700 dark:text-red-400">{error}</p>
            </div>
          )}

          {activeTab === 'basic' && (
            <section className="space-y-5 p-5 md:p-6">
              <div>
                <h2 className="text-base font-semibold text-gray-900 dark:text-white">基本設定</h2>
                <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">一覧やセッションプロファイルで識別するための情報です。</p>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  名前 <span className="text-red-500">*</span>
                </label>
                <input
                  type="text"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="例: github-access"
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                  required
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">説明</label>
                <input
                  type="text"
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  placeholder="例: GitHub と npm へのアクセスを許可"
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>
            </section>
          )}

          {activeTab === 'domains' && (
            <section className="space-y-5 p-5 md:p-6">
              <div>
                <h2 className="text-base font-semibold text-gray-900 dark:text-white">ドメイン</h2>
                <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">許可または拒否するドメインを 1 行ずつ指定します。</p>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">モード</label>
                <div className="grid gap-3 sm:grid-cols-2">
                  {(['allowlist', 'denylist'] as const).map((m) => (
                    <label
                      key={m}
                      className={`flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors ${
                        mode === m
                          ? 'border-blue-400 bg-blue-50 dark:border-blue-600 dark:bg-blue-900/20'
                          : 'border-gray-200 hover:border-gray-300 dark:border-gray-600 dark:hover:border-gray-500'
                      }`}
                    >
                      <input type="radio" value={m} checked={mode === m} onChange={() => setMode(m)} className="mt-0.5 h-4 w-4 text-blue-600 focus:ring-blue-500" />
                      <span>
                        <span className="block text-sm font-medium text-gray-800 dark:text-gray-200">
                          {m === 'allowlist' ? '許可リスト' : '拒否リスト'}
                        </span>
                        <span className="mt-0.5 block text-xs text-gray-500 dark:text-gray-400">
                          {m === 'allowlist' ? '指定したドメインのみ許可します' : '指定したドメインをブロックします'}
                        </span>
                      </span>
                    </label>
                  ))}
                </div>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  ドメインリスト <span className="text-gray-400 font-normal">（ワイルドカード *.example.com 可）</span>
                </label>
                <textarea
                  value={domains}
                  onChange={(e) => setDomains(e.target.value)}
                  placeholder={mode === 'allowlist' ? 'github.com\n*.github.com\nregistry.npmjs.org' : 'example.com\nbad-domain.com'}
                  rows={12}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 font-mono resize-y"
                />
              </div>
            </section>
          )}

          {activeTab === 'evaluation' && (
            <section className="space-y-5 p-5 md:p-6">
              <div>
                <h2 className="text-base font-semibold text-gray-900 dark:text-white">評価</h2>
                <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">ポリシーを実際にブロックへ反映するか、記録だけにするかを選びます。</p>
              </div>
              <label className="flex cursor-pointer items-start gap-3 rounded-lg border border-gray-200 p-4 dark:border-gray-600">
                <input
                  type="checkbox"
                  checked={countMode}
                  onChange={(e) => setCountMode(e.target.checked)}
                  className="mt-0.5 h-4 w-4 rounded border-gray-300 dark:border-gray-600 text-blue-600 focus:ring-blue-500 cursor-pointer"
                />
                <div>
                  <span className="text-sm font-medium text-gray-700 dark:text-gray-300">カウントモード</span>
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                    ポリシーを評価してブロック対象ドメインを記録するが、トラフィックは実際にはブロックしない。本番適用前の監査に利用できる。
                  </p>
                </div>
              </label>
            </section>
          )}

          <div className="grid grid-cols-2 gap-3 border-t border-gray-200 px-5 py-4 dark:border-gray-700 sm:flex sm:items-center sm:justify-end">
            <button type="button" onClick={onClose} disabled={isSubmitting} className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-600 transition-colors disabled:opacity-50">
              キャンセル
            </button>
            <button type="submit" disabled={isSubmitting} className="px-4 py-2 text-sm text-white bg-blue-600 hover:bg-blue-700 rounded-lg transition-colors disabled:opacity-50">
              {isSubmitting ? '保存中...' : editing ? '保存' : '作成'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

// ---- Policy Card ----
function PolicyCard({
  policy,
  onEdit,
  onDelete,
  onImportDomains,
}: {
  policy: SandboxPolicy
  onEdit: () => void
  onDelete: () => void
  onImportDomains: () => void
}) {
  const domains = policy.allowed_domains?.length
    ? { mode: '許可リスト', list: policy.allowed_domains }
    : policy.denied_domains?.length
    ? { mode: '拒否リスト', list: policy.denied_domains }
    : null

  return (
    <div className="min-w-0 bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 p-5 flex flex-col gap-3">
      <div>
        <div className="flex min-w-0 items-center gap-2">
          <h3 className="min-w-0 truncate font-semibold text-gray-900 dark:text-white">{policy.name}</h3>
          {policy.count_mode && (
            <span className="inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300">
              カウント
            </span>
          )}
        </div>
        {policy.description && <p className="text-sm text-gray-500 dark:text-gray-400 mt-0.5">{policy.description}</p>}
      </div>
      <div className="flex flex-wrap gap-2">
        <button onClick={onImportDomains} title="検出ドメインを確認" className="text-xs px-2 py-1 rounded border border-blue-200 dark:border-blue-800 text-blue-600 dark:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20">
          検出ドメインを確認
        </button>
        <button onClick={onEdit} className="text-xs px-2 py-1 rounded border border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-700">編集</button>
        <button onClick={onDelete} className="text-xs px-2 py-1 rounded border border-red-200 dark:border-red-800 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20">削除</button>
      </div>
      {domains ? (
        <div>
          <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 mb-2">{domains.mode}</span>
          <ul className="text-xs font-mono text-gray-600 dark:text-gray-400 space-y-0.5">
            {domains.list.slice(0, 5).map((d) => <li key={d} className="truncate">{d}</li>)}
            {domains.list.length > 5 && <li className="text-gray-400">... 他 {domains.list.length - 5} 件</li>}
          </ul>
        </div>
      ) : (
        <p className="text-xs text-gray-400">ドメイン未設定</p>
      )}
      <CopyableResourceId id={policy.id} />
    </div>
  )
}

// ---- Main Page ----
export default function SandboxPoliciesPage() {
  const [policies, setPolicies] = useState<SandboxPolicy[]>([])
  const [loading, setLoading] = useState(true)
  const [isFormOpen, setIsFormOpen] = useState(false)
  const [editing, setEditing] = useState<SandboxPolicy | null>(null)
  const [importTarget, setImportTarget] = useState<SandboxPolicy | null>(null)
  const [sidebarVisible, setSidebarVisible] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const res = await client.getSandboxPolicies()
      setPolicies(res.sandbox_policies || [])
    } catch {
      setPolicies([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const handleDelete = async (policy: SandboxPolicy) => {
    if (!confirm(`「${policy.name}」を削除しますか？`)) return
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.deleteSandboxPolicy(policy.id)
      await load()
    } catch (err) {
      alert(err instanceof Error ? err.message : '削除に失敗しました')
    }
  }

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar
        title="Sandbox Policies"
        showSettingsButton={true}
        showFilterButton={true}
        filterButtonText="ナビゲーション"
        onFilterToggle={() => setSidebarVisible(!sidebarVisible)}
      >
        <div className="md:hidden">
          <NavigationTabs />
        </div>
      </TopBar>

      <div className="flex min-w-0">
        <TagFilterSidebar
          onFiltersChange={() => {}}
          currentFilters={{}}
          isVisible={sidebarVisible}
          onToggleVisibility={() => setSidebarVisible(!sidebarVisible)}
        />

        <div className="min-w-0 flex-1 px-4 md:px-6 lg:px-8 pt-6 md:pt-8 pb-6 md:pb-8">
          {importTarget ? (
            <SessionDomainImportPanel
              onClose={() => setImportTarget(null)}
              policy={importTarget}
              onImported={load}
            />
          ) : isFormOpen ? (
            <SandboxPolicySettingsPanel
              onClose={() => { setIsFormOpen(false); setEditing(null) }}
              onSuccess={() => { setIsFormOpen(false); setEditing(null); load() }}
              editing={editing}
            />
          ) : (
            <>
            <div className="mb-6 flex items-start justify-between gap-3">
              <div className="min-w-0">
                <h1 className="text-xl font-bold text-gray-900 dark:text-white">サンドボックスポリシー</h1>
                <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
                  名前付きのネットワークフィルタールールセットを定義し、セッションプロファイルから参照できます。
                </p>
              </div>
              <button
                onClick={() => { setEditing(null); setIsFormOpen(true) }}
                className="hidden md:inline-flex shrink-0 items-center px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors"
              >
                <svg className="w-4 h-4 mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                </svg>
                新しいポリシー
              </button>
            </div>

            {loading ? (
              <div className="text-center py-12 text-gray-400">読み込み中...</div>
            ) : policies.length === 0 ? (
              <div className="text-center py-16">
                <div className="w-12 h-12 rounded-full bg-gray-100 dark:bg-gray-800 flex items-center justify-center mx-auto mb-4">
                  <svg className="w-6 h-6 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
                  </svg>
                </div>
                <p className="text-gray-500 dark:text-gray-400 mb-4">ポリシーがまだありません</p>
                <button
                  onClick={() => { setEditing(null); setIsFormOpen(true) }}
                  className="inline-flex items-center px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors"
                >
                  最初のポリシーを作成
                </button>
              </div>
            ) : (
              <div className="grid min-w-0 gap-4 sm:grid-cols-2">
                {policies.map((policy) => (
                  <PolicyCard
                    key={policy.id}
                    policy={policy}
                    onEdit={() => { setEditing(policy); setIsFormOpen(true) }}
                    onDelete={() => handleDelete(policy)}
                    onImportDomains={() => setImportTarget(policy)}
                  />
                ))}
              </div>
            )}
            </>
          )}
        </div>
      </div>

      {/* Mobile FAB */}
      {!isFormOpen && !importTarget && (
        <button
          onClick={() => { setEditing(null); setIsFormOpen(true) }}
          className="md:hidden fixed bottom-6 right-6 w-14 h-14 bg-blue-600 hover:bg-blue-700 text-white rounded-full shadow-lg flex items-center justify-center z-50 transition-colors"
          aria-label="新しいポリシー"
        >
          <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
          </svg>
        </button>
      )}
    </main>
  )
}
