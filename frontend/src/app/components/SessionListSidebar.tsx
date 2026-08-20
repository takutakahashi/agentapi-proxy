'use client'

import { useState, useEffect, useCallback } from 'react'
import { useRouter } from 'next/navigation'
import { createAgentAPIProxyClientFromStorage, ProxySessionStatusEvent } from '../../lib/agentapi-proxy-client'
import { Session, SessionStatus } from '../../types/agentapi'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { useSessionsStatusStream } from '../hooks/useSessionsStatusStream'

interface SessionListSidebarProps {
  currentSessionId: string
  isVisible?: boolean
}

function getStatusDotClass(status: SessionStatus): string {
  switch (status) {
    case 'active':   return 'bg-green-500'
    case 'suspended':return 'bg-violet-500'
    case 'running':  return 'bg-yellow-400 animate-pulse'
    case 'starting': return 'bg-yellow-400 animate-pulse'
    case 'creating': return 'bg-blue-400 animate-pulse'
    case 'unhealthy':return 'bg-red-500'
    case 'error':    return 'bg-red-500'
    case 'timeout':  return 'bg-red-500'
    default:         return 'bg-gray-300 dark:bg-gray-600'
  }
}

function isRunningStatus(status: SessionStatus): boolean {
  return status === 'active' || status === 'running' || status === 'starting' || status === 'creating'
}

function getSessionAnnotations(session: Session) {
  const pick = (key: 'description' | 'running_task') => {
    const directValue = session.annotations?.[key]
    if (typeof directValue === 'string' && directValue.trim()) return directValue.trim()
    return ''
  }

  return {
    description: pick('description'),
    runningTask: pick('running_task'),
  }
}

function getSessionTitle(session: Session): string {
  const annotations = getSessionAnnotations(session)
  if (annotations.description) return annotations.description
  if (session.tags?.description?.trim()) return session.tags.description.trim()
  const metaDesc = session.metadata?.description
  if (typeof metaDesc === 'string' && metaDesc.trim()) return metaDesc.trim()
  if (session.description?.trim()) return session.description.trim()
  return `#${session.session_id.substring(0, 8)}`
}

function formatRelativeTime(dateStr?: string): string {
  if (!dateStr) return ''
  const diffMin = Math.floor((Date.now() - new Date(dateStr).getTime()) / 60000)
  if (diffMin < 1) return 'たった今'
  if (diffMin < 60) return `${diffMin}分前`
  const diffHour = Math.floor(diffMin / 60)
  if (diffHour < 24) return `${diffHour}h前`
  return `${Math.floor(diffHour / 24)}d前`
}

export default function SessionListSidebar({
  currentSessionId,
  isVisible = true,
}: SessionListSidebarProps) {
  const router = useRouter()
  const { selectedTeam } = useTeamScope()
  const [client] = useState(() => createAgentAPIProxyClientFromStorage())
  const [sessions, setSessions] = useState<Session[]>([])
  const [loading, setLoading] = useState(true)
  const [deletingIds, setDeletingIds] = useState<Set<string>>(new Set())

  const fetchSessions = useCallback(async () => {
    try {
      const scopeParams: { scope: 'user' | 'team'; team_id?: string } = selectedTeam
        ? { scope: 'team', team_id: selectedTeam }
        : { scope: 'user' }
      const response = await client.search({ limit: 100, ...scopeParams })
      const sorted = (response.sessions || []).sort((a, b) =>
        new Date(b.updated_at || b.started_at).getTime() -
        new Date(a.updated_at || a.started_at).getTime()
      )
      setSessions(sorted)
    } catch (err) {
      console.error('[SessionListSidebar] Failed to fetch sessions:', err)
    } finally {
      setLoading(false)
    }
  }, [client, selectedTeam])

  // SSE でリアルタイム更新: ステータス変化をインプレース反映し、active になったらフルリフレッシュ
  const handleProxyStatusEvent = useCallback((event: ProxySessionStatusEvent) => {
    setSessions(prev => {
      const idx = prev.findIndex(s => s.session_id === event.session_id)
      if (idx === -1) return prev
      const updated = [...prev]
      updated[idx] = { ...updated[idx], status: event.status as SessionStatus }
      return updated
    })
    if (event.status === 'active' || event.status === 'stopped' || event.status === 'error' || event.status === 'timeout') {
      fetchSessions()
    }
  }, [fetchSessions])

  useSessionsStatusStream({
    client,
    onStatusChange: handleProxyStatusEvent,
    enabled: isVisible,
  })

  useEffect(() => {
    if (!isVisible) return
    fetchSessions()
    // SSE がリアルタイム更新を担うため、ポーリングは 30 秒に延長
    const interval = setInterval(fetchSessions, 30000)
    return () => clearInterval(interval)
  }, [fetchSessions, isVisible])

  const handleDelete = useCallback(async (e: React.MouseEvent, sessionId: string) => {
    e.stopPropagation()
    if (!window.confirm('このセッションを削除しますか？')) return

    setDeletingIds(prev => new Set(prev).add(sessionId))
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.delete(sessionId)
      setSessions(prev => prev.filter(s => s.session_id !== sessionId))
      if (sessionId === currentSessionId) {
        router.push('/chats')
      }
    } catch (err) {
      console.error('[SessionListSidebar] Failed to delete session:', err)
    } finally {
      setDeletingIds(prev => {
        const next = new Set(prev)
        next.delete(sessionId)
        return next
      })
    }
  }, [currentSessionId, router])

  if (!isVisible) return null

  const runningCount = sessions.filter(s => isRunningStatus(s.status)).length

  return (
    <div className="flex flex-col w-56 h-full bg-gray-50 dark:bg-gray-950 border-r border-gray-200 dark:border-gray-800 flex-shrink-0 overflow-hidden">
      {/* Session list */}
      <div className="flex-1 overflow-y-auto py-1">
        {loading ? (
          <div className="flex items-center justify-center py-8">
            <div className="animate-spin rounded-full h-4 w-4 border-b-2 border-gray-400" />
          </div>
        ) : sessions.length === 0 ? (
          <p className="text-xs text-gray-400 dark:text-gray-600 text-center py-8 px-3">
            セッションなし
          </p>
        ) : (
          <ul>
            {sessions.map((session) => {
              const isActive = session.session_id === currentSessionId
              const running = isRunningStatus(session.status)
              const title = getSessionTitle(session)
              const annotations = getSessionAnnotations(session)
              const timeStr = formatRelativeTime(session.updated_at || session.started_at)
              const isDeleting = deletingIds.has(session.session_id)

              return (
                <li key={session.session_id} className="group/item relative">
                  {/* Navigate button (whole row) */}
                  <button
                    onClick={() => session.status !== 'error' && session.status !== 'timeout' && router.push(`/sessions/${session.session_id}`)}
                    disabled={isDeleting || session.status === 'error' || session.status === 'timeout'}
                    className={`w-full text-left px-3 py-2 pr-9 flex items-start gap-2 transition-colors ${
                      isActive
                        ? 'bg-white dark:bg-gray-900 border-r-2 border-blue-500'
                        : 'hover:bg-white/60 dark:hover:bg-gray-900/60'
                    } ${isDeleting ? 'opacity-40 pointer-events-none' : ''} ${session.status === 'error' || session.status === 'timeout' ? 'cursor-default bg-red-50/60 dark:bg-red-950/20' : ''}`}
                  >
                    {/* Status dot */}
                    <div className="mt-[5px] flex-shrink-0 relative">
                      <div className={`w-1.5 h-1.5 rounded-full ${getStatusDotClass(session.status)}`} />
                      {session.status === 'active' && (
                        <div className="absolute inset-0 rounded-full bg-green-400 opacity-50 animate-ping" />
                      )}
                    </div>

                    {/* Session info */}
                    <div className="flex-1 min-w-0">
                      <p
                        className={`text-xs truncate leading-snug ${
                          isActive
                            ? 'text-gray-900 dark:text-gray-100 font-medium'
                            : 'text-gray-700 dark:text-gray-300'
                        }`}
                        title={title}
                      >
                        {title}
                      </p>
                      {annotations.runningTask && (
                        <p
                          className="text-[10px] text-amber-600 dark:text-amber-400 truncate mt-0.5 leading-none"
                          title={annotations.runningTask}
                        >
                          {annotations.runningTask}
                        </p>
                      )}
                      <p className="text-[10px] text-gray-400 dark:text-gray-600 mt-0.5 leading-none">
                        {session.status === 'error' || session.status === 'timeout' ? (
                          <span className="text-red-600 dark:text-red-400" title={session.error_message}>{session.status === 'timeout' ? '起動タイムアウト' : '起動失敗'}</span>
                        ) : session.status === 'suspended' ? (
                          <span className="text-violet-600 dark:text-violet-400">サスペンド中</span>
                        ) : session.status === 'running' ? (
                          <span className="text-yellow-500 dark:text-yellow-400">実行中</span>
                        ) : running && session.status !== 'active' ? (
                          <span className="text-yellow-500 dark:text-yellow-400">
                            {session.status === 'starting' ? '起動中' : '作成中'}
                          </span>
                        ) : (
                          timeStr
                        )}
                      </p>
                    </div>
                  </button>

                  {/* Delete button — visible on row hover */}
                  <button
                    onClick={(e) => handleDelete(e, session.session_id)}
                    disabled={isDeleting}
                    className="absolute right-1 top-1/2 -translate-y-1/2 p-1.5 rounded-md
                               text-gray-400 dark:text-gray-600
                               opacity-0 group-hover/item:opacity-100
                               hover:text-red-500 dark:hover:text-red-400
                               hover:bg-red-50 dark:hover:bg-red-900/20
                               transition-all duration-100
                               disabled:pointer-events-none"
                    title="セッションを削除"
                  >
                    {isDeleting ? (
                      <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                      </svg>
                    ) : (
                      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                      </svg>
                    )}
                  </button>
                </li>
              )
            })}
          </ul>
        )}

      </div>

      {/* New session button — just above footer */}
      {!loading && (
        <div className="flex-shrink-0 px-2 py-1.5 border-t border-gray-200 dark:border-gray-800">
          <button
            onClick={() => router.push('/sessions/new')}
            className="w-full flex items-center justify-center gap-1.5 px-2 py-1.5 rounded-md
                       text-xs text-gray-400 dark:text-gray-600
                       border border-dashed border-gray-300 dark:border-gray-700
                       hover:text-blue-600 dark:hover:text-blue-400
                       hover:border-blue-400 dark:hover:border-blue-600
                       hover:bg-blue-50 dark:hover:bg-blue-900/20
                       transition-colors"
          >
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
            新規セッション
          </button>
        </div>
      )}

      {/* Footer */}
      {!loading && sessions.length > 0 && (
        <div className="flex-shrink-0 px-3 py-1.5 border-t border-gray-200 dark:border-gray-800">
          <p className="text-[10px] text-gray-400 dark:text-gray-600 text-center">
            {runningCount > 0 ? (
              <><span className="text-green-500">{runningCount}</span> 件稼働中 / {sessions.length} 件</>
            ) : (
              `${sessions.length} 件`
            )}
          </p>
        </div>
      )}
    </div>
  )
}
