'use client'

import { useState, useEffect, useCallback, useMemo } from 'react'
import { useSearchParams, useRouter } from 'next/navigation'
import { Session, SessionListParams } from '../../types/agentapi'
import { createAgentAPIProxyClientFromStorage, AgentAPIProxyError } from '../../lib/agentapi-proxy-client'
import {
  extractFilterGroups,
  applySessionFilters,
  parseFiltersFromURL,
  filtersToURLParams,
  getFilterValuesForSessionCreation,
  SessionFilter
} from '../../lib/filter-utils'
import LoadingSpinner from './LoadingSpinner'
import NewConversationModal from './NewConversationModal'
import SessionFilterSidebar from './SessionFilterSidebar'
import { SmartSessionList } from './VirtualizedSessionList'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import { useSessionList, mutateSessionList } from '../../hooks/useSessionList'
import { useSessionStatusSSE } from '../../hooks/useSessionStatusSSE'

interface PageState {
  page: number
  limit: number
}

type SortField = 'started_at' | 'updated_at'
type SortOrder = 'asc' | 'desc'

export default function ConversationList() {
  const searchParams = useSearchParams()
  const router = useRouter()
  const { selectedTeam } = useTeamScope()

  // Extract repository from query parameters
  const repositoryParam = searchParams.get('repository')

  // Create global API client
  const [agentAPI] = useState(() => createAgentAPIProxyClientFromStorage(repositoryParam || undefined))

  const [sessionFilters, setSessionFilters] = useState<SessionFilter>(() => parseFiltersFromURL(searchParams))
  const [pageState, setPageState] = useState<PageState>({
    page: 1,
    limit: 50 // Increased limit for better UX with SWR caching
  })
  const [showNewConversationModal, setShowNewConversationModal] = useState(false)
  const [quickStartMessage, setQuickStartMessage] = useState('')
  const [isCreatingQuickSession, setIsCreatingQuickSession] = useState(false)
  const [deletingSession, setDeletingSession] = useState<string | null>(null)
  const [selectedSessions, setSelectedSessions] = useState<Set<string>>(new Set())
  const [isBulkDeleting, setIsBulkDeleting] = useState(false)
  const [isSelectionMode, setIsSelectionMode] = useState(false)
  const [sidebarVisible, setSidebarVisible] = useState(false)
  const [sortBy, setSortBy] = useState<SortField>(() => {
    if (typeof window !== 'undefined') {
      return (localStorage.getItem('conversationListSortBy') as SortField) || 'updated_at'
    }
    return 'updated_at'
  })
  const [sortOrder, setSortOrder] = useState<SortOrder>(() => {
    if (typeof window !== 'undefined') {
      return (localStorage.getItem('conversationListSortOrder') as SortOrder) || 'desc'
    }
    return 'desc'
  })

  // Compute scope parameters for SWR hook
  const scopeParams: SessionListParams = useMemo(() => {
    return selectedTeam
      ? { scope: 'team', team_id: selectedTeam, limit: 1000 }
      : { scope: 'user', limit: 1000 }
  }, [selectedTeam])

  // Use SWR hook for data fetching with caching
  const { sessions: allSessions, isLoading, isError, error: swrError, mutate } = useSessionList(scopeParams, agentAPI, {
    revalidateOnFocus: true,
    revalidateOnReconnect: true,
    refreshInterval: 30000, // Refresh every 30 seconds
  })

  // Local error state for user actions (create, delete, etc.)
  const [localError, setLocalError] = useState<string | null>(null)

  // Convert SWR error to display message
  const error = localError || (isError && swrError
    ? swrError instanceof AgentAPIProxyError
      ? `Failed to load sessions: ${swrError.message}`
      : 'An unexpected error occurred while loading sessions'
    : null)

  // Extract filter groups from all sessions (memoized)
  const filterGroups = useMemo(() => extractFilterGroups(allSessions), [allSessions])

  // Apply filters to get filtered sessions (memoized)
  const filteredSessionsBeforeSort = useMemo(
    () => applySessionFilters(allSessions, sessionFilters),
    [allSessions, sessionFilters]
  )

  // Apply sorting (memoized)
  const filteredSessions = useMemo(
    () => [...filteredSessionsBeforeSort].sort((a, b) => {
      const aValue = a[sortBy] || ''
      const bValue = b[sortBy] || ''

      if (aValue < bValue) return sortOrder === 'asc' ? -1 : 1
      if (aValue > bValue) return sortOrder === 'asc' ? 1 : -1
      return 0
    }),
    [filteredSessionsBeforeSort, sortBy, sortOrder]
  )

  // Apply pagination to filtered sessions (memoized)
  const paginatedSessions = useMemo(
    () => filteredSessions.slice(
      (pageState.page - 1) * pageState.limit,
      pageState.page * pageState.limit
    ),
    [filteredSessions, pageState]
  )

  const totalPages = Math.ceil(filteredSessions.length / pageState.limit)

  // Loading state from SWR
  const loading = isLoading

  // Refresh sessions using SWR mutate
  const fetchSessions = useCallback(() => {
    mutate()
  }, [mutate])

  // Subscribe to real-time session status updates via SSE
  useSessionStatusSSE({
    enabled: true,
    client: agentAPI,
    onStatusChange: (event) => {
      console.log('[ConversationList] Session status update:', event)
    },
    onConnectionChange: (connected) => {
      if (connected) {
        console.log('[ConversationList] SSE connected')
      } else {
        console.log('[ConversationList] SSE disconnected')
      }
    },
  })

  // Update URL when filters change
  const updateURL = useCallback((filters: SessionFilter) => {
    const params = filtersToURLParams(filters)
    const newURL = params.toString() ? `?${params.toString()}` : '/chats'
    router.replace(newURL, { scroll: false })
  }, [router])

  const handleFiltersChange = (newFilters: SessionFilter) => {
    setSessionFilters(newFilters)
    setPageState(prev => ({ ...prev, page: 1 })) // Reset to first page
    updateURL(newFilters)
  }

  const handlePageChange = (newPage: number) => {
    setPageState(prev => ({ ...prev, page: newPage }))
  }

  const handleNewConversationSuccess = () => {
    // Refresh the session list to show the new session
    mutateSessionList()
  }

  const handleQuickStart = async () => {
    if (!quickStartMessage.trim()) return

    try {
      setIsCreatingQuickSession(true)
      setLocalError(null)

      // Get current filter values to use as default parameters for new session
      const { metadata, environment } = getFilterValuesForSessionCreation(sessionFilters)

      // Compute scope parameters directly from selectedTeam
      const scopeParams: { scope: 'user' | 'team'; team_id?: string } = selectedTeam
        ? { scope: 'team', team_id: selectedTeam }
        : { scope: 'user' }

      console.log('[ConversationList] Creating session with scope params:', scopeParams)

      await agentAPI.start({
        metadata: {
          ...metadata,
          description: quickStartMessage.trim()
        },
        environment: {
          ...environment
        },
        params: {
          message: quickStartMessage.trim()
        },
        ...scopeParams
      })

      setQuickStartMessage('')
      mutateSessionList() // Refresh the session list
    } catch (err) {
      if (err instanceof AgentAPIProxyError) {
        setLocalError(`Failed to start session: ${err.message}`)
      } else {
        setLocalError('An unexpected error occurred while starting session')
      }
    } finally {
      setIsCreatingQuickSession(false)
    }
  }

  const handleQuickStartKeyPress = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleQuickStart()
    }
  }

  const deleteSession = async (sessionId: string) => {
    if (!confirm('このセッションを削除してもよろしいですか？')) return

    try {
      setDeletingSession(sessionId)

      await agentAPI.delete(sessionId)

      // セッション一覧を更新
      mutateSessionList()
    } catch (err) {
      console.error('Failed to delete session:', err)
      setLocalError('セッションの削除に失敗しました')
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
      setLocalError(null)

      await agentAPI.deleteBatch(Array.from(selectedSessions))

      setSelectedSessions(new Set())
      mutateSessionList()
    } catch (err) {
      console.error('Failed to bulk delete sessions:', err)
      setLocalError('セッションの一括削除に失敗しました')
    } finally {
      setIsBulkDeleting(false)
    }
  }

  // Initialize filters from URL on mount
  useEffect(() => {
    const urlFilters = parseFiltersFromURL(searchParams)
    setSessionFilters(urlFilters)
  }, [searchParams])

  // ソート設定を localStorage に保存
  useEffect(() => {
    if (typeof window !== 'undefined') {
      localStorage.setItem('conversationListSortBy', sortBy)
    }
  }, [sortBy])

  useEffect(() => {
    if (typeof window !== 'undefined') {
      localStorage.setItem('conversationListSortOrder', sortOrder)
    }
  }, [sortOrder])


  if (loading && allSessions.length === 0) {
    return <LoadingSpinner />
  }

  return (
    <div className="flex">
      {/* Sidebar */}
      <SessionFilterSidebar
        filterGroups={filterGroups}
        currentFilters={sessionFilters}
        onFiltersChange={handleFiltersChange}
        isVisible={sidebarVisible}
        onToggleVisibility={() => setSidebarVisible(!sidebarVisible)}
      />
      
      {/* Main Content */}
      <div className="flex-1 space-y-6 p-4 md:p-6">
      {/* Quick Start Input */}
      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-4">
        <div className="flex gap-3">
          <input
            type="text"
            value={quickStartMessage}
            onChange={(e) => setQuickStartMessage(e.target.value)}
            onKeyPress={handleQuickStartKeyPress}
            placeholder="Type your message to start a new session..."
            className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
            disabled={isCreatingQuickSession}
          />
          <button
            onClick={handleQuickStart}
            disabled={!quickStartMessage.trim() || isCreatingQuickSession}
            className="inline-flex items-center px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-400 disabled:cursor-not-allowed text-white text-sm font-medium rounded-md transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
          >
            {isCreatingQuickSession ? (
              <>
                <svg className="animate-spin -ml-1 mr-2 h-4 w-4 text-white" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                  <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
                Starting...
              </>
            ) : (
              <>
                <svg className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
                </svg>
                Start
              </>
            )}
          </button>
        </div>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-2">
          Press Enter or click Start to begin a new conversation session
        </p>
      </div>

      {/* Header with New Conversation Button */}
      <div className="flex justify-between items-center">
        <div></div>
        <button
          onClick={() => setShowNewConversationModal(true)}
          className="inline-flex items-center px-4 py-2 bg-gray-600 hover:bg-gray-700 text-white text-sm font-medium rounded-lg transition-colors focus:outline-none focus:ring-2 focus:ring-gray-500 focus:ring-offset-2"
        >
          <svg className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 100 4m0-4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 100 4m0-4v2m0-6V4" />
          </svg>
          Advanced Options
        </button>
      </div>

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

        {/* Filter Summary Bar */}
        <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-4">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
            <div className="flex items-center gap-4">
              <button
                onClick={toggleSelectAll}
                className="flex items-center gap-2.5 cursor-pointer select-none group/selectall"
                aria-label="全て選択"
              >
                <span className={`flex-shrink-0 flex w-4 h-4 rounded border-2 items-center justify-center transition-colors duration-150 group-hover/selectall:border-blue-400 ${
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
                <span className={`text-xs font-medium transition-colors duration-150 ${selectedSessions.size > 0 ? 'text-blue-600 dark:text-blue-400' : 'text-gray-500 dark:text-gray-400 group-hover/selectall:text-gray-700 dark:group-hover/selectall:text-gray-300'}`}>
                  全て選択
                </span>
              </button>

              <button
                onClick={() => setSidebarVisible(!sidebarVisible)}
                className="inline-flex items-center px-3 py-2 text-sm bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors"
              >
                <svg className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 4a1 1 0 011-1h16a1 1 0 011 1v2.586a1 1 0 01-.293.707l-6.414 6.414a1 1 0 00-.293.707V17l-4 4v-6.586a1 1 0 00-.293-.707L3.293 7.414A1 1 0 013 6.707V4z" />
                </svg>
                {sidebarVisible ? 'Hide Filters' : 'Show Filters'}
              </button>

              <span className="text-sm text-gray-500 dark:text-gray-400">
                {filteredSessions.length} of {allSessions.length} sessions
                {sessionFilters.status && ` • Status: ${sessionFilters.status}`}
                {Object.keys(sessionFilters.metadataFilters).length > 0 && ` • ${Object.keys(sessionFilters.metadataFilters).length} metadata filters`}
                {Object.keys(sessionFilters.environmentFilters).length > 0 && ` • ${Object.keys(sessionFilters.environmentFilters).length} environment filters`}
              </span>
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
              <div className="w-px h-6 bg-gray-300 dark:bg-gray-600"></div>
              {/* Sort Controls */}
              <select
                value={sortBy}
                onChange={(e) => setSortBy(e.target.value as SortField)}
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
                onClick={fetchSessions}
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
        </div>

      {/* Error State */}
      {error && (
        <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-4">
          <div className="flex items-center">
            <span className="text-red-600 dark:text-red-400 text-sm">{error}</span>
          </div>
        </div>
      )}

        {/* Session List */}
        <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg">
          {paginatedSessions.length === 0 ? (
            <div className="text-center py-12">
              <div className="text-gray-400 dark:text-gray-500 mb-2">
                <svg className="w-12 h-12 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                </svg>
              </div>
              <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-1">
                No sessions found
              </h3>
              <p className="text-gray-500 dark:text-gray-400">
                {allSessions.length === 0
                  ? 'Start a new session to see it here.'
                  : 'No sessions match the current filters. Try adjusting your filters.'
                }
              </p>
            </div>
          ) : (
            <div>
              <SmartSessionList
                sessions={paginatedSessions}
                onDelete={deleteSession}
                deletingSession={deletingSession}
                selectedSessions={selectedSessions}
                onToggleSelect={toggleSessionSelection}
                isSelectionMode={isSelectionMode}
                virtualizationThreshold={50}
              />

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
          )}
        </div>

        {/* New Conversation Modal */}
        <NewConversationModal
          isOpen={showNewConversationModal}
          onClose={() => setShowNewConversationModal(false)}
          onSuccess={handleNewConversationSuccess}
          currentFilters={sessionFilters}
          initialRepository={repositoryParam || undefined}
        />
      </div>
    </div>
  )
}
