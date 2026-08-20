'use client'

import { useState, useEffect, useCallback } from 'react'
import { useRouter } from 'next/navigation'
import { CircleAlert, CircleDot, GitPullRequest, LoaderCircle, MoreHorizontal } from 'lucide-react'
import { Session, AgentStatus, SessionListParams } from '../../types/agentapi'
import { createAgentAPIProxyClientFromStorage, AgentAPIProxyError, ProxySessionStatusEvent } from '../../lib/agentapi-proxy-client'
import { createACPServerClientFromStorage, ACPServerSession } from '../../lib/acp-server-client'
import { getACPServerEnabled } from '../../types/settings'
import { useSessionsStatusStream } from '../hooks/useSessionsStatusStream'
import { formatDate, formatRelativeTime } from '../../utils/timeUtils'
import { truncateText } from '../../utils/textUtils'
import { useTeamScope } from '../../contexts/TeamScopeContext'

function createAgentStatusFromSession(session: Pick<Session, 'status' | 'updated_at'>): AgentStatus {
  switch (session.status) {
    case 'running':
      return {
        status: 'running',
        last_activity: session.updated_at,
      }
    case 'active':
      return {
        status: 'stable',
        last_activity: session.updated_at,
      }
    default:
      return {
        status: 'error',
        last_activity: session.updated_at,
      }
  }
}

function createAgentStatusMapFromSessions(sessionList: Session[]): { [sessionId: string]: AgentStatus } {
  return Object.fromEntries(
    sessionList.map(session => [
      session.session_id,
      createAgentStatusFromSession(session),
    ])
  )
}

function mapACPSessionToSession(acpSession: ACPServerSession): Session {
  return {
    session_id: acpSession.sessionId,
    user_id: '',
    status: (acpSession._meta?.status as Session['status']) || 'active',
    started_at: acpSession.updatedAt || new Date().toISOString(),
    updated_at: acpSession.updatedAt,
    metadata: {
      description: acpSession.title || acpSession.cwd,
    },
    tags: acpSession._meta?.tags || {},
    environment: {},
    scope: (acpSession._meta?.scope as Session['scope']) || 'user',
    team_id: acpSession._meta?.teamId,
  }
}

function getSessionAnnotations(session: Session) {
  const pick = (key: 'pr_url' | 'issue_url' | 'description' | 'running_task') => {
    const directValue = session.annotations?.[key]
    if (typeof directValue === 'string' && directValue.trim()) return directValue.trim()
    return ''
  }

  return {
    prUrl: pick('pr_url'),
    issueUrl: pick('issue_url'),
    description: pick('description'),
    runningTask: pick('running_task'),
  }
}

function getURLNumber(url: string): string | null {
  const match = url.match(/\/(?:pull|issues)\/(\d+)(?:[/?#]|$)/)
  return match?.[1] ?? null
}

interface TagFilter {
  [key: string]: string[]
}

interface CreatingSession {
  id: string
  message: string
  repository?: string
  status: 'creating' | 'waiting-agent' | 'sending-message' | 'completed' | 'failed'
  startTime: Date
}

interface SessionListViewProps {
  tagFilters: TagFilter
  onSessionsUpdate: () => void
  creatingSessions?: CreatingSession[]
}

export default function SessionListView({ tagFilters, onSessionsUpdate, creatingSessions = [] }: SessionListViewProps) {
  const router = useRouter()
  const { selectedTeam } = useTeamScope()

  // Create global API clients
  const [agentAPI] = useState(() => createAgentAPIProxyClientFromStorage())
  const [agentAPIProxy] = useState(() => createAgentAPIProxyClientFromStorage())
  const [acpClient] = useState(() => createACPServerClientFromStorage())
  const [acpMode] = useState(() => getACPServerEnabled())
  
  const [sessions, setSessions] = useState<Session[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [deletingSession, setDeletingSession] = useState<string | null>(null)
  const [isMobile, setIsMobile] = useState(false)
  const [sessionAgentStatus, setSessionAgentStatus] = useState<{ [sessionId: string]: AgentStatus }>({})
  const [selectedSessions, setSelectedSessions] = useState<Set<string>>(new Set())
  const [isBulkDeleting, setIsBulkDeleting] = useState(false)
  const [isSelectionMode, setIsSelectionMode] = useState(false)
  const [showHiddenSessions, setShowHiddenSessions] = useState(false)
  const [openAnnotationMenuId, setOpenAnnotationMenuId] = useState<string | null>(null)
  const [expandedErrorSessionId, setExpandedErrorSessionId] = useState<string | null>(null)

  const [sortBy, setSortBy] = useState<'started_at' | 'updated_at'>(() => {
    if (typeof window !== 'undefined') {
      return (localStorage.getItem('sessionListSortBy') as 'started_at' | 'updated_at') || 'updated_at'
    }
    return 'updated_at'
  })
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>(() => {
    if (typeof window !== 'undefined') {
      return (localStorage.getItem('sessionListSortOrder') as 'asc' | 'desc') || 'desc'
    }
    return 'desc'
  })

  const fetchSessions = useCallback(async (silent = false) => {
    try {
      if (!silent) setLoading(true)
      setError(null)

      let sessionList: Session[]

      if (acpMode) {
        const response = await acpClient.listSessions()
        sessionList = (response.sessions || []).map(mapACPSessionToSession)
      } else {
        const scopeParams: { scope: 'user' | 'team'; team_id?: string } = selectedTeam
          ? { scope: 'team', team_id: selectedTeam }
          : { scope: 'user' }

        const params: SessionListParams = { limit: 1000, ...scopeParams }
        const response = await agentAPI.search!(params)
        sessionList = response.sessions || []
      }

      setSessions(sessionList)
      setSessionAgentStatus(createAgentStatusMapFromSessions(sessionList))
    } catch (err) {
      if (err instanceof AgentAPIProxyError) {
        setError(`セッション一覧の取得に失敗しました: ${err.message}`)
      } else {
        setError('セッション一覧の取得中に予期せぬエラーが発生しました')
      }

      const mockSessions = getMockSessions()
      setSessions(mockSessions)
      setSessionAgentStatus(createAgentStatusMapFromSessions(mockSessions))
    } finally {
      setLoading(false)
    }
  }, [acpMode, acpClient, agentAPI, selectedTeam])

  // SSE でセッションステータス変化を受信したときの処理
  const handleProxyStatusEvent = useCallback((event: ProxySessionStatusEvent) => {
    setSessions(prev => {
      const idx = prev.findIndex(s => s.session_id === event.session_id)
      if (idx === -1) return prev
      const updated = [...prev]
      updated[idx] = { ...updated[idx], status: event.status as Session['status'] }
      return updated
    })
    setSessionAgentStatus(prev => ({
      ...prev,
      [event.session_id]: createAgentStatusFromSession({
        status: event.status as Session['status'],
        updated_at: event.timestamp,
      }),
    }))

    // セッションが active になったタイミングでフルリフレッシュ（メタデータ取得のため）
    if (event.status === 'active' || event.status === 'error' || event.status === 'timeout') {
      fetchSessions()
    }
  }, [fetchSessions])

  // プロキシ全体のセッションステータス SSE ストリーム
  useSessionsStatusStream({
    client: agentAPIProxy,
    onStatusChange: handleProxyStatusEvent,
  })

  useEffect(() => {
    fetchSessions()
  }, [fetchSessions])


  useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth < 640)
    }

    checkMobile()
    window.addEventListener('resize', checkMobile)

    return () => window.removeEventListener('resize', checkMobile)
  }, [])

  // ソート設定を localStorage に保存
  useEffect(() => {
    if (typeof window !== 'undefined') {
      localStorage.setItem('sessionListSortBy', sortBy)
    }
  }, [sortBy])

  useEffect(() => {
    if (typeof window !== 'undefined') {
      localStorage.setItem('sessionListSortOrder', sortOrder)
    }
  }, [sortOrder])


  const getMockSessions = (): Session[] => []

  // hidden タグが付いたセッションの数を計算
  const hiddenSessionsCount = sessions.filter(session => session.tags?.hidden === 'true').length

  // タグフィルタを適用してセッションをフィルタリング
  const filteredSessionsBeforeSort = sessions.filter(session => {
    // hidden=true タグが付いたセッションは非表示フラグがない限り除外
    if (session.tags?.hidden === 'true' && !showHiddenSessions) return false

    return Object.entries(tagFilters).every(([tagKey, selectedValues]) => {
      if (selectedValues.length === 0) return true

      // First check tags field
      const sessionTagValue = session.tags?.[tagKey]
      if (sessionTagValue && selectedValues.includes(sessionTagValue)) {
        return true
      }

      // Fallback to metadata for backward compatibility
      const sessionMetadataValue = session.metadata?.[tagKey]
      return sessionMetadataValue && selectedValues.includes(String(sessionMetadataValue))
    })
  })

  // ソートを適用
  const filteredSessions = [...filteredSessionsBeforeSort].sort((a, b) => {
    const aValue = a[sortBy] || ''
    const bValue = b[sortBy] || ''

    if (aValue < bValue) return sortOrder === 'asc' ? -1 : 1
    if (aValue > bValue) return sortOrder === 'asc' ? 1 : -1
    return 0
  })

  // ページネーション
  const pageState = { page: 1, limit: 20 }
  const paginatedSessions = filteredSessions.slice(
    (pageState.page - 1) * pageState.limit,
    pageState.page * pageState.limit
  )
  const totalPages = Math.ceil(filteredSessions.length / pageState.limit)

  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const handlePageChange = (_newPage: number) => {
    // ページ変更の実装は省略（既存の実装があれば使用）
  }


  const navigateToChat = (sessionId: string) => {
    // AgentAPIチャット画面への遷移
    router.push(`/sessions/${sessionId}`)
  }


  const deleteSession = async (sessionId: string) => {
    if (!confirm('このセッションを削除してもよろしいですか？')) return

    try {
      setDeletingSession(sessionId)
      setError(null)
      setSuccess(null)

      if (acpMode) {
        await acpClient.closeSession(sessionId)
      } else {
        await agentAPI.delete!(sessionId)
      }

      fetchSessions()
      onSessionsUpdate()

      setSuccess('セッションを削除しました')
      setTimeout(() => setSuccess(null), 3000)
    } catch (err) {
      console.error('Failed to delete session:', err)
      setError('セッションの削除に失敗しました')
    } finally {
      setDeletingSession(null)
    }
  }

  const toggleSessionSelection = (sessionId: string) => {
    setSelectedSessions(prev => {
      const next = new Set(prev)
      if (next.has(sessionId)) {
        next.delete(sessionId)
      } else {
        next.add(sessionId)
      }
      return next
    })
  }

  const toggleSelectAll = () => {
    if (selectedSessions.size === paginatedSessions.length && paginatedSessions.length > 0) {
      setSelectedSessions(new Set())
    } else {
      setSelectedSessions(new Set(paginatedSessions.map(s => s.session_id)))
    }
  }

  const deleteSelectedSessions = async () => {
    const count = selectedSessions.size
    if (count === 0) return
    if (!confirm(`${count}件のセッションを削除してもよろしいですか？`)) return

    try {
      setIsBulkDeleting(true)
      setError(null)
      setSuccess(null)

      if (acpMode) {
        await Promise.all(Array.from(selectedSessions).map(id => acpClient.closeSession(id)))
      } else {
        await agentAPI.deleteBatch!(Array.from(selectedSessions))
      }

      setSelectedSessions(new Set())
      fetchSessions()
      onSessionsUpdate()

      setSuccess(`${count}件のセッションを削除しました`)
      setTimeout(() => setSuccess(null), 3000)
    } catch (err) {
      console.error('Failed to bulk delete sessions:', err)
      setError('セッションの一括削除に失敗しました')
    } finally {
      setIsBulkDeleting(false)
    }
  }


  const getStatusText = (status: CreatingSession['status']) => {
    switch (status) {
      case 'creating': return 'セッション作成中...'
      case 'waiting-agent': return 'エージェント準備中...'
      case 'sending-message': return 'メッセージ送信中...'
      case 'completed': return '完了'
      case 'failed': return '失敗'
      default: return '処理中...'
    }
  }

  // セッションが新規かどうかを判定（開始から2分以内）
  const isNewSession = (startedAt: string) => {
    const started = new Date(startedAt)
    const now = new Date()
    const diffMs = now.getTime() - started.getTime()
    return diffMs < 2 * 60 * 1000 // 2分以内
  }

  // セッションが古いかどうかを判定（started_at と updated_at が両方とも3日前より古い）
  const isOldSession = (session: Session) => {
    const threeDaysAgo = new Date()
    threeDaysAgo.setDate(threeDaysAgo.getDate() - 3)

    const startedAt = new Date(session.started_at)
    const updatedAt = session.updated_at ? new Date(session.updated_at) : startedAt

    return startedAt < threeDaysAgo && updatedAt < threeDaysAgo
  }

  // セッションの表示用説明を取得
  const getSessionDescription = (session: Session) => {
    const annotations = getSessionAnnotations(session)
    if (annotations.description) {
      return annotations.description
    }

    const description = session.metadata?.description

    if (description && description !== 'No description available') {
      return description
    }

    // 新規セッションでメッセージがまだ取得できていない場合
    if (isNewSession(session.started_at)) {
      return '読み込み中...'
    }

    return 'No description available'
  }

  // エージェントステータスの表示情報を取得（新規セッション考慮）
  const getAgentStatusForSession = (session: Session) => {
    const agentStatus = sessionAgentStatus[session.session_id]
    const isNew = isNewSession(session.started_at)

    // セッションが作成中または起動中の場合は、そのステータスを優先表示
    if (session.status === 'creating') {
      return { status: 'creating' as const, colorClass: 'bg-blue-500 animate-pulse', text: '作成中' }
    }
    if (session.status === 'starting') {
      return { status: 'starting' as const, colorClass: 'bg-indigo-500 animate-pulse', text: '起動中' }
    }
    if (session.status === 'running') {
      return { status: 'running' as const, colorClass: 'bg-yellow-500 animate-pulse', text: 'Running' }
    }
    if (session.status === 'suspended') {
      return { status: 'suspended' as const, colorClass: 'bg-violet-500', text: 'Suspended' }
    }
    if (session.status === 'error' || session.status === 'timeout') {
      return { status: 'error' as const, colorClass: 'bg-red-500', text: session.status === 'timeout' ? '起動タイムアウト' : '起動失敗' }
    }

    // ステータスが取得できていない場合
    if (!agentStatus) {
      if (isNew) {
        return { status: 'preparing' as const, colorClass: 'bg-blue-500 animate-pulse', text: '準備中' }
      }
      return { status: 'unknown' as const, colorClass: 'bg-gray-400', text: 'Unknown' }
    }

    // 新規セッションでerrorの場合は「準備中」として扱う
    if (agentStatus.status === 'error' && isNew) {
      return { status: 'preparing' as const, colorClass: 'bg-blue-500 animate-pulse', text: '準備中' }
    }

    switch (agentStatus.status) {
      case 'stable':
        return { status: 'stable' as const, colorClass: 'bg-green-500', text: 'Available' }
      case 'running':
        return { status: 'running' as const, colorClass: 'bg-yellow-500 animate-pulse', text: 'Running' }
      case 'error':
        return { status: 'error' as const, colorClass: 'bg-red-500', text: 'Error' }
      default:
        return { status: 'unknown' as const, colorClass: 'bg-gray-400', text: 'Unknown' }
    }
  }

  const getStatusColor = (status: CreatingSession['status']) => {
    switch (status) {
      case 'creating': return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
      case 'waiting-agent': return 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200'
      case 'sending-message': return 'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200'
      case 'completed': return 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
      case 'failed': return 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
      default: return 'bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200'
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <svg className="animate-spin h-8 w-8 text-blue-600" fill="none" viewBox="0 0 24 24">
          <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
          <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* フローティング一括操作バー */}
      <div className={`fixed bottom-8 left-1/2 -translate-x-1/2 z-50 flex items-center gap-3 px-5 py-3 bg-gray-900 dark:bg-gray-100 text-white dark:text-gray-900 rounded-full shadow-2xl transition-all duration-300 ${selectedSessions.size > 0 ? 'translate-y-0 opacity-100' : 'translate-y-24 opacity-0 pointer-events-none'}`}>
        <span className="text-sm font-semibold tabular-nums whitespace-nowrap">{selectedSessions.size}件選択中</span>
        <div className="w-px h-4 bg-gray-600 dark:bg-gray-400" />
        <button
          onClick={() => setSelectedSessions(new Set())}
          className="text-sm font-medium text-gray-300 dark:text-gray-600 hover:text-white dark:hover:text-gray-900 transition-colors whitespace-nowrap"
        >
          選択解除
        </button>
        <button
          onClick={deleteSelectedSessions}
          disabled={isBulkDeleting}
          className="flex items-center gap-1.5 px-3 py-1.5 bg-red-500 hover:bg-red-600 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-medium rounded-full transition-colors whitespace-nowrap"
        >
          {isBulkDeleting ? (
            <svg className="animate-spin w-3.5 h-3.5" fill="none" viewBox="0 0 24 24">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
            </svg>
          ) : (
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
          )}
          {isBulkDeleting ? '削除中...' : '削除'}
        </button>
      </div>

      {error && (
        <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-4">
          <p className="text-red-600 dark:text-red-400 text-sm">{error}</p>
        </div>
      )}

      {success && (
        <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg p-4">
          <p className="text-green-600 dark:text-green-400 text-sm">{success}</p>
        </div>
      )}

      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 sm:gap-4">
        <div className="flex items-center gap-3">
          <h3 className="hidden sm:block text-lg font-medium text-gray-900 dark:text-white">
            セッション一覧 ({filteredSessions.length + creatingSessions.length})
          </h3>
        </div>
        <div className="flex items-center gap-2">
          {/* 選択モードトグルボタン */}
          <button
            onClick={() => {
              setIsSelectionMode(prev => {
                if (prev) setSelectedSessions(new Set())
                return !prev
              })
            }}
            className={`inline-flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md border transition-colors font-medium ${
              isSelectionMode
                ? 'bg-blue-50 border-blue-400 text-blue-700 dark:bg-blue-900/30 dark:border-blue-500 dark:text-blue-300'
                : 'bg-gray-50 border-gray-200 text-gray-700 dark:bg-gray-800 dark:border-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'
            }`}
            title="複数選択モード"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <rect x="3" y="3" width="18" height="18" rx="2" strokeWidth="2" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4" />
            </svg>
            選択
          </button>
          {/* 非表示セッション表示ボタン（hidden タグ付きセッションが存在する場合のみ表示） */}
          {hiddenSessionsCount > 0 && (
            <button
              onClick={() => setShowHiddenSessions(prev => !prev)}
              className={`inline-flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md border transition-colors font-medium ${
                showHiddenSessions
                  ? 'bg-blue-50 border-blue-400 text-blue-700 dark:bg-blue-900/30 dark:border-blue-500 dark:text-blue-300'
                  : 'bg-gray-50 border-gray-200 text-gray-700 dark:bg-gray-800 dark:border-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'
              }`}
              title={showHiddenSessions ? `非表示セッションを隠す (${hiddenSessionsCount}件)` : `非表示セッションを表示 (${hiddenSessionsCount}件)`}
            >
              {showHiddenSessions ? (
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l3.59 3.59m0 0A9.953 9.953 0 0112 5c4.478 0 8.268 2.943 9.543 7a10.025 10.025 0 01-4.132 5.411m0 0L21 21" />
                </svg>
              ) : (
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                </svg>
              )}
              <span className="hidden sm:inline">{showHiddenSessions ? '非表示を隠す' : '非表示を表示'}</span>
            </button>
          )}
          <div className="w-px h-6 bg-gray-300 dark:bg-gray-600"></div>
          {/* ソート機能 */}
          <select
            value={sortBy}
            onChange={(e) => setSortBy(e.target.value as 'started_at' | 'updated_at')}
            className="text-sm border border-gray-200 dark:border-gray-700 rounded-md px-3 py-1.5 bg-gray-50 dark:bg-gray-800 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            <option value="started_at">開始日時順</option>
            <option value="updated_at">更新日時順</option>
          </select>
          <button
            onClick={() => setSortOrder(sortOrder === 'asc' ? 'desc' : 'asc')}
            className="inline-flex items-center justify-center px-2 py-1.5 text-sm bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors"
            title={sortOrder === 'asc' ? '昇順' : '降順'}
          >
            {sortOrder === 'asc' ? (
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
              </svg>
            ) : (
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
              </svg>
            )}
          </button>
          <div className="w-px h-6 bg-gray-300 dark:bg-gray-600"></div>
          <button
            onClick={() => fetchSessions()}
            disabled={loading}
            className="inline-flex items-center px-3 py-1.5 text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 disabled:opacity-50 font-medium"
          >
            <svg className="w-4 h-4 mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            {loading ? '更新中...' : '更新'}
          </button>
        </div>
      </div>

      {/* 作成中セッション */}
      {creatingSessions.length > 0 && (
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <h4 className="text-md font-medium text-gray-700 dark:text-gray-300">
              作成中のセッション ({creatingSessions.length})
            </h4>
            <div className="flex items-center gap-1">
              <span className="w-2 h-2 bg-blue-500 rounded-full animate-pulse"></span>
              <span className="w-2 h-2 bg-blue-500 rounded-full animate-pulse" style={{ animationDelay: '0.2s' }}></span>
              <span className="w-2 h-2 bg-blue-500 rounded-full animate-pulse" style={{ animationDelay: '0.4s' }}></span>
            </div>
          </div>
          {creatingSessions.map((creatingSession) => (
            <div
              key={creatingSession.id}
              className={`relative overflow-hidden rounded-lg p-6 ${
                creatingSession.status === 'failed'
                  ? 'bg-red-50 dark:bg-red-900/20 border-2 border-red-300 dark:border-red-700'
                  : creatingSession.status === 'completed'
                  ? 'bg-green-50 dark:bg-green-900/20 border-2 border-green-300 dark:border-green-700'
                  : 'bg-gradient-to-r from-blue-50 to-indigo-50 dark:from-blue-900/20 dark:to-indigo-900/20 border-2 border-blue-300 dark:border-blue-600'
              }`}
            >
              {/* 作成中のアニメーションボーダー */}
              {creatingSession.status !== 'completed' && creatingSession.status !== 'failed' && (
                <div className="absolute inset-0 rounded-lg overflow-hidden pointer-events-none">
                  <div className="absolute inset-0 animate-pulse bg-gradient-to-r from-transparent via-blue-200/30 dark:via-blue-500/20 to-transparent"></div>
                </div>
              )}

              <div className="relative flex items-start gap-4">
                {/* 左側のスピナー/アイコン */}
                <div className="flex-shrink-0">
                  {creatingSession.status === 'failed' ? (
                    <div className="w-12 h-12 rounded-full bg-red-100 dark:bg-red-900/50 flex items-center justify-center">
                      <svg className="h-6 w-6 text-red-600 dark:text-red-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                      </svg>
                    </div>
                  ) : creatingSession.status === 'completed' ? (
                    <div className="w-12 h-12 rounded-full bg-green-100 dark:bg-green-900/50 flex items-center justify-center">
                      <svg className="h-6 w-6 text-green-600 dark:text-green-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                      </svg>
                    </div>
                  ) : (
                    <div className="w-12 h-12 rounded-full bg-blue-100 dark:bg-blue-900/50 flex items-center justify-center">
                      <svg className="animate-spin h-6 w-6 text-blue-600 dark:text-blue-400" fill="none" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                      </svg>
                    </div>
                  )}
                </div>

                {/* コンテンツ */}
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-3 mb-2">
                    <span className={`inline-flex items-center px-3 py-1.5 text-sm font-semibold rounded-full ${getStatusColor(creatingSession.status)}`}>
                      {creatingSession.status !== 'completed' && creatingSession.status !== 'failed' && (
                        <svg className="animate-spin -ml-0.5 mr-2 h-3 w-3" fill="none" viewBox="0 0 24 24">
                          <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                          <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                        </svg>
                      )}
                      {getStatusText(creatingSession.status)}
                    </span>
                    <span className="text-sm text-gray-500 dark:text-gray-400">
                      {formatDate(creatingSession.startTime.toISOString())} ({formatRelativeTime(creatingSession.startTime.toISOString())})
                    </span>
                  </div>

                  {/* プログレスステップ表示 */}
                  {creatingSession.status !== 'completed' && creatingSession.status !== 'failed' && (
                    <div className="flex items-center gap-2 mb-3">
                      <div className={`h-1.5 flex-1 rounded-full ${
                        creatingSession.status === 'creating' || creatingSession.status === 'waiting-agent' || creatingSession.status === 'sending-message'
                          ? 'bg-blue-500'
                          : 'bg-gray-200 dark:bg-gray-700'
                      }`}></div>
                      <div className={`h-1.5 flex-1 rounded-full ${
                        creatingSession.status === 'waiting-agent' || creatingSession.status === 'sending-message'
                          ? 'bg-blue-500'
                          : 'bg-gray-200 dark:bg-gray-700'
                      }`}></div>
                      <div className={`h-1.5 flex-1 rounded-full ${
                        creatingSession.status === 'sending-message'
                          ? 'bg-blue-500'
                          : 'bg-gray-200 dark:bg-gray-700'
                      }`}></div>
                    </div>
                  )}

                  <div className="mb-2">
                    <p className="text-gray-900 dark:text-white font-medium">
                      {truncateText(creatingSession.message)}
                    </p>
                  </div>

                  {/* リポジトリタグ */}
                  {creatingSession.repository && (
                    <div className="flex flex-wrap gap-2">
                      <span className="inline-flex items-center px-2 py-1 text-xs bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 rounded-full">
                        <svg className="w-3 h-3 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
                        </svg>
                        {creatingSession.repository}
                      </span>
                    </div>
                  )}

                  {/* 失敗時のエラーメッセージ */}
                  {creatingSession.status === 'failed' && (
                    <div className="mt-3 p-3 bg-red-100 dark:bg-red-900/30 rounded-lg">
                      <p className="text-sm text-red-700 dark:text-red-300 font-medium">
                        セッションの作成に失敗しました。再度お試しください。
                      </p>
                    </div>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {filteredSessions.length === 0 && creatingSessions.length === 0 ? (
        <div className="text-center py-12 bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700">
          <div className="text-gray-400 dark:text-gray-500 mb-2">
            <svg className="w-12 h-12 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
            </svg>
          </div>
          <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-1">
            セッションが見つかりません
          </h3>
          <p className="text-gray-500 dark:text-gray-400">
            {sessions.length === 0 && creatingSessions.length === 0
              ? '新しいセッションを開始してください。'
              : 'フィルタ条件に一致するセッションがありません。'
            }
          </p>
          {hiddenSessionsCount > 0 && !showHiddenSessions && (
            <div className="mt-3">
              <button
                onClick={() => setShowHiddenSessions(true)}
                className="inline-flex items-center gap-1.5 text-sm text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-400"
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                </svg>
                非表示セッションを表示する ({hiddenSessionsCount}件)
              </button>
            </div>
          )}
        </div>
      ) : (
        <div className="space-y-4">
          {/* 既存セッション */}
          {filteredSessions.length > 0 && (
            <div>
              <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg divide-y divide-gray-200 dark:divide-gray-700">
                {/* 全選択ヘッダー行（複数選択モード時のみ表示） */}
                {isSelectionMode && <div className={`px-3 py-2 sm:px-4 flex items-center gap-3 transition-colors duration-150 ${selectedSessions.size > 0 ? 'bg-blue-50 dark:bg-blue-900/20' : 'bg-gray-50 dark:bg-gray-800/50'}`}>
                  <button
                    onClick={toggleSelectAll}
                    className="flex items-center gap-2.5 cursor-pointer select-none group"
                    aria-label="全て選択"
                  >
                    {/* カスタムチェックボックス */}
                    <span className={`flex-shrink-0 flex w-4 h-4 rounded border-2 items-center justify-center transition-colors duration-150 group-hover:border-blue-400 ${
                      selectedSessions.size > 0
                        ? 'bg-blue-600 border-blue-600'
                        : 'bg-white dark:bg-gray-800 border-gray-300 dark:border-gray-600'
                    }`}>
                      {selectedSessions.size > 0 && selectedSessions.size < paginatedSessions.length ? (
                        <span className="block w-2 h-0.5 bg-white rounded-full" />
                      ) : selectedSessions.size > 0 ? (
                        <svg className="w-2.5 h-2.5 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M5 13l4 4L19 7" />
                        </svg>
                      ) : null}
                    </span>
                    <span className={`text-xs font-medium transition-colors duration-150 ${selectedSessions.size > 0 ? 'text-blue-600 dark:text-blue-400' : 'text-gray-500 dark:text-gray-400 group-hover:text-gray-700 dark:group-hover:text-gray-300'}`}>
                      {selectedSessions.size > 0 ? `${selectedSessions.size}件選択中` : '全て選択'}
                    </span>
                  </button>
                </div>}
                {paginatedSessions.map((session) => {
                  const isNew = isNewSession(session.started_at)
                  const agentStatusInfo = getAgentStatusForSession(session)
                  const description = getSessionDescription(session)
                  const isOld = isOldSession(session)
                  const annotations = getSessionAnnotations(session)
                  const prNumber = annotations.prUrl ? getURLNumber(annotations.prUrl) : null
                  const issueNumber = annotations.issueUrl ? getURLNumber(annotations.issueUrl) : null

                  const isSelected = selectedSessions.has(session.session_id)
                  return (
                  <div
                    key={session.session_id}
                    onClick={isSelectionMode ? () => toggleSessionSelection(session.session_id) : undefined}
                    className={`group px-3 py-4 sm:px-4 transition-colors duration-150 ${
                      isSelectionMode ? 'cursor-pointer' : ''
                    } ${
                      isSelected
                        ? 'bg-blue-50 dark:bg-blue-900/20 border-l-2 border-blue-500'
                        : session.status === 'error' || session.status === 'timeout'
                        ? 'bg-red-50 dark:bg-red-900/20 hover:bg-red-100 dark:hover:bg-red-900/30 border-l-4 border-red-500'
                        : session.status === 'creating' || session.status === 'starting'
                        ? 'bg-gradient-to-r from-blue-50 to-indigo-50 dark:from-blue-900/20 dark:to-indigo-900/20 hover:from-blue-100 hover:to-indigo-100 dark:hover:from-blue-900/30 dark:hover:to-indigo-900/30 border-l-4 border-blue-500'
                        : isNew
                        ? 'bg-gradient-to-r from-blue-50 to-indigo-50 dark:from-blue-900/10 dark:to-indigo-900/10 hover:from-blue-100 hover:to-indigo-100 dark:hover:from-blue-900/20 dark:hover:to-indigo-900/20'
                        : 'hover:bg-gray-50 dark:hover:bg-gray-800/50'
                    }`}
                    style={isOld ? { filter: 'grayscale(50%)', opacity: 0.7 } : {}}
                  >
                    <div className="flex gap-3">
                      {/* カスタムチェックボックス（選択モード時は常時表示、非選択モードではホバー時のみ表示） */}
                      <div className="flex-shrink-0 pt-1">
                        <button
                          onClick={(e) => { e.stopPropagation(); toggleSessionSelection(session.session_id) }}
                          aria-label={`セッション ${session.session_id.substring(0, 8)} を選択`}
                          className={`flex w-4 h-4 rounded border-2 items-center justify-center transition-all duration-150 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-1 ${
                            isSelected
                              ? 'bg-blue-600 border-blue-600 opacity-100'
                              : isSelectionMode
                              ? 'bg-white dark:bg-gray-800 border-gray-300 dark:border-gray-600 hover:border-blue-400 opacity-100'
                              : 'bg-white dark:bg-gray-800 border-gray-300 dark:border-gray-600 hover:border-blue-400 opacity-0 group-hover:opacity-100'
                          }`}
                        >
                          {isSelected && (
                            <svg className="w-2.5 h-2.5 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M5 13l4 4L19 7" />
                            </svg>
                          )}
                        </button>
                      </div>
                      <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between space-y-3 sm:space-y-0 flex-1 min-w-0">
                      <div className="flex-1 min-w-0">
                        {/* タイトルとステータス */}
                        <div className="flex items-start gap-3 mb-2">
                          {/* 作成中/起動中/準備中セッションの場合はスピナーを表示 */}
                          {(session.status === 'creating' || session.status === 'starting' || (isNew && agentStatusInfo.status === 'preparing')) && (
                            <div className="flex-shrink-0 mt-0.5">
                              <svg className={`animate-spin h-4 w-4 ${
                                session.status === 'creating' || session.status === 'starting'
                                  ? 'text-blue-500'
                                  : 'text-blue-500'
                              }`} fill="none" viewBox="0 0 24 24">
                                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                              </svg>
                            </div>
                          )}
                          <h3 className={`text-sm sm:text-base font-medium leading-5 sm:leading-6 ${
                            description === '読み込み中...'
                              ? 'text-gray-500 dark:text-gray-400 italic'
                              : 'text-gray-900 dark:text-white'
                          }`}>
                            {truncateText(String(description), isMobile ? 60 : 80)}
                          </h3>
                          <div
                            className={`w-3 h-3 rounded-full flex-shrink-0 ${agentStatusInfo.colorClass}`}
                            title={`Agent: ${agentStatusInfo.text}`}
                          />
                          {/* 新規セッション/作成中/起動中バッジ */}
                          {(isNew || session.status === 'creating' || session.status === 'starting' || session.status === 'error' || session.status === 'timeout') && (
                            <span className={`inline-flex items-center px-2 py-0.5 text-xs font-medium rounded-full ${
                              session.status === 'error' || session.status === 'timeout'
                                ? 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
                                : session.status === 'creating'
                                ? 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
                                : session.status === 'starting'
                                ? 'bg-indigo-100 text-indigo-800 dark:bg-indigo-900 dark:text-indigo-200'
                                : agentStatusInfo.status === 'preparing'
                                ? 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
                                : 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
                            }`}>
                              {session.status === 'error' || session.status === 'timeout'
                                ? session.status === 'timeout' ? '起動タイムアウト' : '起動失敗'
                                : session.status === 'creating'
                                ? '作成中'
                                : session.status === 'starting'
                                ? '起動中'
                                : agentStatusInfo.status === 'preparing'
                                ? '準備中'
                                : '新規'}
                            </span>
                          )}
                          {(session.status === 'error' || session.status === 'timeout') && (
                            <button
                              type="button"
                              onClick={(event) => {
                                event.stopPropagation()
                                setExpandedErrorSessionId(current => current === session.session_id ? null : session.session_id)
                              }}
                              className="inline-flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full text-red-600 transition-colors hover:bg-red-100 hover:text-red-800 focus:outline-none focus:ring-2 focus:ring-red-500 focus:ring-offset-1 dark:text-red-400 dark:hover:bg-red-900/50 dark:hover:text-red-200"
                              aria-label="エラー詳細を表示"
                              title="エラー詳細"
                              aria-expanded={expandedErrorSessionId === session.session_id}
                              aria-controls={`session-error-${session.session_id}`}
                            >
                              <CircleAlert className="h-4 w-4" aria-hidden="true" />
                            </button>
                          )}
                        </div>

                        {annotations.runningTask && (
                          <div className="flex flex-wrap items-center gap-2 mb-2">
                            <span
                              className="inline-flex max-w-full items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400"
                              title={annotations.runningTask}
                            >
                              <LoaderCircle className="h-3 w-3 flex-shrink-0" aria-hidden="true" />
                              <span>作業中:</span>
                              <span className="truncate">{truncateText(annotations.runningTask, isMobile ? 32 : 64)}</span>
                            </span>
                          </div>
                        )}

                        {(session.status === 'error' || session.status === 'timeout') && expandedErrorSessionId === session.session_id && (
                          <div id={`session-error-${session.session_id}`} className="mb-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300" role="alert">
                            {session.error_message || 'セッションの起動に失敗しました。もう一度実行してください。繰り返し失敗する場合は、セッション ID を添えて管理者にお問い合わせください。'}
                          </div>
                        )}

                        {/* セッション情報 */}
                        <div className="flex flex-col sm:flex-row sm:items-center text-xs text-gray-500 dark:text-gray-400 space-y-1 sm:space-y-0 sm:space-x-4 mb-3">
                          <span>#{session.session_id.substring(0, 8)}</span>
                          <div className="flex items-center space-x-2 sm:space-x-4">
                            <span>開始: {formatDate(session.started_at)} ({formatRelativeTime(session.started_at)})</span>
                            {session.updated_at && (
                              <span className="hidden sm:inline">更新: {formatDate(session.updated_at)} ({formatRelativeTime(session.updated_at)})</span>
                            )}
                            {sessionAgentStatus[session.session_id]?.last_activity && (
                              <span className="hidden sm:inline">Agent活動: {formatDate(sessionAgentStatus[session.session_id].last_activity!)} ({formatRelativeTime(sessionAgentStatus[session.session_id].last_activity!)})</span>
                            )}
                            {sessionAgentStatus[session.session_id]?.current_task && (
                              <span className="hidden sm:inline text-blue-600 dark:text-blue-400">タスク: {sessionAgentStatus[session.session_id].current_task!.substring(0, 30)}...</span>
                            )}
                          </div>
                          <span className="hidden sm:inline">by {session.user_id}</span>
                          {session.environment?.WORKSPACE_NAME && (
                            <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-300 self-start">
                              {session.environment.WORKSPACE_NAME}
                            </span>
                          )}
                        </div>

                        {/* タグとメタデータの表示 - モバイルでは最大2個まで表示 */}
                        <div className="flex flex-wrap gap-1 sm:gap-2">
                          {/* Tags field (priority display) */}
                          {session.tags && Object.entries(session.tags)
                            .filter(([key]) => key !== 'description')
                            .slice(0, isMobile ? 2 : 5)
                            .map(([key, value]) => (
                              <span
                                key={`tag-${key}`}
                                className="inline-flex items-center px-2 py-1 text-xs bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-200 rounded-full"
                              >
                                <span className="hidden sm:inline">{key}: </span>
                                <span className="sm:hidden">{key.substring(0, 8)}: </span>
                                {value.substring(0, isMobile ? 8 : 20)}
                              </span>
                            ))}
                          
                          {/* Metadata (fallback/additional info) */}
                          {session.metadata && Object.entries(session.metadata)
                            .filter(([key]) => key !== 'description')
                            .slice(0, Math.max(0, (isMobile ? 2 : 10) - (session.tags ? Object.keys(session.tags).length : 0)))
                            .map(([key, value]) => (
                              <span
                                key={`meta-${key}`}
                                className="inline-flex items-center px-2 py-1 text-xs bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200 rounded-full"
                              >
                                <span className="hidden sm:inline">{key}: </span>
                                <span className="sm:hidden">{key.substring(0, 8)}: </span>
                                {String(value).substring(0, isMobile ? 8 : 20)}
                              </span>
                            ))}
                          
                          {/* Show "more" indicator if there are additional tags/metadata */}
                          {(() => {
                            const totalTags = (session.tags ? Object.keys(session.tags).length : 0) + 
                                            (session.metadata ? Object.entries(session.metadata).filter(([key]) => key !== 'description').length : 0)
                            const displayed = isMobile ? 2 : 10
                            return totalTags > displayed && isMobile && (
                              <span className="inline-flex items-center px-2 py-1 text-xs bg-gray-100 dark:bg-gray-900 text-gray-600 dark:text-gray-400 rounded-full">
                                +{totalTags - displayed}
                              </span>
                            )
                          })()}
                        </div>
                      </div>

                      {/* アクションボタン - モバイルでは縦並び */}
                      <div className="flex flex-row sm:flex-row items-center space-x-2 sm:space-x-2 sm:ml-4" onClick={(e) => e.stopPropagation()}>
                        {session.status !== 'error' && session.status !== 'timeout' && (
                          <button
                            onClick={() => navigateToChat(session.session_id)}
                            className="inline-flex items-center justify-center px-3 py-2 sm:px-3 sm:py-1.5 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-md transition-colors min-h-[44px] sm:min-h-0"
                          >
                            <svg className="w-4 h-4 sm:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
                            </svg>
                            <span className="hidden sm:inline">チャット</span>
                          </button>
                        )}
                        
                        {session.metadata?.claude_login_url ? (
                          <a
                            href={String(session.metadata.claude_login_url).replace(/\s+/g, '')}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center justify-center px-3 py-2 sm:px-3 sm:py-1.5 border border-blue-300 dark:border-blue-600 hover:bg-blue-50 dark:hover:bg-blue-700 text-blue-700 dark:text-blue-300 text-sm font-medium rounded-md transition-colors min-h-[44px] sm:min-h-0"
                          >
                            <svg className="w-4 h-4 sm:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
                            </svg>
                            <span className="hidden sm:inline">ログイン</span>
                          </a>
                        ) : null}

                        {(annotations.prUrl || annotations.issueUrl) && (
                          <div className="relative">
                            <button
                              type="button"
                              onClick={() => setOpenAnnotationMenuId(prev => prev === session.session_id ? null : session.session_id)}
                              className="inline-flex min-h-[44px] items-center justify-center rounded-md border border-gray-300 px-3 py-2 text-gray-600 transition-colors hover:bg-gray-50 hover:text-gray-900 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700 dark:hover:text-white sm:min-h-0 sm:px-3 sm:py-1.5"
                              aria-label="関連リンク"
                              title="関連リンク"
                            >
                              <MoreHorizontal className="h-4 w-4" aria-hidden="true" />
                            </button>
                            {openAnnotationMenuId === session.session_id && (
                              <div className="absolute right-0 top-full z-20 mt-2 min-w-32 overflow-hidden rounded-md border border-gray-200 bg-white py-1 text-xs shadow-lg dark:border-gray-700 dark:bg-gray-900">
                                {annotations.prUrl && (
                                  <a
                                    href={annotations.prUrl.replace(/\s+/g, '')}
                                    target="_blank"
                                    rel="noopener noreferrer"
                                    onClick={() => setOpenAnnotationMenuId(null)}
                                    className="flex items-center gap-2 px-3 py-2 text-gray-700 hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-gray-800"
                                    title={annotations.prUrl}
                                  >
                                    <GitPullRequest className="h-3.5 w-3.5 text-violet-500" aria-hidden="true" />
                                    {prNumber ? `PR #${prNumber}` : 'PR'}
                                  </a>
                                )}
                                {annotations.issueUrl && (
                                  <a
                                    href={annotations.issueUrl.replace(/\s+/g, '')}
                                    target="_blank"
                                    rel="noopener noreferrer"
                                    onClick={() => setOpenAnnotationMenuId(null)}
                                    className="flex items-center gap-2 px-3 py-2 text-gray-700 hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-gray-800"
                                    title={annotations.issueUrl}
                                  >
                                    <CircleDot className="h-3.5 w-3.5 text-emerald-500" aria-hidden="true" />
                                    {issueNumber ? `Issue #${issueNumber}` : 'Issue'}
                                  </a>
                                )}
                              </div>
                            )}
                          </div>
                        )}
                        
                        <button
                          onClick={() => deleteSession(session.session_id)}
                          disabled={deletingSession === session.session_id}
                          aria-label={`${deletingSession === session.session_id ? '削除中' : '削除'}: ${session.metadata?.description || `セッション ${session.session_id.substring(0, 8)}`}`}
                          className="inline-flex items-center justify-center px-3 py-2 sm:px-3 sm:py-1.5 border border-red-300 dark:border-red-600 hover:bg-red-50 dark:hover:bg-red-700 text-red-700 dark:text-red-300 text-sm font-medium rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed min-h-[44px] sm:min-h-0"
                        >
                          {deletingSession === session.session_id ? (
                            <svg className="animate-spin w-4 h-4 sm:mr-1.5 text-red-600 dark:text-red-400" fill="none" viewBox="0 0 24 24">
                              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                            </svg>
                          ) : (
                            <svg className="w-4 h-4 sm:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                            </svg>
                          )}
                          <span className="hidden sm:inline">{deletingSession === session.session_id ? '削除中...' : '削除'}</span>
                        </button>
                      </div>
                    </div>
                    </div>
                  </div>
                  )
                })}

              {/* Pagination */}
              {totalPages > 1 && (
                <div className="flex items-center justify-center gap-2 px-4 py-4 border-t border-gray-200 dark:border-gray-700">
                  <button
                    onClick={() => handlePageChange(pageState.page - 1)}
                    disabled={pageState.page === 1}
                    className="px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md disabled:opacity-50 disabled:cursor-not-allowed hover:bg-gray-50 dark:hover:bg-gray-700"
                  >
                    Previous
                  </button>
                  
                  <span className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300">
                    Page {pageState.page} of {totalPages}
                  </span>
                  
                  <button
                    onClick={() => handlePageChange(pageState.page + 1)}
                    disabled={pageState.page === totalPages}
                    className="px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md disabled:opacity-50 disabled:cursor-not-allowed hover:bg-gray-50 dark:hover:bg-gray-700"
                  >
                    Next
                  </button>
                </div>
              )}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
