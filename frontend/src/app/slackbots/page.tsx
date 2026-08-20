'use client'

import { useState, useCallback, useEffect } from 'react'
import { SlackBot, SlackBotStatus } from '../../types/slackbot'
import SlackbotFilterSidebar from '../components/SlackbotFilterSidebar'
import SlackbotListView from '../components/SlackbotListView'
import SlackbotFormModal from '../components/SlackbotFormModal'
import TopBar from '../components/TopBar'
import NavigationTabs from '../components/NavigationTabs'

const SLACKBOTS_STATUS_FILTER_KEY = 'slackbots_status_filter'

export default function SlackbotsPage() {
  const [statusFilter, setStatusFilter] = useState<SlackBotStatus | null>(() => {
    if (typeof window !== 'undefined') {
      const saved = localStorage.getItem(SLACKBOTS_STATUS_FILTER_KEY)
      if (saved === 'active' || saved === 'paused') return saved
    }
    return null
  })

  useEffect(() => {
    if (typeof window !== 'undefined') {
      if (statusFilter === null) {
        localStorage.removeItem(SLACKBOTS_STATUS_FILTER_KEY)
      } else {
        localStorage.setItem(SLACKBOTS_STATUS_FILTER_KEY, statusFilter)
      }
    }
  }, [statusFilter])

  const [sidebarVisible, setSidebarVisible] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)
  const [isFormModalOpen, setIsFormModalOpen] = useState(false)
  const [editingSlackbot, setEditingSlackbot] = useState<SlackBot | null>(null)

  const handleSlackbotsUpdate = useCallback(() => {
    setRefreshKey((prev) => prev + 1)
  }, [])

  const handleSlackbotEdit = useCallback((slackbot: SlackBot) => {
    setEditingSlackbot(slackbot)
    setIsFormModalOpen(true)
  }, [])

  const handleFormModalClose = useCallback(() => {
    setIsFormModalOpen(false)
    setEditingSlackbot(null)
  }, [])

  const handleFormModalSuccess = useCallback(() => {
    handleSlackbotsUpdate()
    handleFormModalClose()
  }, [handleSlackbotsUpdate, handleFormModalClose])

  const handleNewSlackbot = useCallback(() => {
    setEditingSlackbot(null)
    setIsFormModalOpen(true)
  }, [])

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar
        title="Slackbots"
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
        <SlackbotFilterSidebar
          statusFilter={statusFilter}
          onStatusFilterChange={setStatusFilter}
          isVisible={sidebarVisible}
          onToggleVisibility={() => setSidebarVisible(!sidebarVisible)}
        />

        {/* Main Content */}
        <div className="flex-1 px-4 md:px-6 lg:px-8 pt-6 md:pt-8 pb-6 md:pb-8">
          {/* New Slackbot Button (Desktop) */}
          <div className="mb-6 flex justify-end hidden md:flex">
            <button
              onClick={handleNewSlackbot}
              className="inline-flex items-center px-6 py-3 bg-blue-600 hover:bg-blue-700 text-white font-medium rounded-lg transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
            >
              <svg className="w-5 h-5 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
              </svg>
              新しいSlackbot
            </button>
          </div>

          {/* Active Filter Display */}
          {statusFilter && (
            <div className="mb-6 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="text-sm font-medium text-blue-800 dark:text-blue-200">
                    フィルタ:
                  </span>
                  <span className="inline-flex items-center px-2 py-1 text-xs bg-blue-100 dark:bg-blue-800 text-blue-800 dark:text-blue-200 rounded-full">
                    {statusFilter}
                    <button
                      onClick={() => setStatusFilter(null)}
                      className="ml-1 text-blue-600 dark:text-blue-300 hover:text-blue-800 dark:hover:text-blue-100"
                    >
                      ×
                    </button>
                  </span>
                </div>
                <button
                  onClick={() => setStatusFilter(null)}
                  className="text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300"
                >
                  クリア
                </button>
              </div>
            </div>
          )}

          {/* Slackbot List */}
          <SlackbotListView
            statusFilter={statusFilter}
            onSlackbotEdit={handleSlackbotEdit}
            onSlackbotsUpdate={handleSlackbotsUpdate}
            key={refreshKey}
          />
        </div>
      </div>

      {/* Floating New Slackbot Button (Mobile) */}
      <button
        onClick={handleNewSlackbot}
        className="md:hidden fixed bottom-6 right-6 w-14 h-14 bg-blue-600 hover:bg-blue-700 text-white rounded-full shadow-lg flex items-center justify-center z-50 transition-colors"
        aria-label="新しいSlackbot"
      >
        <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
        </svg>
      </button>

      {/* Slackbot Form Modal */}
      <SlackbotFormModal
        isOpen={isFormModalOpen}
        onClose={handleFormModalClose}
        onSuccess={handleFormModalSuccess}
        editingSlackbot={editingSlackbot}
      />
    </main>
  )
}
