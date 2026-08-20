'use client'

import { useState, useCallback, useEffect } from 'react'
import { Webhook, WebhookStatus, WebhookType } from '../../types/webhook'
import WebhookFilterSidebar from '../components/WebhookFilterSidebar'
import WebhookListView from '../components/WebhookListView'
import WebhookFormModal from '../components/WebhookFormModal'
import TopBar from '../components/TopBar'
import NavigationTabs from '../components/NavigationTabs'

const WEBHOOKS_STATUS_FILTER_KEY = 'webhooks_status_filter'
const WEBHOOKS_TYPE_FILTER_KEY = 'webhooks_type_filter'

export default function WebhooksPage() {
  const [statusFilter, setStatusFilter] = useState<WebhookStatus | null>(() => {
    if (typeof window !== 'undefined') {
      const saved = localStorage.getItem(WEBHOOKS_STATUS_FILTER_KEY)
      if (saved === 'active' || saved === 'paused') return saved
    }
    return null
  })
  const [typeFilter, setTypeFilter] = useState<WebhookType | null>(() => {
    if (typeof window !== 'undefined') {
      const saved = localStorage.getItem(WEBHOOKS_TYPE_FILTER_KEY)
      if (saved === 'github' || saved === 'custom') return saved
    }
    return null
  })

  useEffect(() => {
    if (typeof window !== 'undefined') {
      if (statusFilter === null) {
        localStorage.removeItem(WEBHOOKS_STATUS_FILTER_KEY)
      } else {
        localStorage.setItem(WEBHOOKS_STATUS_FILTER_KEY, statusFilter)
      }
    }
  }, [statusFilter])

  useEffect(() => {
    if (typeof window !== 'undefined') {
      if (typeFilter === null) {
        localStorage.removeItem(WEBHOOKS_TYPE_FILTER_KEY)
      } else {
        localStorage.setItem(WEBHOOKS_TYPE_FILTER_KEY, typeFilter)
      }
    }
  }, [typeFilter])
  const [sidebarVisible, setSidebarVisible] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)
  const [isFormModalOpen, setIsFormModalOpen] = useState(false)
  const [editingWebhook, setEditingWebhook] = useState<Webhook | null>(null)

  const handleWebhooksUpdate = useCallback(() => {
    setRefreshKey((prev) => prev + 1)
  }, [])

  const handleWebhookEdit = useCallback((webhook: Webhook) => {
    setEditingWebhook(webhook)
    setIsFormModalOpen(true)
  }, [])

  const handleFormModalClose = useCallback(() => {
    setIsFormModalOpen(false)
    setEditingWebhook(null)
  }, [])

  const handleFormModalSuccess = useCallback(() => {
    handleWebhooksUpdate()
    handleFormModalClose()
  }, [handleWebhooksUpdate, handleFormModalClose])

  const handleNewWebhook = useCallback(() => {
    setEditingWebhook(null)
    setIsFormModalOpen(true)
  }, [])

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar
        title="Webhooks"
        showFilterButton={true}
        showSettingsButton={true}
        onFilterToggle={() => setSidebarVisible(!sidebarVisible)}
      >
        {/* Mobile Navigation Tabs */}
        <div className="md:hidden">
          <NavigationTabs />
        </div>
      </TopBar>

      <div className="flex">
        {/* Filter Sidebar */}
        <WebhookFilterSidebar
          statusFilter={statusFilter}
          onStatusFilterChange={setStatusFilter}
          typeFilter={typeFilter}
          onTypeFilterChange={setTypeFilter}
          isVisible={sidebarVisible}
          onToggleVisibility={() => setSidebarVisible(!sidebarVisible)}
        />

        {/* Main Content */}
        <div className="flex-1 px-4 md:px-6 lg:px-8 pt-6 md:pt-8 pb-6 md:pb-8">
          {/* New Webhook Button (Desktop) */}
          <div className="mb-6 flex justify-end hidden md:flex">
            <button
              onClick={handleNewWebhook}
              className="inline-flex items-center px-6 py-3 bg-blue-600 hover:bg-blue-700 text-white font-medium rounded-lg transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
            >
              <svg className="w-5 h-5 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
              </svg>
              新しいWebhook
            </button>
          </div>

          {/* Active Filter Display */}
          {(statusFilter || typeFilter) && (
            <div className="mb-6 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="text-sm font-medium text-blue-800 dark:text-blue-200">
                    フィルタ:
                  </span>
                  {statusFilter && (
                    <span className="inline-flex items-center px-2 py-1 text-xs bg-blue-100 dark:bg-blue-800 text-blue-800 dark:text-blue-200 rounded-full">
                      {statusFilter}
                      <button
                        onClick={() => setStatusFilter(null)}
                        className="ml-1 text-blue-600 dark:text-blue-300 hover:text-blue-800 dark:hover:text-blue-100"
                      >
                        ×
                      </button>
                    </span>
                  )}
                  {typeFilter && (
                    <span className="inline-flex items-center px-2 py-1 text-xs bg-purple-100 dark:bg-purple-800 text-purple-800 dark:text-purple-200 rounded-full">
                      {typeFilter}
                      <button
                        onClick={() => setTypeFilter(null)}
                        className="ml-1 text-purple-600 dark:text-purple-300 hover:text-purple-800 dark:hover:text-purple-100"
                      >
                        ×
                      </button>
                    </span>
                  )}
                </div>
                <button
                  onClick={() => {
                    setStatusFilter(null)
                    setTypeFilter(null)
                  }}
                  className="text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300"
                >
                  クリア
                </button>
              </div>
            </div>
          )}

          {/* Webhook List */}
          <WebhookListView
            statusFilter={statusFilter}
            typeFilter={typeFilter}
            onWebhookEdit={handleWebhookEdit}
            onWebhooksUpdate={handleWebhooksUpdate}
            key={refreshKey}
          />
        </div>
      </div>

      {/* Floating New Webhook Button (Mobile) */}
      <button
        onClick={handleNewWebhook}
        className="md:hidden fixed bottom-6 right-6 w-14 h-14 bg-blue-600 hover:bg-blue-700 text-white rounded-full shadow-lg flex items-center justify-center z-50 transition-colors"
        aria-label="新しいWebhook"
      >
        <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
        </svg>
      </button>

      {/* Webhook Form Modal */}
      <WebhookFormModal
        isOpen={isFormModalOpen}
        onClose={handleFormModalClose}
        onSuccess={handleFormModalSuccess}
        editingWebhook={editingWebhook}
      />
    </main>
  )
}
