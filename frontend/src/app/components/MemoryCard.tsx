'use client'

import { Memory, MemoryScope } from '../../types/memory'
import CopyableResourceId from './CopyableResourceId'

interface MemoryCardProps {
  memory: Memory
  onEdit: (memory: Memory) => void
  onDelete: (memoryId: string) => void
  isDeleting?: boolean
}

function ScopeBadge({ scope }: { scope: MemoryScope }) {
  if (scope === 'team') {
    return (
      <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 border border-purple-200 dark:border-purple-800">
        Team
      </span>
    )
  }
  return (
    <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 border border-blue-200 dark:border-blue-800">
      Personal
    </span>
  )
}

export default function MemoryCard({
  memory,
  onEdit,
  onDelete,
  isDeleting = false,
}: MemoryCardProps) {
  const handleDelete = () => {
    if (confirm(`"${memory.title}" を削除してもよいですか？`)) {
      onDelete(memory.id)
    }
  }

  const formatDate = (dateStr: string) => {
    try {
      return new Date(dateStr).toLocaleString('ja-JP', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      })
    } catch {
      return dateStr
    }
  }

  const tagEntries = Object.entries(memory.tags || {})

  return (
    <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-5 shadow-sm hover:shadow-md transition-shadow">
      {/* Header */}
      <div className="flex items-start justify-between mb-3">
        <div className="flex items-center gap-2 flex-1 min-w-0">
          {/* Brain icon */}
          <div className="flex-shrink-0 w-8 h-8 rounded-lg bg-emerald-100 dark:bg-emerald-900/30 flex items-center justify-center">
            <svg className="w-4 h-4 text-emerald-600 dark:text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
            </svg>
          </div>
          <h3 className="text-base font-semibold text-gray-900 dark:text-white truncate">
            {memory.title}
          </h3>
        </div>
        <ScopeBadge scope={memory.scope} />
      </div>

      <CopyableResourceId id={memory.id} className="mb-3" />

      {/* Content preview */}
      {memory.content && (
        <div className="mb-3">
          <p className="text-sm text-gray-600 dark:text-gray-400 line-clamp-3 bg-gray-50 dark:bg-gray-700/50 px-3 py-2 rounded whitespace-pre-wrap">
            {memory.content}
          </p>
        </div>
      )}

      {/* Tags */}
      {tagEntries.length > 0 && (
        <div className="mb-3">
          <div className="text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">タグ</div>
          <div className="flex flex-wrap gap-1">
            {tagEntries.slice(0, 5).map(([k, v]) => (
              <span
                key={k}
                className="inline-flex items-center px-2 py-0.5 rounded text-xs bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 border border-gray-200 dark:border-gray-600 font-mono"
              >
                {k}={v}
              </span>
            ))}
            {tagEntries.length > 5 && (
              <span className="inline-flex items-center px-2 py-0.5 rounded text-xs bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-400">
                +{tagEntries.length - 5}
              </span>
            )}
          </div>
        </div>
      )}

      {/* Timestamps */}
      <div className="text-xs text-gray-400 dark:text-gray-500 mt-3 mb-4">
        作成: {formatDate(memory.created_at)}
        {memory.updated_at !== memory.created_at && (
          <span className="ml-3">更新: {formatDate(memory.updated_at)}</span>
        )}
      </div>

      {/* Action Buttons */}
      <div className="flex items-center gap-2 pt-3 border-t border-gray-100 dark:border-gray-700">
        {/* Edit */}
        <button
          onClick={() => onEdit(memory)}
          className="flex-1 inline-flex items-center justify-center px-3 py-1.5 text-sm text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors"
        >
          <svg className="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
          </svg>
          編集
        </button>

        {/* Delete */}
        <button
          onClick={handleDelete}
          disabled={isDeleting}
          className="flex-1 inline-flex items-center justify-center px-3 py-1.5 text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-900/20 hover:bg-red-100 dark:hover:bg-red-900/40 rounded-md transition-colors disabled:opacity-50"
        >
          {isDeleting ? (
            <svg className="w-4 h-4 mr-1 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
          ) : (
            <svg className="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
          )}
          削除
        </button>
      </div>
    </div>
  )
}
