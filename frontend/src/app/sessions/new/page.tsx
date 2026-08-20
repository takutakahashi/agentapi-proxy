'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { createAgentAPIClient } from '../../../lib/api'
import type { AgentAPIProxyClient } from '../../../lib/agentapi-proxy-client'
import { InitialMessageCache } from '../../../utils/initialMessageCache'
import { messageTemplateManager } from '../../../utils/messageTemplateManager'
import { MessageTemplate } from '../../../types/messageTemplate'
import { recentMessagesManager } from '../../../utils/recentMessagesManager'
import { OrganizationHistory } from '../../../utils/organizationHistory'
import { addRepositoryToHistory, AgentApiType, getACPServerEnabled } from '../../../types/settings'
import { AvailableManager } from '../../../types/settings'
import { createAgentAPIProxyClientFromStorage } from '../../../lib/agentapi-proxy-client'
import { createACPServerClientFromStorage } from '../../../lib/acp-server-client'
import TopBar from '../../components/TopBar'
import SessionCreationProgressModal from '../../components/SessionCreationProgressModal'
import SessionProfileSelect from '../../components/SessionProfileSelect'
import { SessionCreationProgress, SessionCreationStatus } from '../../../types/sessionProgress'
import { useTeamScope } from '../../../contexts/TeamScopeContext'

type CheckoutTarget = 'branch' | 'pr' | ''

const getAgentTypeLabel = (agentType: AgentApiType): string => {
  switch (agentType) {
    case 'auto':
      return '自動選択'
    case 'claude-legacy':
      return 'Claude Legacy'
    case 'claude-acp':
      return 'Claude ACP'
    case 'codex-acp':
      return 'Codex ACP'
    case 'pi-ollama':
      return 'Pi Ollama'
    case 'cursor':
      return 'Cursor ACP'
    default:
      return 'Claude ACP'
  }
}

function buildRepositoryTags(
  repo: string,
  checkoutTarget: CheckoutTarget,
  branch: string,
  prNumber: string
): Record<string, string> {
  const tags: Record<string, string> = {}
  const trimmedRepo = repo.trim()
  const trimmedBranch = branch.trim()
  const trimmedPrNumber = prNumber.trim()

  if (trimmedRepo) {
    tags.repository = trimmedRepo
  }

  if (trimmedRepo && checkoutTarget === 'branch' && trimmedBranch) {
    tags.branch = trimmedBranch
  }

  if (trimmedRepo && checkoutTarget === 'pr' && trimmedPrNumber) {
    tags.pr_number = trimmedPrNumber
  }

  return tags
}

export default function NewSessionPage() {
  const { selectedTeam } = useTeamScope()
  const router = useRouter()
  const [initialMessage, setInitialMessage] = useState('')
  const [freeFormRepository, setFreeFormRepository] = useState('')
  const [checkoutTarget, setCheckoutTarget] = useState<CheckoutTarget>('')
  const [checkoutBranch, setCheckoutBranch] = useState('')
  const [checkoutPrNumber, setCheckoutPrNumber] = useState('')
  const [isCreating, setIsCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [statusMessage, setStatusMessage] = useState('')
  const [templates, setTemplates] = useState<MessageTemplate[]>([])
  const [recentMessages, setRecentMessages] = useState<string[]>([])
  const [showTemplateModal, setShowTemplateModal] = useState(false)
  const [repositoryHistory, setRepositoryHistory] = useState<string[]>([])
  const [repositoryList, setRepositoryList] = useState<string[]>([])
  const [showFreeFormRepositorySuggestions, setShowFreeFormRepositorySuggestions] = useState(false)
  const [showProgressModal, setShowProgressModal] = useState(false)
  const [creationProgress, setCreationProgress] = useState<SessionCreationProgress | null>(null)
  const [showAdvancedSettings, setShowAdvancedSettings] = useState(false)
  const [selectedAgentType, setSelectedAgentType] = useState<AgentApiType>('default')
  const [availableManagers, setAvailableManagers] = useState<AvailableManager[]>([])
  const [selectedManagerId, setSelectedManagerId] = useState<string>('')
  const [cycleEnabled, setCycleEnabled] = useState(false)
  const [cycleMessage, setCycleMessage] = useState('')
  const [cycleMaxCount, setCycleMaxCount] = useState(10)
  const [sessionProfileId, setSessionProfileId] = useState('')
  const [dockerEnabled, setDockerEnabled] = useState(false)
  const [dockerRegistries, setDockerRegistries] = useState<Array<{ server: string; username: string; password: string; secretName: string; insecure: boolean }>>([])
  const [sessionTTL, setSessionTTL] = useState('')

  const addDockerRegistry = () => {
    setDockerRegistries(prev => [...prev, { server: '', username: '', password: '', secretName: '', insecure: false }])
  }
  const removeDockerRegistry = (index: number) => {
    setDockerRegistries(prev => prev.filter((_, i) => i !== index))
  }
  const updateDockerRegistry = (index: number, field: string, value: string | boolean) => {
    setDockerRegistries(prev => prev.map((r, i) => i === index ? { ...r, [field]: value } : r))
  }

  useEffect(() => {
    loadTemplates()
    loadRecentMessages()
    loadAvailableManagers()
  }, [])

  // ESCキーで戻る
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        if (showTemplateModal) {
          setShowTemplateModal(false)
        } else {
          router.back()
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [showTemplateModal, router])

  const loadTemplates = async () => {
    try {
      const allTemplates = await messageTemplateManager.getTemplates()
      setTemplates(allTemplates)
    } catch (error) {
      console.error('Failed to load templates:', error)
      setTemplates([])
    }
  }

  const loadRecentMessages = async () => {
    try {
      const messages = await recentMessagesManager.getRecentMessages()
      setRecentMessages(messages.map(msg => msg.content))
    } catch (error) {
      console.error('Failed to load recent messages:', error)
    }
  }

  const loadAvailableManagers = async () => {
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const managers = await client.getAvailableManagers()
      setAvailableManagers(managers)
    } catch (error) {
      console.error('Failed to load available managers:', error)
    }
  }

  // 進捗状態を更新するヘルパー関数
  const updateProgress = (status: SessionCreationStatus, errorMessage?: string) => {
    setCreationProgress(prev => {
      if (!prev) return null
      return {
        ...prev,
        status,
        errorMessage
      }
    })
  }

  const createSession = async (
    client: AgentAPIProxyClient,
    message: string,
    repo: string,
    agentType: AgentApiType,
    pool: string
  ): Promise<{ success: boolean; error?: string }> => {
    try {
      console.log('Starting session creation with initial message...')
      updateProgress('creating')

      const tags = buildRepositoryTags(repo, checkoutTarget, checkoutBranch, checkoutPrNumber)

      const environment: Record<string, string> = {}
      if (repo) {
        environment.REPOSITORY = repo
      }

      // Compute scope parameters directly from selectedTeam
      const scopeParams: { scope: 'user' | 'team'; team_id?: string } = selectedTeam
        ? { scope: 'team', team_id: selectedTeam }
        : { scope: 'user' }

      console.log('[NewSessionPage] Creating session with scope params:', scopeParams)

      // Build params object
      const params: Record<string, unknown> = {
        message: message
      }

      // agent_type はデフォルト以外の場合のみ送信
      if (agentType !== 'default') {
        params.agent_type = agentType
      }

      // セッションマネージャーが指定されている場合は送信
      if (pool) {
        tags['allocator.pool'] = pool
      }

      // サイクルセッションが有効な場合はcycle_messageを送信
      if (cycleEnabled && cycleMessage.trim()) {
        params.cycle_message = cycleMessage.trim()
        if (cycleMaxCount > 0) {
          params.cycle_max_count = cycleMaxCount
        }
      }

      // DinDが有効な場合はdockerを送信
      if (dockerEnabled) {
        const registries = dockerRegistries
          .filter(r => r.server || r.username || r.secretName)
          .map(r => ({
            ...(r.server ? { server: r.server } : {}),
            ...(r.secretName ? { secret_name: r.secretName } : {}),
            ...(r.username && !r.secretName ? { username: r.username, password: r.password } : {}),
            ...(r.insecure ? { insecure: true } : {}),
          }))
        params.docker = {
          enabled: true,
          ...(registries.length > 0 ? { registries } : {}),
        }
      }

      // セッション TTL が指定されている場合は送信
      if (sessionTTL.trim()) {
        params.session_ttl = sessionTTL.trim()
      }

      console.log('[NewSessionPage] Final params:', params)

      const session = await client.start({
        environment,
        metadata: {
          description: message
        },
        tags: Object.keys(tags).length > 0 ? tags : undefined,
        params,
        ...(sessionProfileId ? { session_profile_id: sessionProfileId } : {}),
        ...scopeParams
      })
      console.log('Session created with initial message:', session)

      updateProgress('completed')
      return { success: true }
    } catch (err) {
      console.error('Session creation failed:', err)
      const errorMessage = err instanceof Error ? err.message : 'セッション作成に失敗しました'
      updateProgress('failed', errorMessage)
      return {
        success: false,
        error: errorMessage
      }
    }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!initialMessage.trim()) {
      setError('初期メッセージを入力してください')
      return
    }

    setIsCreating(true)
    setError(null)
    setStatusMessage('')

    const acpServerEnabled = getACPServerEnabled()
    const client = createAgentAPIClient()
    const currentMessage = initialMessage.trim()
    const currentRepository = freeFormRepository.trim()

    InitialMessageCache.addMessage(currentMessage)
    await recentMessagesManager.saveMessage(currentMessage)

    if (currentRepository) {
      console.log('Adding repository to history before session creation:', { currentRepository })
      try {
        addRepositoryToHistory(currentRepository)
        console.log('Repository added to history successfully (pre-session)')
      } catch (error) {
        console.error('Failed to add repository to history (pre-session):', error)
      }
    }

    // ACP サーバーモードが有効な場合は ACP クライアントでセッションを作成
    let result: { success: boolean; error?: string }
    if (acpServerEnabled) {
      try {
        setCreationProgress({
          status: 'creating',
          message: currentMessage,
          repository: currentRepository || undefined,
          startTime: new Date()
        })
        setShowProgressModal(true)
        const acpClient = createACPServerClientFromStorage()
        await acpClient.initialize()
        const tags = buildRepositoryTags(currentRepository, checkoutTarget, checkoutBranch, checkoutPrNumber)
        if (selectedTeam) tags.team = selectedTeam
        if (selectedManagerId) tags['allocator.pool'] = selectedManagerId
        // cwd: リポジトリが指定されていれば /home/user/workdir/<repo名> を使用、なければデフォルト
        const repoPart = currentRepository ? currentRepository.split('/').pop() : ''
        const cwd = repoPart ? `/home/user/workdir/${repoPart}` : '/home/user'
        await acpClient.createSession({
          cwd,
          message: currentMessage,
          agentType: selectedAgentType !== 'default' ? selectedAgentType : undefined,
          tags,
        })
        setCreationProgress(prev => prev ? { ...prev, status: 'completed' } : null)
        result = { success: true }
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : 'ACP セッション作成に失敗しました'
        setCreationProgress(prev => prev ? { ...prev, status: 'failed', errorMessage } : null)
        result = { success: false, error: errorMessage }
      }
    } else {
      // 通常の REST API でセッションを作成
      setCreationProgress({
        status: 'creating',
        message: currentMessage,
        repository: currentRepository || undefined,
        startTime: new Date()
      })
      setShowProgressModal(true)
      result = await createSession(
        client,
        currentMessage,
        currentRepository,
        selectedAgentType,
        selectedManagerId
      )
    }

    if (result.success) {
      // 成功したら少し待ってから /chats に遷移
      setTimeout(() => {
        router.push('/chats')
      }, 1500)
    } else {
      // 失敗した場合はモーダルにエラーが表示される
      setIsCreating(false)
    }
  }

  const handleCloseProgressModal = () => {
    setShowProgressModal(false)
    setCreationProgress(null)
    setIsCreating(false)
  }

  const handleRetry = () => {
    setShowProgressModal(false)
    setCreationProgress(null)
    setIsCreating(false)
  }

  const handleCancel = () => {
    router.back()
  }

  const handleFreeFormRepositoryChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const value = e.target.value
    setFreeFormRepository(value)

    if (value.trim()) {
      const { history, repositories } = OrganizationHistory.getRepositorySuggestionsGrouped(value)
      setRepositoryHistory(history)
      setRepositoryList(repositories)
      setShowFreeFormRepositorySuggestions(history.length > 0 || repositories.length > 0)
    } else {
      setCheckoutTarget('')
      setCheckoutBranch('')
      setCheckoutPrNumber('')
      setShowFreeFormRepositorySuggestions(false)
    }
  }

  const handleCheckoutTargetChange = (target: Exclude<CheckoutTarget, ''>) => {
    setCheckoutTarget(target)
    if (target === 'branch') {
      setCheckoutPrNumber('')
    } else {
      setCheckoutBranch('')
    }
  }

  const handleFreeFormRepositoryFocus = () => {
    const { history, repositories } = OrganizationHistory.getRepositorySuggestionsGrouped()
    setRepositoryHistory(history)
    setRepositoryList(repositories)
    setShowFreeFormRepositorySuggestions(history.length > 0 || repositories.length > 0)
  }

  const handleFreeFormRepositoryBlur = () => {
    setTimeout(() => setShowFreeFormRepositorySuggestions(false), 150)
  }

  const selectFreeFormRepositorySuggestion = (suggestion: string) => {
    setFreeFormRepository(suggestion)
    setShowFreeFormRepositorySuggestions(false)
  }

  const selectTemplate = (template: MessageTemplate) => {
    setInitialMessage(template.content)
    setShowTemplateModal(false)
  }

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar
        title="新しいセッション"
        showSettingsButton={true}
      />

      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        <div className="bg-white dark:bg-gray-800 rounded-xl shadow-sm border border-gray-200 dark:border-gray-700">
          {/* ヘッダー */}
          <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
            <h1 className="text-2xl font-semibold text-gray-900 dark:text-white">
              新しいセッションを開始
            </h1>
            <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
              セッションの設定を入力してください
            </p>
          </div>

          {/* フォーム */}
          <form onSubmit={handleSubmit} className="p-6 space-y-6">
            {/* 初期メッセージ */}
            <div className="relative">
              <label htmlFor="initialMessage" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                初期メッセージ <span className="text-red-500">*</span>
              </label>
              <div className="flex items-start gap-3">
                <textarea
                  id="initialMessage"
                  value={initialMessage}
                  onChange={(e) => setInitialMessage(e.target.value)}
                  placeholder="このセッションで何をしたいか説明してください..."
                  className="flex-1 px-4 py-3 border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white resize-y min-h-[200px] text-base"
                  disabled={isCreating}
                  required
                />
                <button
                  type="button"
                  onClick={() => setShowTemplateModal(true)}
                  className="px-4 py-3 bg-blue-600 hover:bg-blue-700 text-white rounded-lg transition-colors flex items-center flex-shrink-0"
                  title="テンプレートから選択"
                  disabled={isCreating}
                >
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                  </svg>
                </button>
              </div>
            </div>

            {/* リポジトリ入力 */}
            <div className="relative">
              <label htmlFor="freeFormRepository" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                対象リポジトリ
              </label>
              <input
                id="freeFormRepository"
                type="text"
                value={freeFormRepository}
                onChange={handleFreeFormRepositoryChange}
                onFocus={handleFreeFormRepositoryFocus}
                onBlur={handleFreeFormRepositoryBlur}
                className="w-full px-4 py-3 border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-base"
                placeholder="例: owner/repository-name"
                disabled={isCreating}
              />

              {/* サジェストドロップダウン */}
              {showFreeFormRepositorySuggestions && (repositoryHistory.length > 0 || repositoryList.length > 0) && (
                <div className="absolute top-full left-0 right-0 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg shadow-xl z-50 mt-1 max-h-80 overflow-y-auto">
                  {/* リポジトリ履歴セクション */}
                  {repositoryHistory.length > 0 && (
                    <>
                      <div className="p-3 border-b border-gray-200 dark:border-gray-700">
                        <span className="text-xs font-medium text-gray-500 dark:text-gray-400">リポジトリ履歴</span>
                      </div>
                      <div className="p-3 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
                        {repositoryHistory.map((suggestion, index) => (
                          <button
                            key={`history-${index}`}
                            type="button"
                            onClick={() => selectFreeFormRepositorySuggestion(suggestion)}
                            className="text-left px-3 py-2 hover:bg-blue-50 dark:hover:bg-blue-900/30 text-gray-900 dark:text-white font-mono text-sm rounded-md border border-gray-200 dark:border-gray-600 hover:border-blue-300 dark:hover:border-blue-700 transition-colors break-all"
                            title={suggestion}
                          >
                            <span className="flex items-center gap-2">
                              <svg className="w-4 h-4 text-blue-500 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
                              </svg>
                              <span className="break-all">{suggestion}</span>
                            </span>
                          </button>
                        ))}
                      </div>
                    </>
                  )}

                  {/* リポジトリ一覧セクション */}
                  {repositoryList.length > 0 && (
                    <>
                      <div className="p-3 border-b border-gray-200 dark:border-gray-700">
                        <span className="text-xs font-medium text-gray-500 dark:text-gray-400">リポジトリ一覧</span>
                      </div>
                      <div className="p-3 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
                        {repositoryList.map((suggestion, index) => (
                          <button
                            key={`repo-${index}`}
                            type="button"
                            onClick={() => selectFreeFormRepositorySuggestion(suggestion)}
                            className="text-left px-3 py-2 hover:bg-blue-50 dark:hover:bg-blue-900/30 text-gray-900 dark:text-white font-mono text-sm rounded-md border border-gray-200 dark:border-gray-600 hover:border-blue-300 dark:hover:border-blue-700 transition-colors break-all"
                            title={suggestion}
                          >
                            <span className="flex items-center gap-2">
                              <svg className="w-4 h-4 text-gray-400 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
                              </svg>
                              <span className="break-all">{suggestion}</span>
                            </span>
                          </button>
                        ))}
                      </div>
                    </>
                  )}
                </div>
              )}

              <p className="text-sm text-gray-500 dark:text-gray-400 mt-2">
                リポジトリを指定しない場合は一般的なチャットになります
              </p>
            </div>

            {/* Checkout 指定 */}
            <div>
              <div className="flex items-center justify-between gap-3 mb-2">
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                  Checkout 対象
                </label>
                {!freeFormRepository.trim() && (
                  <span className="text-xs text-gray-400 dark:text-gray-500">
                    リポジトリ指定時に有効
                  </span>
                )}
              </div>
              <div className="flex flex-wrap gap-2">
                <button
                  type="button"
                  onClick={() => handleCheckoutTargetChange('branch')}
                  disabled={isCreating || !freeFormRepository.trim()}
                  aria-pressed={checkoutTarget === 'branch'}
                  className={`px-3 py-2 rounded-lg border text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed ${
                    checkoutTarget === 'branch'
                      ? 'border-blue-500 bg-blue-50 text-blue-700 dark:bg-blue-900/20 dark:text-blue-300'
                      : 'border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:border-blue-300 dark:hover:border-blue-700'
                  }`}
                >
                  ブランチ
                </button>
                <button
                  type="button"
                  onClick={() => handleCheckoutTargetChange('pr')}
                  disabled={isCreating || !freeFormRepository.trim()}
                  aria-pressed={checkoutTarget === 'pr'}
                  className={`px-3 py-2 rounded-lg border text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed ${
                    checkoutTarget === 'pr'
                      ? 'border-blue-500 bg-blue-50 text-blue-700 dark:bg-blue-900/20 dark:text-blue-300'
                      : 'border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:border-blue-300 dark:hover:border-blue-700'
                  }`}
                >
                  Pull Request
                </button>
              </div>

              {freeFormRepository.trim() && checkoutTarget === 'branch' && (
                <div className="mt-3">
                  <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                    ブランチ名
                  </label>
                  <input
                    type="text"
                    value={checkoutBranch}
                    onChange={(e) => {
                      setCheckoutBranch(e.target.value)
                      if (e.target.value.trim()) setCheckoutPrNumber('')
                    }}
                    placeholder="例: feature/session-form"
                    disabled={isCreating || !freeFormRepository.trim()}
                    className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm font-mono disabled:bg-gray-100 dark:disabled:bg-gray-800 disabled:text-gray-400"
                  />
                </div>
              )}

              {freeFormRepository.trim() && checkoutTarget === 'pr' && (
                <div className="mt-3">
                  <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                    PR 番号
                  </label>
                  <input
                    type="text"
                    inputMode="numeric"
                    value={checkoutPrNumber}
                    onChange={(e) => {
                      setCheckoutPrNumber(e.target.value)
                      if (e.target.value.trim()) setCheckoutBranch('')
                    }}
                    placeholder="例: 123"
                    disabled={isCreating || !freeFormRepository.trim()}
                    className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm font-mono disabled:bg-gray-100 dark:disabled:bg-gray-800 disabled:text-gray-400"
                  />
                </div>
              )}
            </div>

            {/* セッションプロファイル */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                セッションプロファイル
              </label>
              <SessionProfileSelect
                value={sessionProfileId}
                onChange={setSessionProfileId}
                disabled={isCreating}
              />
              <p className="text-sm text-gray-500 dark:text-gray-400 mt-2">
                プロファイルを選択すると、環境変数・タグ・テンプレートなどの設定を適用します
              </p>
            </div>

            {/* その他の設定 */}
            <div>
              <button
                type="button"
                onClick={() => setShowAdvancedSettings(!showAdvancedSettings)}
                className="flex items-center gap-1.5 text-xs text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-300 transition-colors disabled:opacity-40"
                disabled={isCreating}
              >
                <svg
                  className={`w-3 h-3 transition-transform ${showAdvancedSettings ? 'rotate-180' : ''}`}
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                </svg>
                その他の設定
                {selectedAgentType !== 'default' && (
                  <span className="px-1.5 py-0.5 bg-blue-100 dark:bg-blue-900/40 text-blue-600 dark:text-blue-400 text-xs rounded-full">
                    {getAgentTypeLabel(selectedAgentType)}
                  </span>
                )}
                {selectedManagerId !== '' && (
                  <span className="px-1.5 py-0.5 bg-green-100 dark:bg-green-900/40 text-green-600 dark:text-green-400 text-xs rounded-full">
                    {availableManagers.find(m => m.pool === selectedManagerId)?.pool ?? 'カスタム'}
                  </span>
                )}
                {cycleEnabled && cycleMessage.trim() && (
                  <span className="px-1.5 py-0.5 bg-purple-100 dark:bg-purple-900/40 text-purple-600 dark:text-purple-400 text-xs rounded-full">
                    サイクル{cycleMaxCount > 0 ? ` (${cycleMaxCount}回)` : ''}
                  </span>
                )}
              </button>

              {showAdvancedSettings && (
                <div className="mt-3 pl-1 space-y-4">
                  {/* セッションマネージャー選択 */}
                  {availableManagers.length > 0 && (
                    <div>
                      <p className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">セッションマネージャー</p>
                      <div className="space-y-1.5">
                        {/* ローカル（マネージャーなし）オプション */}
                        <label
                          className={`flex items-start gap-3 p-2.5 rounded-lg border cursor-pointer transition-colors ${
                            selectedManagerId === ''
                              ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
                              : 'border-gray-200 dark:border-gray-600 hover:border-gray-300 dark:hover:border-gray-500 hover:bg-gray-50 dark:hover:bg-gray-700/50'
                          } ${isCreating ? 'opacity-50 cursor-not-allowed' : ''}`}
                        >
                          <input
                            type="radio"
                            name="session-manager"
                            value=""
                            checked={selectedManagerId === ''}
                            onChange={() => setSelectedManagerId('')}
                            disabled={isCreating}
                            className="mt-0.5 w-3.5 h-3.5 text-blue-600 border-gray-300 dark:border-gray-600 focus:ring-blue-500 flex-shrink-0"
                          />
                          <span className="flex-1 min-w-0">
                            <span className="block text-xs font-medium text-gray-800 dark:text-gray-200">ローカル</span>
                            <span className="block text-xs text-gray-400 dark:text-gray-500 mt-0.5">このサーバー上で作成</span>
                          </span>
                        </label>
                        {/* 各マネージャーオプション */}
                        {availableManagers.map((m) => (
                          <label
                            key={`${m.source}:${m.source_name}:${m.pool}`}
                            className={`flex items-start gap-3 p-2.5 rounded-lg border cursor-pointer transition-colors ${
                              selectedManagerId === m.pool
                                ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
                                : 'border-gray-200 dark:border-gray-600 hover:border-gray-300 dark:hover:border-gray-500 hover:bg-gray-50 dark:hover:bg-gray-700/50'
                            } ${isCreating ? 'opacity-50 cursor-not-allowed' : ''}`}
                          >
                            <input
                              type="radio"
                              name="session-manager"
                              value={m.pool}
                              checked={selectedManagerId === m.pool}
                              onChange={() => setSelectedManagerId(m.pool)}
                              disabled={isCreating}
                              className="mt-0.5 w-3.5 h-3.5 text-blue-600 border-gray-300 dark:border-gray-600 focus:ring-blue-500 flex-shrink-0"
                            />
                            <span className="flex-1 min-w-0">
                              <span className="flex items-center gap-1.5 flex-wrap">
                                <span className="text-xs font-medium text-gray-800 dark:text-gray-200 truncate">{m.pool}</span>
                                <span className="px-1 py-0.5 text-xs bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-400 rounded flex-shrink-0">
                                  {m.source === 'team' ? `チーム: ${m.source_name}` : '個人'}
                                </span>
                              </span>
                              <span className="block text-xs text-gray-400 dark:text-gray-500 mt-0.5 truncate" title="allocator 接続">
                                {m.name}
                              </span>
                            </span>
                          </label>
                        ))}
                      </div>
                    </div>
                  )}
                  {/* サイクルセッション */}
                  <div>
                    <div className="flex items-center justify-between mb-2">
                      <p className="text-xs font-medium text-gray-600 dark:text-gray-400">サイクルセッション</p>
                      <label className="flex items-center gap-2 cursor-pointer">
                        <span className="text-xs text-gray-400 dark:text-gray-500">
                          {cycleEnabled ? '有効' : '無効'}
                        </span>
                        <button
                          type="button"
                          role="switch"
                          aria-checked={cycleEnabled}
                          onClick={() => setCycleEnabled(!cycleEnabled)}
                          disabled={isCreating}
                          className={`relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-purple-500 focus:ring-offset-1 disabled:opacity-50 disabled:cursor-not-allowed ${
                            cycleEnabled ? 'bg-purple-600' : 'bg-gray-300 dark:bg-gray-600'
                          }`}
                        >
                          <span
                            className={`pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
                              cycleEnabled ? 'translate-x-4' : 'translate-x-0'
                            }`}
                          />
                        </button>
                      </label>
                    </div>
                    <p className="text-xs text-gray-400 dark:text-gray-500 mb-3">
                      Claudeが停止するたびに指定したメッセージを自動送信し、タスクを繰り返します。サイクルを終了する条件を満たした場合に自動的に停止します。
                    </p>
                    {cycleEnabled && (
                      <div className="space-y-3 pl-1">
                        <div>
                          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                            サイクルメッセージ <span className="text-red-500">*</span>
                          </label>
                          <textarea
                            value={cycleMessage}
                            onChange={(e) => setCycleMessage(e.target.value)}
                            placeholder="例: タスクを続けてください。サイクルを終了する条件: すべてのテストが通過したとき。"
                            rows={3}
                            disabled={isCreating}
                            className="w-full px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 dark:bg-gray-700 dark:text-white resize-y"
                          />
                          <p className="text-xs text-gray-400 dark:text-gray-500 mt-1">
                            Claudeが停止するたびに送信されるメッセージです。サイクルを終了する条件を記載してください。
                          </p>
                        </div>
                        <div>
                          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                            最大サイクル数
                          </label>
                          <input
                            type="number"
                            min={0}
                            value={cycleMaxCount}
                            onChange={(e) => setCycleMaxCount(Math.max(0, parseInt(e.target.value) || 0))}
                            disabled={isCreating}
                            className="w-32 px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 dark:bg-gray-700 dark:text-white"
                          />
                          <p className="text-xs text-gray-400 dark:text-gray-500 mt-1">
                            0 の場合は無制限。<code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">/tmp/check/CYCLE_COUNT</code> で進捗を確認できます。
                          </p>
                        </div>
                      </div>
                    )}
                  </div>

                  {/* Docker in Docker (DinD) */}
                  <div>
                    <div className="flex items-center justify-between mb-2">
                      <p className="text-xs font-medium text-gray-600 dark:text-gray-400">Docker in Docker (DinD)</p>
                      <label className="flex items-center gap-2 cursor-pointer">
                        <span className="text-xs text-gray-400 dark:text-gray-500">
                          {dockerEnabled ? '有効' : '無効'}
                        </span>
                        <button
                          type="button"
                          role="switch"
                          aria-checked={dockerEnabled}
                          onClick={() => setDockerEnabled(!dockerEnabled)}
                          disabled={isCreating}
                          className={`relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-1 disabled:opacity-50 disabled:cursor-not-allowed ${
                            dockerEnabled ? 'bg-blue-500' : 'bg-gray-300 dark:bg-gray-600'
                          }`}
                        >
                          <span
                            className={`pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
                              dockerEnabled ? 'translate-x-4' : 'translate-x-0'
                            }`}
                          />
                        </button>
                      </label>
                    </div>
                    <p className="text-xs text-gray-400 dark:text-gray-500 mb-3">
                      セッション Pod に docker:dind サイドカーを追加し、<code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">DOCKER_HOST</code> を自動設定します。
                    </p>
                    {dockerEnabled && (
                      <div className="pl-1 space-y-3">
                        <div className="flex items-center justify-between">
                          <p className="text-xs font-medium text-gray-600 dark:text-gray-400">認証済みレジストリ（任意）</p>
                          <button
                            type="button"
                            onClick={addDockerRegistry}
                            disabled={isCreating}
                            className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-700 disabled:opacity-50"
                          >
                            + 追加
                          </button>
                        </div>
                        {dockerRegistries.length === 0 && (
                          <p className="text-xs text-gray-400 dark:text-gray-500">
                            レジストリ認証が不要な場合は追加不要です。
                          </p>
                        )}
                        {dockerRegistries.map((registry, index) => (
                          <div key={index} className="border border-gray-200 dark:border-gray-600 rounded-lg p-3 space-y-2">
                            <div className="flex items-center justify-between">
                              <span className="text-xs font-medium text-gray-600 dark:text-gray-400">レジストリ #{index + 1}</span>
                              <button
                                type="button"
                                onClick={() => removeDockerRegistry(index)}
                                disabled={isCreating}
                                className="text-xs text-red-500 hover:text-red-600 disabled:opacity-50"
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
                                disabled={isCreating}
                                className="w-full px-2 py-1.5 text-xs border border-gray-300 dark:border-gray-600 rounded focus:outline-none focus:ring-1 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                              />
                            </div>
                            <div className="flex items-center gap-2">
                              <input
                                type="checkbox"
                                id={`insecure-${index}`}
                                checked={registry.insecure}
                                onChange={e => updateDockerRegistry(index, 'insecure', e.target.checked)}
                                disabled={isCreating}
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
                                disabled={isCreating}
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
                                    disabled={isCreating}
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
                                    disabled={isCreating}
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
                    <p className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">セッション自動削除 TTL</p>
                    <p className="text-xs text-gray-400 dark:text-gray-500 mb-2">最後のメッセージからこの時間が経過するとセッションを自動削除します。例: <code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">24h</code>、<code className="px-1 py-0.5 bg-gray-100 dark:bg-gray-700 rounded">168h</code>（空欄 = 自動削除なし）</p>
                    <input
                      type="text"
                      value={sessionTTL}
                      onChange={e => setSessionTTL(e.target.value)}
                      placeholder="例: 24h、72h、168h"
                      disabled={isCreating}
                      className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                    />
                  </div>

                  {/* エージェントタイプ */}
                  <div>
                    <p className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">エージェントタイプ</p>
                    {getACPServerEnabled() ? (
                      <div className="flex items-center space-x-2 px-3 py-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-700 rounded-md">
                        <svg className="w-4 h-4 text-blue-500 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
                        </svg>
                        <span className="text-xs text-blue-700 dark:text-blue-300">
                          ACP サーバーモード: <strong>{getAgentTypeLabel(selectedAgentType)}</strong>（{selectedAgentType !== 'default' ? selectedAgentType : 'auto'}）が使用されます
                        </span>
                      </div>
                    ) : (
                      <div className="space-y-2">
                      {([
                        { value: 'default', label: 'チーム設定', description: 'チーム設定（未設定なら自動選択）を使用' },
                        { value: 'auto', label: '自動選択', description: 'Codex auth.json があれば Codex ACP、それ以外は Claude ACP' },
                        { value: 'claude-legacy', label: 'Claude Legacy', description: 'agent_type=claude-legacy を送信' },
                        { value: 'claude-acp', label: 'Claude ACP', description: 'agent_type=claude-acp を送信' },
                        { value: 'codex-acp', label: 'Codex ACP', description: 'agent_type=codex-acp を送信' },
                        { value: 'pi-ollama', label: 'Pi Ollama', description: 'agent_type=pi-ollama を送信' },
                        { value: 'cursor', label: 'Cursor ACP', description: 'agent_type=cursor を送信' },
                      ] as { value: AgentApiType; label: string; description: string }[]).map(({ value, label, description }) => (
                        <label key={value} className="flex items-start cursor-pointer group">
                          <input
                            type="radio"
                            name="session-agent-type"
                            value={value}
                            checked={selectedAgentType === value}
                            onChange={() => setSelectedAgentType(value)}
                            className="mt-0.5 w-3.5 h-3.5 text-blue-600 border-gray-300 dark:border-gray-600 focus:ring-blue-500"
                            disabled={isCreating}
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
                    )}
                  </div>
                </div>
              )}
            </div>

            {/* エラー表示 */}
            {error && (
              <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-4">
                <p className="text-red-600 dark:text-red-400 text-sm">{error}</p>
              </div>
            )}

            {/* ステータス表示 */}
            {statusMessage && (
              <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-4">
                <p className="text-blue-600 dark:text-blue-400 text-sm">{statusMessage}</p>
              </div>
            )}

            {/* ボタン */}
            <div className="flex justify-end gap-4 pt-4 border-t border-gray-200 dark:border-gray-700">
              <button
                type="button"
                onClick={handleCancel}
                className="px-6 py-3 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-lg transition-colors"
              >
                キャンセル
              </button>
              <button
                type="submit"
                disabled={!initialMessage.trim() || isCreating}
                className="px-6 py-3 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 disabled:bg-gray-400 disabled:cursor-not-allowed rounded-lg transition-colors flex items-center"
              >
                {isCreating ? (
                  <>
                    <svg className="animate-spin -ml-1 mr-2 h-4 w-4 text-white" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                      <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                    </svg>
                    作成中...
                  </>
                ) : (
                  'セッション開始'
                )}
              </button>
            </div>
          </form>
        </div>
      </div>

      {/* Template Selection Modal */}
      {showTemplateModal && (
        <div
          className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
          onClick={(e) => {
            if (e.target === e.currentTarget) {
              setShowTemplateModal(false)
            }
          }}
        >
          <div className="bg-white dark:bg-gray-800 rounded-xl shadow-xl w-full max-w-4xl max-h-[85vh] overflow-hidden">
            <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold text-gray-900 dark:text-white">テンプレートから選択</h2>
                <button
                  onClick={() => setShowTemplateModal(false)}
                  className="text-gray-400 hover:text-gray-500 dark:hover:text-gray-300"
                >
                  <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
            </div>

            <div className="overflow-y-auto max-h-[calc(85vh-5rem)]">
              {/* Recent Messages Section */}
              {recentMessages.length > 0 && (
                <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
                  <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">最近のメッセージ</h3>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                    {recentMessages.map((message, index) => (
                      <button
                        key={index}
                        onClick={() => {
                          setInitialMessage(message)
                          setShowTemplateModal(false)
                        }}
                        className="text-left px-4 py-3 bg-gray-50 dark:bg-gray-700 hover:bg-blue-50 dark:hover:bg-blue-900/30 rounded-lg transition-colors border border-transparent hover:border-blue-300 dark:hover:border-blue-700"
                      >
                        <div className="text-sm text-gray-900 dark:text-white line-clamp-2">
                          {message}
                        </div>
                      </button>
                    ))}
                  </div>
                </div>
              )}

              {/* Templates Section */}
              <div className="px-6 py-4">
                <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">テンプレート</h3>
                {templates.length > 0 ? (
                  <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                    {templates.map((template) => (
                      <button
                        key={template.id}
                        onClick={() => selectTemplate(template)}
                        className="text-left px-4 py-4 bg-gray-50 dark:bg-gray-700 hover:bg-blue-50 dark:hover:bg-blue-900/30 rounded-lg transition-colors border border-gray-200 dark:border-gray-600 hover:border-blue-300 dark:hover:border-blue-700"
                      >
                        <div className="font-medium text-sm text-gray-900 dark:text-white flex items-center gap-2">
                          <svg className="w-4 h-4 text-blue-500 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                          </svg>
                          <span className="truncate">{template.name}</span>
                        </div>
                        <div className="text-sm text-gray-600 dark:text-gray-400 mt-2 line-clamp-3">
                          {template.content}
                        </div>
                      </button>
                    ))}
                  </div>
                ) : (
                  <div className="text-center py-12 text-gray-500 dark:text-gray-400">
                    <svg className="w-12 h-12 mx-auto mb-3 text-gray-300 dark:text-gray-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                    </svg>
                    <p className="text-sm">テンプレートがありません</p>
                    <a
                      href="/settings?tab=templates"
                      target="_blank"
                      rel="noopener noreferrer"
                      className="mt-3 inline-flex items-center gap-1 text-sm text-blue-600 dark:text-blue-400 hover:underline"
                    >
                      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
                      </svg>
                      テンプレートを作成
                    </a>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Session Creation Progress Modal */}
      {showProgressModal && creationProgress && (
        <SessionCreationProgressModal
          isOpen={showProgressModal}
          progress={creationProgress}
          onClose={creationProgress.status === 'failed' ? handleCloseProgressModal : undefined}
          onRetry={creationProgress.status === 'failed' ? handleRetry : undefined}
        />
      )}
    </main>
  )
}
