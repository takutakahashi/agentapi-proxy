'use client'

import { useState, useEffect, useCallback, useRef } from 'react'
import { Schedule, ScheduleStatus } from '../../types/schedule'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import ScheduleCard from './ScheduleCard'
import { useTeamScope } from '../../contexts/TeamScopeContext'

interface ScheduleListViewProps {
  statusFilter: ScheduleStatus | null
  onScheduleEdit: (schedule: Schedule) => void
  onSchedulesUpdate?: () => void
}

export default function ScheduleListView({
  statusFilter,
  onScheduleEdit,
  onSchedulesUpdate,
}: ScheduleListViewProps) {
  const { getScopeParams } = useTeamScope()
  const [schedules, setSchedules] = useState<Schedule[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const fetchIdRef = useRef(0)

  const fetchSchedules = useCallback(async () => {
    const fetchId = ++fetchIdRef.current
    try {
      setLoading(true)
      setError(null)

      // Get scope parameters for filtering
      const scopeParams = getScopeParams()

      const client = createAgentAPIProxyClientFromStorage()
      const response = await client.getSchedules({
        ...(statusFilter ? { status: statusFilter } : {}),
        ...scopeParams,
      })

      if (fetchId !== fetchIdRef.current) return
      setSchedules(response.schedules || [])
    } catch (err) {
      if (fetchId !== fetchIdRef.current) return
      console.error('Failed to fetch schedules:', err)
      setError(err instanceof Error ? err.message : 'スケジュールの取得に失敗しました')
    } finally {
      if (fetchId === fetchIdRef.current) {
        setLoading(false)
      }
    }
  }, [statusFilter, getScopeParams])

  // Refetch schedules when team scope changes
  useEffect(() => {
    fetchSchedules()
  }, [fetchSchedules])

  const handleDelete = async (scheduleId: string) => {
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.deleteSchedule(scheduleId)
      setSchedules((prev) => prev.filter((s) => s.id !== scheduleId))
      onSchedulesUpdate?.()
    } catch (err) {
      console.error('Failed to delete schedule:', err)
      alert(err instanceof Error ? err.message : 'スケジュールの削除に失敗しました')
    }
  }

  const handleTogglePause = async (scheduleId: string, currentStatus: ScheduleStatus) => {
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const newStatus: ScheduleStatus = currentStatus === 'paused' ? 'active' : 'paused'
      const updated = await client.updateSchedule(scheduleId, { status: newStatus })
      setSchedules((prev) =>
        prev.map((s) => (s.id === scheduleId ? updated : s))
      )
      onSchedulesUpdate?.()
    } catch (err) {
      console.error('Failed to toggle schedule pause:', err)
      alert(err instanceof Error ? err.message : 'スケジュールの状態変更に失敗しました')
    }
  }

  const handleTrigger = async (scheduleId: string) => {
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const result = await client.triggerSchedule(scheduleId)
      alert(`スケジュールを実行しました。セッションID: ${result.session_id}`)
      // Refresh to get updated execution count
      await fetchSchedules()
      onSchedulesUpdate?.()
    } catch (err) {
      console.error('Failed to trigger schedule:', err)
      alert(err instanceof Error ? err.message : 'スケジュールの実行に失敗しました')
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

  if (error) {
    return (
      <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-4">
        <div className="flex items-center gap-2">
          <svg className="w-5 h-5 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span className="text-red-700 dark:text-red-300">{error}</span>
        </div>
        <button
          onClick={fetchSchedules}
          className="mt-3 text-sm text-red-600 dark:text-red-400 hover:underline"
        >
          再試行
        </button>
      </div>
    )
  }

  if (schedules.length === 0) {
    return (
      <div className="text-center py-12">
        <svg className="w-16 h-16 mx-auto text-gray-300 dark:text-gray-600 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">
          スケジュールがありません
        </h3>
        <p className="text-gray-500 dark:text-gray-400">
          {statusFilter
            ? `${statusFilter} のスケジュールはありません。`
            : '新しいスケジュールを作成してください。'}
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h2 className="hidden sm:block text-lg font-semibold text-gray-900 dark:text-white">
          スケジュール一覧 ({schedules.length})
        </h2>
        <button
          onClick={fetchSchedules}
          className="text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white flex items-center gap-1"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          更新
        </button>
      </div>

      {/* Schedule Cards */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-1 xl:grid-cols-2">
        {schedules.map((schedule) => (
          <ScheduleCard
            key={schedule.id}
            schedule={schedule}
            onEdit={onScheduleEdit}
            onDelete={handleDelete}
            onTogglePause={handleTogglePause}
            onTrigger={handleTrigger}
          />
        ))}
      </div>
    </div>
  )
}
