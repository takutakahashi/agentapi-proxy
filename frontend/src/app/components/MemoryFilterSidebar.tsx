'use client'

import { useState } from 'react'
import { MemoryScope } from '../../types/memory'
import NavigationTabs from './NavigationTabs'

interface MemoryFilterSidebarProps {
  scopeFilter: MemoryScope | null
  onScopeFilterChange: (scope: MemoryScope | null) => void
  tagFilter: Record<string, string>
  onTagFilterChange: (tags: Record<string, string>) => void
  isVisible?: boolean
  onToggleVisibility?: () => void
}

const scopeOptions: { value: MemoryScope | null; label: string; description: string }[] = [
  { value: null, label: 'すべて', description: '個人・チーム両方' },
  { value: 'user', label: 'Personal', description: '個人のメモリ' },
  { value: 'team', label: 'Team', description: 'チームのメモリ' },
]

export default function MemoryFilterSidebar({
  scopeFilter,
  onScopeFilterChange,
  tagFilter,
  onTagFilterChange,
  isVisible = true,
  onToggleVisibility,
}: MemoryFilterSidebarProps) {
  const [tagKeyInput, setTagKeyInput] = useState('')
  const [tagValueInput, setTagValueInput] = useState('')

  const hasFilter = scopeFilter !== null || Object.keys(tagFilter).length > 0

  const handleAddTag = () => {
    const k = tagKeyInput.trim()
    const v = tagValueInput.trim()
    if (!k) return
    onTagFilterChange({ ...tagFilter, [k]: v })
    setTagKeyInput('')
    setTagValueInput('')
  }

  const handleRemoveTag = (key: string) => {
    const next = { ...tagFilter }
    delete next[key]
    onTagFilterChange(next)
  }

  const handleClearAll = () => {
    onScopeFilterChange(null)
    onTagFilterChange({})
  }

  return (
    <>
      {/* Sidebar */}
      <div
        className={`
          fixed inset-y-0 left-0 z-10 w-80 bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700 transform transition-transform duration-300 ease-in-out flex flex-col
          md:relative md:translate-x-0 md:inset-auto md:w-80 md:h-screen
          ${isVisible ? 'translate-x-0' : '-translate-x-full'}
        `}
      >
        {/* Navigation Tabs - outside overflow container so dropdown isn't clipped */}
        <div className="flex-shrink-0 px-4 pt-4 pb-0">
          <NavigationTabs />
        </div>

        <div className="flex-1 overflow-y-auto px-4 pt-4 pb-4">
          {/* Close button for mobile */}
          {onToggleVisibility && (
            <div className="flex justify-end mb-2">
              <button
                onClick={onToggleVisibility}
                className="md:hidden text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
          )}

          <>
              {/* Header */}
              <h2 className="text-lg font-semibold text-gray-900 dark:text-white mb-4">
                フィルタ
              </h2>

              {/* Scope Filter */}
              <div className="mb-6">
                <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  スコープ
                </h3>
                <div className="space-y-2">
                  {scopeOptions.map((option) => (
                    <button
                      key={option.value ?? 'all-scope'}
                      onClick={() => onScopeFilterChange(option.value)}
                      className={`
                        w-full text-left px-3 py-2 rounded-md transition-colors
                        ${
                          scopeFilter === option.value
                            ? 'bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-300 border border-blue-200 dark:border-blue-800'
                            : 'text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700'
                        }
                      `}
                    >
                      <div className="font-medium text-sm">{option.label}</div>
                      <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                        {option.description}
                      </div>
                    </button>
                  ))}
                </div>
              </div>

              {/* Tag Filter */}
              <div className="mb-6">
                <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  タグフィルター
                </h3>

                {/* Active tag chips */}
                {Object.entries(tagFilter).length > 0 && (
                  <div className="flex flex-wrap gap-1 mb-3">
                    {Object.entries(tagFilter).map(([k, v]) => (
                      <span
                        key={k}
                        className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 border border-emerald-200 dark:border-emerald-800 font-mono"
                      >
                        {k}={v}
                        <button
                          onClick={() => handleRemoveTag(k)}
                          className="text-emerald-500 hover:text-emerald-700 dark:hover:text-emerald-200 ml-0.5"
                          aria-label={`${k}タグを削除`}
                        >
                          ×
                        </button>
                      </span>
                    ))}
                  </div>
                )}

                {/* Tag input */}
                <div className="space-y-2">
                  <input
                    type="text"
                    placeholder="キー"
                    value={tagKeyInput}
                    onChange={(e) => setTagKeyInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        handleAddTag()
                      }
                    }}
                    className="w-full px-3 py-1.5 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  />
                  <input
                    type="text"
                    placeholder="値"
                    value={tagValueInput}
                    onChange={(e) => setTagValueInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        handleAddTag()
                      }
                    }}
                    className="w-full px-3 py-1.5 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  />
                  <button
                    onClick={handleAddTag}
                    disabled={!tagKeyInput.trim()}
                    className="w-full px-3 py-1.5 text-sm bg-emerald-600 hover:bg-emerald-700 disabled:opacity-50 text-white rounded-md transition-colors"
                  >
                    タグを追加
                  </button>
                </div>
              </div>

              {/* Clear All */}
              {hasFilter && (
                <button
                  onClick={handleClearAll}
                  className="w-full text-center text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300"
                >
                  フィルタをクリア
                </button>
              )}
          </>
        </div>
      </div>

      {/* Mobile overlay */}
      {isVisible && onToggleVisibility && (
        <div
          className="md:hidden fixed inset-0 bg-black bg-opacity-50 z-[5]"
          onClick={onToggleVisibility}
        />
      )}
    </>
  )
}
