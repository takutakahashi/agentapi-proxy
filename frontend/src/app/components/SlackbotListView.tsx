'use client'

import { useState, useEffect, useCallback, useRef } from 'react'
import { SlackBot, SlackBotStatus } from '../../types/slackbot'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import SlackbotCard from './SlackbotCard'
import { useTeamScope } from '../../contexts/TeamScopeContext'

interface SlackbotListViewProps {
  statusFilter: SlackBotStatus | null
  onSlackbotEdit: (slackbot: SlackBot) => void
  onSlackbotsUpdate?: () => void
}

export default function SlackbotListView({
  statusFilter,
  onSlackbotEdit,
  onSlackbotsUpdate,
}: SlackbotListViewProps) {
  const { getScopeParams } = useTeamScope()
  const [slackbots, setSlackbots] = useState<SlackBot[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [deletingIds, setDeletingIds] = useState<Set<string>>(new Set())
  const [togglingIds, setTogglingIds] = useState<Set<string>>(new Set())
  const fetchIdRef = useRef(0)

  const fetchSlackbots = useCallback(async () => {
    const fetchId = ++fetchIdRef.current
    try {
      setLoading(true)
      setError(null)

      const scopeParams = getScopeParams()

      const client = createAgentAPIProxyClientFromStorage()
      const response = await client.getSlackBots({
        ...(statusFilter ? { status: statusFilter } : {}),
        ...scopeParams,
      })

      if (fetchId !== fetchIdRef.current) return
      setSlackbots(response.slackbots || [])
    } catch (err) {
      if (fetchId !== fetchIdRef.current) return
      console.error('Failed to fetch slackbots:', err)
      setError(err instanceof Error ? err.message : 'Slackbotの取得に失敗しました')
    } finally {
      if (fetchId === fetchIdRef.current) {
        setLoading(false)
      }
    }
  }, [statusFilter, getScopeParams])

  // Refetch slackbots when team scope changes
  useEffect(() => {
    fetchSlackbots()
  }, [fetchSlackbots])

  const handleDelete = async (slackbotId: string) => {
    setDeletingIds((prev) => new Set(prev).add(slackbotId))
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.deleteSlackBot(slackbotId)
      setSlackbots((prev) => prev.filter((s) => s.id !== slackbotId))
      onSlackbotsUpdate?.()
    } catch (err) {
      console.error('Failed to delete slackbot:', err)
      alert(err instanceof Error ? err.message : 'Slackbotの削除に失敗しました')
    } finally {
      setDeletingIds((prev) => {
        const next = new Set(prev)
        next.delete(slackbotId)
        return next
      })
    }
  }

  const handleToggleStatus = async (slackbotId: string, newStatus: SlackBotStatus) => {
    setTogglingIds((prev) => new Set(prev).add(slackbotId))
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const updated = await client.updateSlackBot(slackbotId, { status: newStatus })
      setSlackbots((prev) =>
        prev.map((s) => (s.id === slackbotId ? updated : s))
      )
      onSlackbotsUpdate?.()
    } catch (err) {
      console.error('Failed to toggle slackbot status:', err)
      alert(err instanceof Error ? err.message : 'Slackbotの状態変更に失敗しました')
    } finally {
      setTogglingIds((prev) => {
        const next = new Set(prev)
        next.delete(slackbotId)
        return next
      })
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="flex items-center gap-3 text-gray-500 dark:text-gray-400">
          <svg className="w-5 h-5 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          <span className="text-sm">Slackbotを読み込み中...</span>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="text-center">
          <div className="w-12 h-12 mx-auto mb-4 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center">
            <svg className="w-6 h-6 text-red-600 dark:text-red-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </div>
          <p className="text-sm text-red-600 dark:text-red-400 mb-3">{error}</p>
          <button
            onClick={fetchSlackbots}
            className="text-sm text-blue-600 dark:text-blue-400 hover:underline"
          >
            再試行
          </button>
        </div>
      </div>
    )
  }

  if (slackbots.length === 0) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="text-center">
          <div className="w-12 h-12 mx-auto mb-4 rounded-full bg-gray-100 dark:bg-gray-700 flex items-center justify-center">
            <svg className="w-6 h-6 text-gray-400 dark:text-gray-500" fill="currentColor" viewBox="0 0 24 24">
              <path d="M6 15a2 2 0 0 1-2 2 2 2 0 0 1-2-2 2 2 0 0 1 2-2h2v2zm1 0a2 2 0 0 1 2-2 2 2 0 0 1 2 2v5a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-5zm2-8a2 2 0 0 1-2-2 2 2 0 0 1 2-2 2 2 0 0 1 2 2v2H9zm0 1a2 2 0 0 1 2 2 2 2 0 0 1-2 2H4a2 2 0 0 1-2-2 2 2 0 0 1 2-2h5zm8 2a2 2 0 0 1 2-2 2 2 0 0 1 2 2 2 2 0 0 1-2 2h-2v-2zm-1 0a2 2 0 0 1-2 2 2 2 0 0 1-2-2V5a2 2 0 0 1 2-2 2 2 0 0 1 2 2v5zm-2 8a2 2 0 0 1 2 2 2 2 0 0 1-2 2 2 2 0 0 1-2-2v-2h2zm0-1a2 2 0 0 1-2-2 2 2 0 0 1 2-2h5a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-5z" />
            </svg>
          </div>
          <p className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
            {statusFilter ? `"${statusFilter}" のSlackbotはありません` : 'Slackbotがまだありません'}
          </p>
          <p className="text-xs text-gray-500 dark:text-gray-400">
            右上の「新しいSlackbot」ボタンから作成してください
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      {slackbots.map((slackbot) => (
        <SlackbotCard
          key={slackbot.id}
          slackbot={slackbot}
          onEdit={onSlackbotEdit}
          onDelete={handleDelete}
          onToggleStatus={handleToggleStatus}
          isDeleting={deletingIds.has(slackbot.id)}
          isTogglingStatus={togglingIds.has(slackbot.id)}
        />
      ))}
    </div>
  )
}
