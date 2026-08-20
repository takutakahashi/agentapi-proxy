'use client'

import { useState, useCallback, useEffect } from 'react'
import { Memory, MemoryScope } from '../../types/memory'
import MemoryFilterSidebar from '../components/MemoryFilterSidebar'
import MemoryListView from '../components/MemoryListView'
import MemoryFormModal from '../components/MemoryFormModal'
import TopBar from '../components/TopBar'
import NavigationTabs from '../components/NavigationTabs'

const MEMORIES_SCOPE_FILTER_KEY = 'memories_scope_filter'

export default function MemoriesPage() {
  const [scopeFilter, setScopeFilter] = useState<MemoryScope | null>(() => {
    if (typeof window !== 'undefined') {
      const saved = localStorage.getItem(MEMORIES_SCOPE_FILTER_KEY)
      if (saved === 'user' || saved === 'team') return saved
    }
    return null
  })

  const [tagFilter, setTagFilter] = useState<Record<string, string>>({})

  useEffect(() => {
    if (typeof window !== 'undefined') {
      if (scopeFilter === null) {
        localStorage.removeItem(MEMORIES_SCOPE_FILTER_KEY)
      } else {
        localStorage.setItem(MEMORIES_SCOPE_FILTER_KEY, scopeFilter)
      }
    }
  }, [scopeFilter])

  const [sidebarVisible, setSidebarVisible] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)
  const [isFormModalOpen, setIsFormModalOpen] = useState(false)
  const [editingMemory, setEditingMemory] = useState<Memory | null>(null)

  const handleMemoriesUpdate = useCallback(() => {
    setRefreshKey((prev) => prev + 1)
  }, [])

  const handleMemoryEdit = useCallback((memory: Memory) => {
    setEditingMemory(memory)
    setIsFormModalOpen(true)
  }, [])

  const handleFormModalClose = useCallback(() => {
    setIsFormModalOpen(false)
    setEditingMemory(null)
  }, [])

  const handleFormModalSuccess = useCallback(() => {
    handleMemoriesUpdate()
    handleFormModalClose()
  }, [handleMemoriesUpdate, handleFormModalClose])

  const handleNewMemory = useCallback(() => {
    setEditingMemory(null)
    setIsFormModalOpen(true)
  }, [])

  const hasActiveFilter = scopeFilter !== null || Object.keys(tagFilter).length > 0

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar
        title="Memories"
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
        <MemoryFilterSidebar
          scopeFilter={scopeFilter}
          onScopeFilterChange={setScopeFilter}
          tagFilter={tagFilter}
          onTagFilterChange={setTagFilter}
          isVisible={sidebarVisible}
          onToggleVisibility={() => setSidebarVisible(!sidebarVisible)}
        />

        {/* Main Content */}
        <div className="flex-1 px-4 md:px-6 lg:px-8 pt-6 md:pt-8 pb-6 md:pb-8">
          {/* New Memory Button (Desktop) */}
          <div className="mb-6 hidden md:flex justify-end">
            <button
              onClick={handleNewMemory}
              className="inline-flex items-center px-6 py-3 bg-blue-600 hover:bg-blue-700 text-white font-medium rounded-lg transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
            >
              <svg className="w-5 h-5 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
              </svg>
              新しいメモリ
            </button>
          </div>

          {/* Active Filter Display */}
          {hasActiveFilter && (
            <div className="mb-6 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="text-sm font-medium text-blue-800 dark:text-blue-200">
                    フィルタ:
                  </span>
                  {scopeFilter && (
                    <span className="inline-flex items-center px-2 py-1 text-xs bg-blue-100 dark:bg-blue-800 text-blue-800 dark:text-blue-200 rounded-full">
                      {scopeFilter === 'user' ? 'Personal' : 'Team'}
                      <button
                        onClick={() => setScopeFilter(null)}
                        className="ml-1 text-blue-600 dark:text-blue-300 hover:text-blue-800 dark:hover:text-blue-100"
                      >
                        ×
                      </button>
                    </span>
                  )}
                  {Object.entries(tagFilter).map(([k, v]) => (
                    <span
                      key={k}
                      className="inline-flex items-center px-2 py-1 text-xs bg-emerald-100 dark:bg-emerald-800 text-emerald-800 dark:text-emerald-200 rounded-full font-mono"
                    >
                      {k}={v}
                      <button
                        onClick={() => {
                          const next = { ...tagFilter }
                          delete next[k]
                          setTagFilter(next)
                        }}
                        className="ml-1 text-emerald-600 dark:text-emerald-300 hover:text-emerald-800 dark:hover:text-emerald-100"
                      >
                        ×
                      </button>
                    </span>
                  ))}
                </div>
                <button
                  onClick={() => {
                    setScopeFilter(null)
                    setTagFilter({})
                  }}
                  className="text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300"
                >
                  クリア
                </button>
              </div>
            </div>
          )}

          {/* Memory List */}
          <MemoryListView
            scopeFilter={scopeFilter}
            tagFilter={tagFilter}
            onMemoryEdit={handleMemoryEdit}
            onMemoriesUpdate={handleMemoriesUpdate}
            key={refreshKey}
          />
        </div>
      </div>

      {/* Floating New Memory Button (Mobile) */}
      <button
        onClick={handleNewMemory}
        className="md:hidden fixed bottom-6 right-6 w-14 h-14 bg-blue-600 hover:bg-blue-700 text-white rounded-full shadow-lg flex items-center justify-center z-50 transition-colors"
        aria-label="新しいメモリ"
      >
        <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
        </svg>
      </button>

      {/* Memory Form Modal */}
      <MemoryFormModal
        isOpen={isFormModalOpen}
        onClose={handleFormModalClose}
        onSuccess={handleFormModalSuccess}
        editingMemory={editingMemory}
      />
    </main>
  )
}
