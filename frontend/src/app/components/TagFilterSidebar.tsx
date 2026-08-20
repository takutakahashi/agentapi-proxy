'use client'

import { useState, useEffect, useCallback } from 'react'
import { agentAPI } from '../../lib/api'
import NavigationTabs from './NavigationTabs'
import { useTeamScope } from '../../contexts/TeamScopeContext'

interface Tag {
  key: string
  values: string[]
}

interface TagFilter {
  [key: string]: string[]
}

interface TagFilterSidebarProps {
  onFiltersChange: (filters: TagFilter) => void
  currentFilters: TagFilter
  isVisible?: boolean
  onToggleVisibility?: () => void
}

export default function TagFilterSidebar({
  onFiltersChange,
  currentFilters,
  isVisible = true,
  onToggleVisibility
}: TagFilterSidebarProps) {
  const { selectedTeam } = useTeamScope()
  const [tags, setTags] = useState<Tag[]>([])
  const [loading, setLoading] = useState(true)
  const [expandedTags, setExpandedTags] = useState<Set<string>>(new Set())

  const fetchTags = useCallback(async () => {
    try {
      setLoading(true)

      // Compute scope parameters directly from selectedTeam
      const scopeParams: { scope: 'user' | 'team'; team_id?: string } = selectedTeam
        ? { scope: 'team', team_id: selectedTeam }
        : { scope: 'user' }

      console.log('[TagFilterSidebar] Fetching tags with scope params:', scopeParams)

      // search APIの代わりに、既存のセッションからメタデータを抽出してTagを作成
      const response = await agentAPI.search!({ limit: 1000, ...scopeParams })
      const sessions = response.sessions || []

      const tagMap = new Map<string, Set<string>>()

      sessions.forEach(session => {
        // Process tags field first (prioritize tags over metadata)
        if (session.tags) {
          Object.entries(session.tags)
            .filter(([key]) => key !== 'description')
            .forEach(([key, value]) => {
              if (value && value !== '') {
                if (!tagMap.has(key)) {
                  tagMap.set(key, new Set())
                }
                tagMap.get(key)!.add(value)
              }
            })
        }

        // Fallback to metadata for backward compatibility
        if (session.metadata) {
          Object.entries(session.metadata).forEach(([key, value]) => {
            if (key !== 'description') { // description は除外
              const valueStr = String(value)
              if (!tagMap.has(key)) {
                tagMap.set(key, new Set())
              }
              tagMap.get(key)!.add(valueStr)
            }
          })
        }
      })

      const extractedTags: Tag[] = Array.from(tagMap.entries()).map(([key, values]) => ({
        key,
        values: Array.from(values).sort()
      }))

      setTags(extractedTags)
    } catch (error) {
      console.error('Failed to fetch tags:', error)
      // モックデータを使用
      setTags([
        {
          key: 'project_type',
          values: ['frontend', 'backend', 'fullstack', 'database']
        },
        {
          key: 'technology',
          values: ['React', 'Node.js', 'PostgreSQL', 'Jest', 'Next.js']
        },
        {
          key: 'priority',
          values: ['high', 'medium', 'low']
        }
      ])
    } finally {
      setLoading(false)
    }
  }, [selectedTeam])

  useEffect(() => {
    fetchTags()
  }, [fetchTags])

  // ESCキーでサイドバーを閉じる
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isVisible && onToggleVisibility) {
        onToggleVisibility()
      }
    }

    if (isVisible) {
      document.addEventListener('keydown', handleKeyDown)
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isVisible, onToggleVisibility])

  const toggleTag = (tagKey: string) => {
    const newExpanded = new Set(expandedTags)
    if (newExpanded.has(tagKey)) {
      newExpanded.delete(tagKey)
    } else {
      newExpanded.add(tagKey)
    }
    setExpandedTags(newExpanded)
  }

  const handleValueChange = (tagKey: string, value: string, checked: boolean) => {
    const newFilters = { ...currentFilters }
    const currentValues = newFilters[tagKey] || []
    
    if (checked) {
      newFilters[tagKey] = [...currentValues, value]
    } else {
      newFilters[tagKey] = currentValues.filter(v => v !== value)
      if (newFilters[tagKey].length === 0) {
        delete newFilters[tagKey]
      }
    }
    
    onFiltersChange(newFilters)
  }

  const isValueSelected = (tagKey: string, value: string): boolean => {
    return currentFilters[tagKey]?.includes(value) || false
  }

  const clearAllFilters = () => {
    onFiltersChange({})
  }

  const hasActiveFilters = Object.keys(currentFilters).length > 0

  return (
    <>
      {/* Sidebar */}
      <div className={`
        fixed inset-y-0 left-0 z-10 w-80 bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700 transform transition-transform duration-300 ease-in-out flex flex-col
        md:relative md:translate-x-0 md:inset-auto md:w-80 md:h-screen
        ${isVisible ? 'translate-x-0' : '-translate-x-full'}
      `}>
        {/* Navigation Tabs - outside overflow container so dropdown isn't clipped */}
        <div className="flex-shrink-0 px-4 pt-4 pb-0">
          <NavigationTabs />
        </div>

        <div className="flex-1 overflow-y-auto px-4 pt-4 pb-4">
            <>
              {/* Filter header with close button and actions */}
              <div className="flex items-center justify-between mb-4">
                <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
                  Tags
                </h2>
                <div className="flex items-center gap-2">
                  {onToggleVisibility && (
                    <button
                      onClick={onToggleVisibility}
                      className="md:hidden text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                    >
                      <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                      </svg>
                    </button>
                  )}
                  {hasActiveFilters && (
                    <button
                      onClick={clearAllFilters}
                      className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300"
                    >
                      Clear All
                    </button>
                  )}
                  <button
                    onClick={fetchTags}
                    disabled={loading}
                    className="text-xs text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-300 disabled:opacity-50"
                  >
                    {loading ? 'Loading...' : 'Refresh'}
                  </button>
                </div>
              </div>

              {loading ? (
                <div className="flex items-center justify-center py-8">
                  <svg className="animate-spin h-8 w-8 text-blue-600" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                  </svg>
                </div>
              ) : (
              <>
              {/* Tag Filters */}
              {tags.map((tag) => (
                <div key={tag.key} className="mb-4">
                  <button
                    onClick={() => toggleTag(tag.key)}
                    className="flex items-center justify-between w-full text-left text-sm font-medium text-gray-700 dark:text-gray-300 mb-2 hover:text-gray-900 dark:hover:text-gray-100 py-2 px-3 rounded-md hover:bg-gray-50 dark:hover:bg-gray-700"
                  >
                    <span className="capitalize">{tag.key.replace(/_/g, ' ')}</span>
                    <div className="flex items-center gap-2">
                      {currentFilters[tag.key] && currentFilters[tag.key].length > 0 && (
                        <span className="bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200 text-xs px-2 py-1 rounded-full">
                          {currentFilters[tag.key].length}
                        </span>
                      )}
                      <svg 
                        className={`w-4 h-4 transition-transform ${expandedTags.has(tag.key) ? 'rotate-180' : ''}`}
                        fill="none" 
                        stroke="currentColor" 
                        viewBox="0 0 24 24"
                      >
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                      </svg>
                    </div>
                  </button>

                  {expandedTags.has(tag.key) && (
                    <div className="space-y-2 pl-4 max-h-48 overflow-y-auto border-l-2 border-gray-100 dark:border-gray-700">
                      {tag.values.map((value) => (
                        <label key={value} className="flex items-center cursor-pointer group py-1">
                          <input
                            type="checkbox"
                            checked={isValueSelected(tag.key, value)}
                            onChange={(e) => handleValueChange(tag.key, value, e.target.checked)}
                            className="mr-3 text-blue-600 focus:ring-blue-500 rounded"
                          />
                          <span className="text-sm text-gray-900 dark:text-gray-100 group-hover:text-blue-600 dark:group-hover:text-blue-400">
                            {value}
                          </span>
                        </label>
                      ))}
                    </div>
                  )}
                </div>
              ))}

              {tags.length === 0 && !loading && (
                <div className="text-sm text-gray-500 dark:text-gray-400 text-center py-8">
                  No tags available.
                  Start some sessions with metadata to see tag options.
                </div>
              )}
              </>
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
