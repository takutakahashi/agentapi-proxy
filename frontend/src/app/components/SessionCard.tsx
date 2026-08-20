'use client'

import { useState, useEffect } from 'react'
import Link from 'next/link'
import { CircleDot, GitPullRequest, LoaderCircle, MoreHorizontal } from 'lucide-react'
import { Session } from '../../types/agentapi'
import StatusBadge from './StatusBadge'
import { formatRelativeTime } from '../../utils/timeUtils'
import { truncateText } from '../../utils/textUtils'

interface SessionCardProps {
  session: Session
  onDelete?: (sessionId: string) => Promise<void>
  isDeleting?: boolean
  isSelected?: boolean
  onToggleSelect?: (sessionId: string) => void
  isSelectionMode?: boolean
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

function getSessionTitle(session: Session, annotationDescription: string): string {
  if (annotationDescription) return annotationDescription
  return String(session.tags?.description || session.metadata?.description || `Session ${session.session_id.substring(0, 8)}`)
}

function getURLNumber(url: string): string | null {
  const match = url.match(/\/(?:pull|issues)\/(\d+)(?:[/?#]|$)/)
  return match?.[1] ?? null
}

export default function SessionCard({ session, onDelete, isDeleting, isSelected, onToggleSelect, isSelectionMode }: SessionCardProps) {
  const [isMobile, setIsMobile] = useState(false)
  const [showAnnotationMenu, setShowAnnotationMenu] = useState(false)

  useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth < 640)
    }

    checkMobile()
    window.addEventListener('resize', checkMobile)

    return () => window.removeEventListener('resize', checkMobile)
  }, [])

  // Check if session is old (both started_at and updated_at are older than 3 days)
  const isOldSession = () => {
    const threeDaysAgo = new Date()
    threeDaysAgo.setDate(threeDaysAgo.getDate() - 3)

    const startedAt = new Date(session.started_at)
    const updatedAt = session.updated_at ? new Date(session.updated_at) : startedAt

    return startedAt < threeDaysAgo && updatedAt < threeDaysAgo
  }

  const isOld = isOldSession()
  const annotations = getSessionAnnotations(session)
  const title = getSessionTitle(session, annotations.description)
  const prNumber = annotations.prUrl ? getURLNumber(annotations.prUrl) : null
  const issueNumber = annotations.issueUrl ? getURLNumber(annotations.issueUrl) : null
  const metadataEntries = session.metadata
    ? Object.entries(session.metadata).filter(([key]) => key !== 'description')
    : []


  return (
    <div
      onClick={isSelectionMode && onToggleSelect ? () => onToggleSelect(session.session_id) : undefined}
      className={`group border-b border-gray-200 dark:border-gray-700 px-3 py-4 sm:px-4 transition-colors duration-150 ${
        isSelectionMode ? 'cursor-pointer' : ''
      } ${
        isSelected
          ? 'bg-blue-50 dark:bg-blue-900/20 border-l-2 border-l-blue-500'
          : 'hover:bg-gray-50 dark:hover:bg-gray-800/50'
      }`}
      style={isOld ? { filter: 'grayscale(50%)', opacity: 0.7 } : {}}
    >
      <div className="flex gap-3">
        {/* カスタムチェックボックス（ホバー時 or 選択時に表示） */}
        {onToggleSelect && (
          <div className="flex-shrink-0 pt-1">
            <button
              onClick={(e) => { e.stopPropagation(); onToggleSelect(session.session_id) }}
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
        )}
      <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between space-y-3 sm:space-y-0 flex-1 min-w-0">
        <div className="flex-1 min-w-0">
          {/* タイトルとステータス */}
          <div className="flex items-start gap-3 mb-2">
            <h3 className="text-sm sm:text-base font-medium text-gray-900 dark:text-white leading-5 sm:leading-6">
              {truncateText(title, isMobile ? 100 : 120)}
            </h3>
            <StatusBadge status={session.status} />
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

          {/* セッション情報 */}
          <div className="flex flex-col sm:flex-row sm:items-center text-xs text-gray-500 dark:text-gray-400 space-y-1 sm:space-y-0 sm:space-x-4 mb-3">
            <span>#{session.session_id.substring(0, 8)}</span>
            <div className="flex items-center space-x-2 sm:space-x-4">
              <span>開始: {formatRelativeTime(session.started_at)}</span>
              {session.updated_at && (
                <span className="hidden sm:inline">更新: {formatRelativeTime(session.updated_at)}</span>
              )}
            </div>
            <span className="hidden sm:inline">by {session.user_id}</span>
            {session.environment?.WORKSPACE_NAME && (
              <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-300 self-start">
                {session.environment.WORKSPACE_NAME}
              </span>
            )}
          </div>

          {/* メタデータタグ - モバイルでは最大2個まで表示 */}
          {metadataEntries.length > 0 && (
            <div className="flex flex-wrap gap-1 sm:gap-2">
              {metadataEntries
                .slice(0, isMobile ? 2 : 10)
                .map(([key, value]) => (
                  <span
                    key={key}
                    className="inline-flex items-center px-2 py-1 text-xs bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200 rounded-full"
                  >
                    <span className="hidden sm:inline">{key}: </span>
                    <span className="sm:hidden">{key.substring(0, 8)}: </span>
                    {String(value).substring(0, isMobile ? 8 : 20)}
                  </span>
                ))}
              {metadataEntries.length > 2 && isMobile && (
                <span className="inline-flex items-center px-2 py-1 text-xs bg-gray-100 dark:bg-gray-900 text-gray-600 dark:text-gray-400 rounded-full">
                  +{metadataEntries.length - 2}
                </span>
              )}
            </div>
          )}
        </div>

        {/* アクションボタン - モバイルでは横並び */}
        <div className="flex flex-row sm:flex-row items-center space-x-2 sm:space-x-2 sm:ml-4" onClick={(e) => e.stopPropagation()}>
          {session.status === 'active' && (
            <Link
              href={`/sessions/${session.session_id}`}
              className="inline-flex items-center justify-center px-3 py-2 sm:px-3 sm:py-1.5 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-md transition-colors min-h-[44px] sm:min-h-0"
              prefetch={true}
            >
              <svg className="w-4 h-4 sm:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
              </svg>
              <span className="hidden sm:inline">チャット</span>
            </Link>
          )}
          
          <Link
            href={`/sessions/${session.session_id}`}
            className="inline-flex items-center justify-center px-3 py-2 sm:px-3 sm:py-1.5 border border-gray-300 dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300 text-sm font-medium rounded-md transition-colors min-h-[44px] sm:min-h-0"
          >
            <svg className="w-4 h-4 sm:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
            </svg>
            <span className="hidden sm:inline">詳細</span>
          </Link>

          {(annotations.prUrl || annotations.issueUrl) && (
            <div className="relative">
              <button
                type="button"
                onClick={() => setShowAnnotationMenu(prev => !prev)}
                className="inline-flex min-h-[44px] items-center justify-center rounded-md border border-gray-300 px-3 py-2 text-gray-600 transition-colors hover:bg-gray-50 hover:text-gray-900 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700 dark:hover:text-white sm:min-h-0 sm:px-3 sm:py-1.5"
                aria-label="関連リンク"
                title="関連リンク"
              >
                <MoreHorizontal className="h-4 w-4" aria-hidden="true" />
              </button>
              {showAnnotationMenu && (
                <div className="absolute right-0 top-full z-20 mt-2 min-w-32 overflow-hidden rounded-md border border-gray-200 bg-white py-1 text-xs shadow-lg dark:border-gray-700 dark:bg-gray-900">
                  {annotations.prUrl && (
                    <a
                      href={annotations.prUrl}
                      target="_blank"
                      rel="noreferrer"
                      onClick={() => setShowAnnotationMenu(false)}
                      className="flex items-center gap-2 px-3 py-2 text-gray-700 hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-gray-800"
                      title={annotations.prUrl}
                    >
                      <GitPullRequest className="h-3.5 w-3.5 text-violet-500" aria-hidden="true" />
                      {prNumber ? `PR #${prNumber}` : 'PR'}
                    </a>
                  )}
                  {annotations.issueUrl && (
                    <a
                      href={annotations.issueUrl}
                      target="_blank"
                      rel="noreferrer"
                      onClick={() => setShowAnnotationMenu(false)}
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
          
          {onDelete && (
            <button
              onClick={() => onDelete(session.session_id)}
              disabled={isDeleting}
              aria-label={`${isDeleting ? '削除中' : '削除'}: ${session.metadata?.description || `セッション ${session.session_id.substring(0, 8)}`}`}
              className="inline-flex items-center justify-center px-3 py-2 sm:px-3 sm:py-1.5 border border-red-300 dark:border-red-600 hover:bg-red-50 dark:hover:bg-red-700 text-red-700 dark:text-red-300 text-sm font-medium rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed min-h-[44px] sm:min-h-0"
            >
              {isDeleting ? (
                <svg className="animate-spin w-4 h-4 sm:mr-1.5 text-red-600 dark:text-red-400" fill="none" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                  <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
              ) : (
                <svg className="w-4 h-4 sm:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                </svg>
              )}
              <span className="hidden sm:inline">{isDeleting ? '削除中...' : '削除'}</span>
            </button>
          )}
        </div>
      </div>
      </div>
    </div>
  )
}
