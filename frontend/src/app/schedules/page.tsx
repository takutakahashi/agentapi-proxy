'use client'

import { useState, useCallback, useEffect } from 'react'
import { Schedule, ScheduleStatus } from '../../types/schedule'
import ScheduleFilterSidebar from '../components/ScheduleFilterSidebar'
import ScheduleListView from '../components/ScheduleListView'
import ScheduleFormModal from '../components/ScheduleFormModal'
import TopBar from '../components/TopBar'
import NavigationTabs from '../components/NavigationTabs'

const SCHEDULES_STATUS_FILTER_KEY = 'schedules_status_filter'

export default function SchedulesPage() {
  const [statusFilter, setStatusFilter] = useState<ScheduleStatus | null>(() => {
    if (typeof window !== 'undefined') {
      const saved = localStorage.getItem(SCHEDULES_STATUS_FILTER_KEY)
      if (saved === 'active' || saved === 'paused' || saved === 'completed') return saved
    }
    return null
  })

  useEffect(() => {
    if (typeof window !== 'undefined') {
      if (statusFilter === null) {
        localStorage.removeItem(SCHEDULES_STATUS_FILTER_KEY)
      } else {
        localStorage.setItem(SCHEDULES_STATUS_FILTER_KEY, statusFilter)
      }
    }
  }, [statusFilter])
  const [sidebarVisible, setSidebarVisible] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)
  const [isFormModalOpen, setIsFormModalOpen] = useState(false)
  const [editingSchedule, setEditingSchedule] = useState<Schedule | null>(null)

  const handleSchedulesUpdate = useCallback(() => {
    setRefreshKey((prev) => prev + 1)
  }, [])

  const handleScheduleEdit = useCallback((schedule: Schedule) => {
    setEditingSchedule(schedule)
    setIsFormModalOpen(true)
  }, [])

  const handleFormModalClose = useCallback(() => {
    setIsFormModalOpen(false)
    setEditingSchedule(null)
  }, [])

  const handleFormModalSuccess = useCallback(() => {
    handleSchedulesUpdate()
    handleFormModalClose()
  }, [handleSchedulesUpdate, handleFormModalClose])

  const handleNewSchedule = useCallback(() => {
    setEditingSchedule(null)
    setIsFormModalOpen(true)
  }, [])

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar
        title="Schedules"
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
        <ScheduleFilterSidebar
          statusFilter={statusFilter}
          onStatusFilterChange={setStatusFilter}
          isVisible={sidebarVisible}
          onToggleVisibility={() => setSidebarVisible(!sidebarVisible)}
        />

        {/* Main Content */}
        <div className="flex-1 px-4 md:px-6 lg:px-8 pt-6 md:pt-8 pb-6 md:pb-8">
          {/* New Schedule Button (Desktop) */}
          <div className="mb-6 flex justify-end hidden md:flex">
            <button
              onClick={handleNewSchedule}
              className="inline-flex items-center px-6 py-3 bg-blue-600 hover:bg-blue-700 text-white font-medium rounded-lg transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
            >
              <svg className="w-5 h-5 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
              </svg>
              新しいスケジュール
            </button>
          </div>

          {/* Active Filter Display */}
          {statusFilter && (
            <div className="mb-6 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
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

          {/* Schedule List */}
          <ScheduleListView
            statusFilter={statusFilter}
            onScheduleEdit={handleScheduleEdit}
            onSchedulesUpdate={handleSchedulesUpdate}
            key={refreshKey}
          />
        </div>
      </div>

      {/* Floating New Schedule Button (Mobile) */}
      <button
        onClick={handleNewSchedule}
        className="md:hidden fixed bottom-6 right-6 w-14 h-14 bg-blue-600 hover:bg-blue-700 text-white rounded-full shadow-lg flex items-center justify-center z-50 transition-colors"
        aria-label="新しいスケジュール"
      >
        <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
        </svg>
      </button>

      {/* Schedule Form Modal */}
      <ScheduleFormModal
        isOpen={isFormModalOpen}
        onClose={handleFormModalClose}
        onSuccess={handleFormModalSuccess}
        editingSchedule={editingSchedule}
      />
    </main>
  )
}
