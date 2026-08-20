'use client'

import { WebhookStatus, WebhookType } from '../../types/webhook'
import NavigationTabs from './NavigationTabs'

interface WebhookFilterSidebarProps {
  statusFilter: WebhookStatus | null
  onStatusFilterChange: (status: WebhookStatus | null) => void
  typeFilter: WebhookType | null
  onTypeFilterChange: (type: WebhookType | null) => void
  isVisible?: boolean
  onToggleVisibility?: () => void
}

const statusOptions: { value: WebhookStatus | null; label: string; description: string }[] = [
  { value: null, label: 'すべて', description: 'すべてのステータス' },
  { value: 'active', label: 'Active', description: '有効なWebhook' },
  { value: 'paused', label: 'Paused', description: '一時停止中のWebhook' },
]

const typeOptions: { value: WebhookType | null; label: string; description: string }[] = [
  { value: null, label: 'すべて', description: 'すべてのタイプ' },
  { value: 'github', label: 'GitHub', description: 'GitHub Webhooks' },
  { value: 'custom', label: 'Custom', description: 'カスタムWebhooks' },
]

export default function WebhookFilterSidebar({
  statusFilter,
  onStatusFilterChange,
  typeFilter,
  onTypeFilterChange,
  isVisible = true,
  onToggleVisibility,
}: WebhookFilterSidebarProps) {
  const hasFilter = statusFilter !== null || typeFilter !== null

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

              {/* Status Filter */}
              <div className="mb-6">
                <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  Status
                </h3>
                <div className="space-y-2">
                  {statusOptions.map((option) => (
                    <button
                      key={option.value ?? 'all-status'}
                      onClick={() => onStatusFilterChange(option.value)}
                      className={`
                        w-full text-left px-3 py-2 rounded-md transition-colors
                        ${
                          statusFilter === option.value
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

              {/* Type Filter */}
              <div className="mb-6">
                <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  Type
                </h3>
                <div className="space-y-2">
                  {typeOptions.map((option) => (
                    <button
                      key={option.value ?? 'all-type'}
                      onClick={() => onTypeFilterChange(option.value)}
                      className={`
                        w-full text-left px-3 py-2 rounded-md transition-colors
                        ${
                          typeFilter === option.value
                            ? 'bg-purple-50 dark:bg-purple-900/20 text-purple-700 dark:text-purple-300 border border-purple-200 dark:border-purple-800'
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

              {/* Clear Filter */}
              {hasFilter && (
                <button
                  onClick={() => {
                    onStatusFilterChange(null)
                    onTypeFilterChange(null)
                  }}
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
