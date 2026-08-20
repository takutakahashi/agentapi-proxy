'use client'

import { useState, useCallback } from 'react'
import { SessionProfile } from '../../types/session_profile'
import SessionProfileListView from '../components/SessionProfileListView'
import SessionProfileFormModal from '../components/SessionProfileFormModal'
import SessionProfileSidebar from '../components/SessionProfileSidebar'
import TopBar from '../components/TopBar'
import NavigationTabs from '../components/NavigationTabs'

export default function SessionProfilesPage() {
  const [refreshKey, setRefreshKey] = useState(0)
  const [isFormModalOpen, setIsFormModalOpen] = useState(false)
  const [editingProfile, setEditingProfile] = useState<SessionProfile | null>(null)
  const [sidebarVisible, setSidebarVisible] = useState(false)

  const handleProfilesUpdate = useCallback(() => {
    setRefreshKey((prev) => prev + 1)
  }, [])

  const handleProfileEdit = useCallback((profile: SessionProfile) => {
    setEditingProfile(profile)
    setIsFormModalOpen(true)
  }, [])

  const handleFormModalClose = useCallback(() => {
    setIsFormModalOpen(false)
    setEditingProfile(null)
  }, [])

  const handleFormModalSuccess = useCallback(() => {
    handleProfilesUpdate()
    handleFormModalClose()
  }, [handleProfilesUpdate, handleFormModalClose])

  const handleNewProfile = useCallback(() => {
    setEditingProfile(null)
    setIsFormModalOpen(true)
  }, [])

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar
        title="Profiles"
        showFilterButton={true}
        showSettingsButton={true}
        onFilterToggle={() => setSidebarVisible((prev) => !prev)}
      >
        {/* Mobile Navigation Tabs */}
        <div className="md:hidden">
          <NavigationTabs />
        </div>
      </TopBar>

      <div className="flex">
        {/* Sidebar */}
        <SessionProfileSidebar
          isVisible={sidebarVisible}
          onToggleVisibility={() => setSidebarVisible((prev) => !prev)}
        />

        {/* Main Content */}
        <div className="flex-1 px-4 md:px-6 lg:px-8 pt-6 md:pt-8 pb-6 md:pb-8">
          {/* New Profile Button (Desktop) */}
          <div className="mb-6 flex justify-end hidden md:flex">
            <button
              onClick={handleNewProfile}
              className="inline-flex items-center px-6 py-3 bg-blue-600 hover:bg-blue-700 text-white font-medium rounded-lg transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
            >
              <svg className="w-5 h-5 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
              </svg>
              新しいプロファイル
            </button>
          </div>

          {/* Session Profile List */}
          <SessionProfileListView
            onProfileEdit={handleProfileEdit}
            onProfilesUpdate={handleProfilesUpdate}
            key={refreshKey}
          />
        </div>
      </div>

      {/* Floating New Profile Button (Mobile) */}
      <button
        onClick={handleNewProfile}
        className="md:hidden fixed bottom-6 right-6 w-14 h-14 bg-blue-600 hover:bg-blue-700 text-white rounded-full shadow-lg flex items-center justify-center z-50 transition-colors"
        aria-label="新しいプロファイル"
      >
        <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
        </svg>
      </button>

      {/* Session Profile Form Modal */}
      <SessionProfileFormModal
        isOpen={isFormModalOpen}
        onClose={handleFormModalClose}
        onSuccess={handleFormModalSuccess}
        editingProfile={editingProfile}
      />
    </main>
  )
}
