'use client'

import { useState, useCallback, useEffect, useRef, Suspense } from 'react'

const CHATS_TAG_FILTERS_KEY = 'chats_tag_filters'
import Link from 'next/link'
import { useSearchParams } from 'next/navigation'
import TagFilterSidebar from '../components/TagFilterSidebar'
import SessionListView from '../components/SessionListView'
import TopBar from '../components/TopBar'
import FloatingNewSessionButton from '../components/FloatingNewSessionButton'
import NavigationTabs from '../components/NavigationTabs'

interface TagFilter {
  [key: string]: string[]
}

function parseTagFiltersFromSearchParams(searchParams: URLSearchParams): TagFilter {
  const filters: TagFilter = {}
  searchParams.getAll('tag').forEach(tag => {
    const colonIdx = tag.indexOf(':')
    if (colonIdx > 0) {
      const key = tag.slice(0, colonIdx)
      const value = tag.slice(colonIdx + 1)
      if (key && value) {
        filters[key] = [...(filters[key] || []), value]
      }
    }
  })
  return filters
}

function ChatsPageInner() {
  const searchParams = useSearchParams()

  const [tagFilters, setTagFilters] = useState<TagFilter>(() => {
    const urlFilters = parseTagFiltersFromSearchParams(searchParams)
    if (Object.keys(urlFilters).length > 0) return urlFilters
    if (typeof window !== 'undefined') {
      try {
        const saved = localStorage.getItem(CHATS_TAG_FILTERS_KEY)
        if (saved) return JSON.parse(saved) as TagFilter
      } catch {
        // ignore parse error
      }
    }
    return {}
  })

  const isFirstRender = useRef(true)
  useEffect(() => {
    if (isFirstRender.current) {
      isFirstRender.current = false
      return
    }
    if (typeof window !== 'undefined') {
      if (Object.keys(tagFilters).length === 0) {
        localStorage.removeItem(CHATS_TAG_FILTERS_KEY)
      } else {
        localStorage.setItem(CHATS_TAG_FILTERS_KEY, JSON.stringify(tagFilters))
      }
    }
  }, [tagFilters])
  const [refreshKey, setRefreshKey] = useState(0)
  const [sidebarVisible, setSidebarVisible] = useState(false)

  const handleSessionsUpdate = useCallback(() => {
    setRefreshKey(prev => prev + 1)
  }, [])

  // Load user info and repositories on mount
  useEffect(() => {
    const loadUserInfo = async () => {
      try {
        const response = await fetch('/api/user/info')
        if (response.ok) {
          const data = await response.json()

          // Store repositories from proxy info in sessionStorage for access by new session page
          if (typeof window !== 'undefined' && data.proxy?.repositories) {
            sessionStorage.setItem('user_repositories', JSON.stringify(data.proxy.repositories))
          }
        }
      } catch (error) {
        console.error('Failed to load user info:', error)
      }
    }

    loadUserInfo()
  }, [])

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar
        title="Conversations"
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
        {/* フィルタサイドバー */}
        <TagFilterSidebar
          onFiltersChange={setTagFilters}
          currentFilters={tagFilters}
          isVisible={sidebarVisible}
          onToggleVisibility={() => setSidebarVisible(!sidebarVisible)}
        />

        {/* メインコンテンツ */}
        <div className="flex-1 px-4 md:px-6 lg:px-8 pt-2 md:pt-8 pb-6 md:pb-8">
          {/* セッション開始ボタン（デスクトップのみ） */}
          <div className="mb-6 flex justify-end hidden md:flex">
            <Link
              href="/sessions/new"
              className="inline-flex items-center px-6 py-3 bg-blue-600 hover:bg-blue-700 text-white font-medium rounded-lg transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
            >
              <svg className="w-5 h-5 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
              </svg>
              新しいセッションを開始
            </Link>
          </div>

          {/* アクティブフィルタの表示 */}
          {Object.keys(tagFilters).length > 0 && (
            <div className="mb-6 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
              <div className="flex items-center justify-between">
                <div className="flex flex-wrap gap-2">
                  <span className="text-sm font-medium text-blue-800 dark:text-blue-200">
                    アクティブフィルタ:
                  </span>
                  {Object.entries(tagFilters).map(([key, values]) =>
                    values.map(value => (
                      <span
                        key={`${key}-${value}`}
                        className="inline-flex items-center px-2 py-1 text-xs bg-blue-100 dark:bg-blue-800 text-blue-800 dark:text-blue-200 rounded-full"
                      >
                        {key}: {value}
                        <button
                          onClick={() => {
                            const newFilters = { ...tagFilters }
                            newFilters[key] = newFilters[key].filter(v => v !== value)
                            if (newFilters[key].length === 0) {
                              delete newFilters[key]
                            }
                            setTagFilters(newFilters)
                          }}
                          className="ml-1 text-blue-600 dark:text-blue-300 hover:text-blue-800 dark:hover:text-blue-100"
                        >
                          ×
                        </button>
                      </span>
                    ))
                  )}
                </div>
                <button
                  onClick={() => setTagFilters({})}
                  className="text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300"
                >
                  すべてクリア
                </button>
              </div>
            </div>
          )}

          {/* セッション一覧 */}
          <SessionListView
            tagFilters={tagFilters}
            onSessionsUpdate={handleSessionsUpdate}
            key={refreshKey}
          />
        </div>
      </div>

      {/* フローティング新セッションボタン */}
      <FloatingNewSessionButton />
    </main>
  )
}

export default function ChatsPage() {
  return (
    <Suspense>
      <ChatsPageInner />
    </Suspense>
  )
}
