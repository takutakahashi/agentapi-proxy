'use client'

import { useState, useEffect } from 'react'
import { FilterGroup, SessionFilter } from '../../lib/filter-utils'

interface SessionFilterSidebarProps {
  filterGroups: FilterGroup[]
  currentFilters: SessionFilter
  onFiltersChange: (filters: SessionFilter) => void
  isVisible?: boolean
  onToggleVisibility?: () => void
}

export default function SessionFilterSidebar({
  filterGroups,
  currentFilters,
  onFiltersChange,
  isVisible = true,
  onToggleVisibility
}: SessionFilterSidebarProps) {
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set())

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

  const toggleGroup = (groupKey: string) => {
    const newExpanded = new Set(expandedGroups)
    if (newExpanded.has(groupKey)) {
      newExpanded.delete(groupKey)
    } else {
      newExpanded.add(groupKey)
    }
    setExpandedGroups(newExpanded)
  }

  const handleStatusChange = (status: SessionFilter['status'] | 'all') => {
    onFiltersChange({
      ...currentFilters,
      status: status === 'all' ? undefined : status
    })
  }

  const handleFilterValueChange = (groupKey: string, value: string, checked: boolean) => {
    const newFilters = { ...currentFilters }

    if (groupKey.startsWith('tags.')) {
      const tagKey = groupKey.replace('tags.', '')
      const currentValues = newFilters.tagFilters[tagKey] || []
      
      if (checked) {
        newFilters.tagFilters[tagKey] = [...currentValues, value]
      } else {
        newFilters.tagFilters[tagKey] = currentValues.filter(v => v !== value)
        if (newFilters.tagFilters[tagKey].length === 0) {
          delete newFilters.tagFilters[tagKey]
        }
      }
    } else if (groupKey.startsWith('metadata.')) {
      const metadataKey = groupKey.replace('metadata.', '')
      const currentValues = newFilters.metadataFilters[metadataKey] || []
      
      if (checked) {
        newFilters.metadataFilters[metadataKey] = [...currentValues, value]
      } else {
        newFilters.metadataFilters[metadataKey] = currentValues.filter(v => v !== value)
        if (newFilters.metadataFilters[metadataKey].length === 0) {
          delete newFilters.metadataFilters[metadataKey]
        }
      }
    } else if (groupKey.startsWith('environment.')) {
      const envKey = groupKey.replace('environment.', '')
      const currentValues = newFilters.environmentFilters[envKey] || []
      
      if (checked) {
        newFilters.environmentFilters[envKey] = [...currentValues, value]
      } else {
        newFilters.environmentFilters[envKey] = currentValues.filter(v => v !== value)
        if (newFilters.environmentFilters[envKey].length === 0) {
          delete newFilters.environmentFilters[envKey]
        }
      }
    }

    onFiltersChange(newFilters)
  }

  const isValueSelected = (groupKey: string, value: string): boolean => {
    if (groupKey.startsWith('tags.')) {
      const tagKey = groupKey.replace('tags.', '')
      return currentFilters.tagFilters[tagKey]?.includes(value) || false
    } else if (groupKey.startsWith('metadata.')) {
      const metadataKey = groupKey.replace('metadata.', '')
      return currentFilters.metadataFilters[metadataKey]?.includes(value) || false
    } else if (groupKey.startsWith('environment.')) {
      const envKey = groupKey.replace('environment.', '')
      return currentFilters.environmentFilters[envKey]?.includes(value) || false
    }
    return false
  }

  const clearAllFilters = () => {
    onFiltersChange({
      metadataFilters: {},
      environmentFilters: {},
      tagFilters: {},
      status: undefined
    })
  }

  const hasActiveFilters = 
    currentFilters.status ||
    Object.keys(currentFilters.metadataFilters).length > 0 ||
    Object.keys(currentFilters.environmentFilters).length > 0 ||
    Object.keys(currentFilters.tagFilters).length > 0

  const statusOptions = [
    { value: 'all' as const, label: 'All Statuses' },
    { value: 'creating' as const, label: '作成中' },
    { value: 'starting' as const, label: '起動中' },
    { value: 'error' as const, label: '起動失敗' },
    { value: 'timeout' as const, label: '起動タイムアウト' },
    { value: 'active' as const, label: 'Active' },
    { value: 'unhealthy' as const, label: 'Unhealthy' },
    { value: 'stopped' as const, label: 'Stopped' },
  ]

  return (
    <>
      {/* Sidebar */}
      <div className={`
        fixed inset-y-0 left-0 z-10 w-80 bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700 transform transition-transform duration-300 ease-in-out overflow-y-auto
        md:relative md:translate-x-0 md:inset-auto md:w-80 md:h-screen
        ${isVisible ? 'translate-x-0' : '-translate-x-full'}
      `}>
        <div className="p-4">
          <>
          {/* Header */}
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
              Filters
            </h2>
            <div className="flex items-center gap-2">
              {/* Close button for mobile */}
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
            </div>
          </div>

          {/* Status Filter */}
          <div className="mb-6">
            <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
              Status
            </h3>
            <div className="space-y-2">
              {statusOptions.map((option) => (
                <label key={option.value} className="flex items-center cursor-pointer">
                  <input
                    type="radio"
                    name="status"
                    value={option.value}
                    checked={option.value === 'all' ? !currentFilters.status : currentFilters.status === option.value}
                    onChange={() => handleStatusChange(option.value)}
                    className="mr-2 text-blue-600 focus:ring-blue-500"
                  />
                  <span className="text-sm text-gray-900 dark:text-gray-100">
                    {option.label}
                  </span>
                </label>
              ))}
            </div>
          </div>

          {/* Dynamic Filter Groups */}
          {filterGroups.map((group) => (
            <div key={group.key} className="mb-4">
              <button
                onClick={() => toggleGroup(group.key)}
                className="flex items-center justify-between w-full text-left text-sm font-medium text-gray-700 dark:text-gray-300 mb-2 hover:text-gray-900 dark:hover:text-gray-100"
              >
                <span>{group.label}</span>
                <svg 
                  className={`w-4 h-4 transition-transform ${expandedGroups.has(group.key) ? 'rotate-180' : ''}`}
                  fill="none" 
                  stroke="currentColor" 
                  viewBox="0 0 24 24"
                >
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                </svg>
              </button>

              {expandedGroups.has(group.key) && (
                <div className="space-y-2 pl-2 max-h-48 overflow-y-auto">
                  {group.values.map((filterValue) => (
                    <label key={filterValue.value} className="flex items-center justify-between cursor-pointer group">
                      <div className="flex items-center min-w-0 flex-1">
                        <input
                          type="checkbox"
                          checked={isValueSelected(group.key, filterValue.value)}
                          onChange={(e) => handleFilterValueChange(group.key, filterValue.value, e.target.checked)}
                          className="mr-2 text-blue-600 focus:ring-blue-500 flex-shrink-0"
                        />
                        <span className="text-sm text-gray-900 dark:text-gray-100 truncate" title={filterValue.value}>
                          {filterValue.value}
                        </span>
                      </div>
                      <span className="text-xs text-gray-500 dark:text-gray-400 ml-2 flex-shrink-0">
                        ({filterValue.count})
                      </span>
                    </label>
                  ))}
                </div>
              )}
            </div>
          ))}

          {filterGroups.length === 0 && (
            <div className="text-sm text-gray-500 dark:text-gray-400 text-center py-8">
              No metadata or environment filters available.
              Start some sessions to see filter options.
            </div>
          )}
          </>
        </div>
      </div>

      {/* Mobile overlay */}
      {isVisible && onToggleVisibility && (
        <div
          className="md:hidden fixed inset-0 bg-black bg-opacity-50 z-0"
          onClick={onToggleVisibility}
        />
      )}
    </>
  )
}
