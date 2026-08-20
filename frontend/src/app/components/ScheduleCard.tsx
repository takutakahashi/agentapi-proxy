'use client'

import { useState } from 'react'
import Link from 'next/link'
import { Schedule } from '../../types/schedule'
import ScheduleStatusBadge from './ScheduleStatusBadge'
import CopyableResourceId from './CopyableResourceId'

interface ScheduleCardProps {
  schedule: Schedule
  onEdit: (schedule: Schedule) => void
  onDelete: (scheduleId: string) => void
  onTogglePause: (scheduleId: string, currentStatus: Schedule['status']) => void
  onTrigger: (scheduleId: string) => void
}

function formatDateTime(isoString?: string): string {
  if (!isoString) return '-'
  try {
    const date = new Date(isoString)
    return date.toLocaleString('ja-JP', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    })
  } catch {
    return isoString
  }
}

function formatCronExpression(cronExpr?: string): string {
  if (!cronExpr) return '-'
  return cronExpr
}

export default function ScheduleCard({
  schedule,
  onEdit,
  onDelete,
  onTogglePause,
  onTrigger,
}: ScheduleCardProps) {
  const [isDeleting, setIsDeleting] = useState(false)
  const [isTriggering, setIsTriggering] = useState(false)

  const handleDelete = async () => {
    if (!confirm(`スケジュール「${schedule.name}」を削除してもよろしいですか？`)) {
      return
    }
    setIsDeleting(true)
    try {
      await onDelete(schedule.id)
    } finally {
      setIsDeleting(false)
    }
  }

  const handleTrigger = async () => {
    setIsTriggering(true)
    try {
      await onTrigger(schedule.id)
    } finally {
      setIsTriggering(false)
    }
  }

  const isPaused = schedule.status === 'paused'
  const isCompleted = schedule.status === 'completed'
  const isOneTime = !schedule.cron_expr && !!schedule.scheduled_at

  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-4 hover:shadow-md transition-shadow">
      {/* Header */}
      <div className="flex items-start justify-between mb-3">
        <div className="flex-1 min-w-0">
          <h3 className="text-lg font-medium text-gray-900 dark:text-white truncate">
            {schedule.name}
          </h3>
          <CopyableResourceId id={schedule.id} className="mt-1" />
          <div className="flex items-center gap-2 mt-1">
            <ScheduleStatusBadge status={schedule.status} />
            {schedule.timezone && (
              <span className="text-xs text-gray-500 dark:text-gray-400">
                {schedule.timezone}
              </span>
            )}
          </div>
        </div>
      </div>

      {/* Schedule Info */}
      <div className="space-y-2 text-sm">
        {/* Cron or Scheduled At */}
        <div className="flex items-center gap-2 text-gray-600 dark:text-gray-400">
          <svg className="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          {schedule.cron_expr ? (
            <span className="font-mono">{formatCronExpression(schedule.cron_expr)}</span>
          ) : (
            <span>{formatDateTime(schedule.scheduled_at)}</span>
          )}
        </div>

        {/* Next Execution */}
        {schedule.next_execution_at && (
          <div className="flex items-center gap-2 text-gray-600 dark:text-gray-400">
            <svg className="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 7l5 5m0 0l-5 5m5-5H6" />
            </svg>
            <span>次回: {formatDateTime(schedule.next_execution_at)}</span>
          </div>
        )}

        {/* Last Execution */}
        {schedule.last_execution_at && (
          <div className="flex items-center gap-2 text-gray-600 dark:text-gray-400">
            <svg className="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span>前回: {formatDateTime(schedule.last_execution_at)}</span>
          </div>
        )}

        {/* Execution Count */}
        {schedule.execution_count !== undefined && schedule.execution_count > 0 && (
          <div className="flex items-center gap-2 text-gray-600 dark:text-gray-400">
            <svg className="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" />
            </svg>
            <span>実行回数: {schedule.execution_count}</span>
          </div>
        )}

        {/* Session Reuse Indicator */}
        {schedule.session_config?.reuse_session && (
          <div className="flex items-center gap-1.5 text-xs text-indigo-600 dark:text-indigo-400">
            <svg className="w-3.5 h-3.5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            <span>セッション再利用</span>
            {schedule.last_execution?.session_reused && (
              <span className="px-1 py-0.5 bg-indigo-100 dark:bg-indigo-900/30 rounded text-indigo-700 dark:text-indigo-300">前回: 再利用</span>
            )}
          </div>
        )}

        {/* Session Config - Message */}
        {schedule.session_config?.params?.message && (
          <div className="mt-2 p-2 bg-gray-50 dark:bg-gray-700/50 rounded text-xs text-gray-600 dark:text-gray-400 line-clamp-2">
            {schedule.session_config.params.message}
          </div>
        )}
      </div>

      {/* Actions */}
      <div className="flex items-center gap-2 mt-4 pt-3 border-t border-gray-100 dark:border-gray-700">
        <Link
          href={`/chats?tag=schedule_id:${schedule.id}`}
          className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20 rounded transition-colors"
          title="このスケジュールが作成したセッション一覧を表示"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
          </svg>
          セッション一覧
        </Link>

        <button
          onClick={() => onEdit(schedule)}
          title={isCompleted && isOneTime ? '新しい実行日時を設定して再アクティブ化できます' : undefined}
          className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20 rounded transition-colors"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
          </svg>
          {isCompleted && isOneTime ? '編集して再実行' : '編集'}
        </button>

        {!isCompleted && (
          <button
            onClick={() => onTogglePause(schedule.id, schedule.status)}
            className={`flex items-center gap-1 px-3 py-1.5 text-sm rounded transition-colors ${
              isPaused
                ? 'text-green-600 dark:text-green-400 hover:bg-green-50 dark:hover:bg-green-900/20'
                : 'text-yellow-600 dark:text-yellow-400 hover:bg-yellow-50 dark:hover:bg-yellow-900/20'
            }`}
          >
            {isPaused ? (
              <>
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                再開
              </>
            ) : (
              <>
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                一時停止
              </>
            )}
          </button>
        )}

        <button
          onClick={handleTrigger}
          disabled={isTriggering || isCompleted}
          className="flex items-center gap-1 px-3 py-1.5 text-sm text-blue-600 dark:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20 rounded transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {isTriggering ? (
            <>
              <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              実行中...
            </>
          ) : (
            <>
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              今すぐ実行
            </>
          )}
        </button>

        <button
          onClick={handleDelete}
          disabled={isDeleting}
          className="flex items-center gap-1 px-3 py-1.5 text-sm text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 rounded transition-colors disabled:opacity-50 ml-auto"
        >
          {isDeleting ? (
            <>
              <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              削除中...
            </>
          ) : (
            <>
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
              削除
            </>
          )}
        </button>
      </div>
    </div>
  )
}
