'use client'

import { useState, useEffect, useRef } from 'react'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import GlobalMenu from './GlobalMenu'

interface TopBarProps {
  title: string
  subtitle?: string
  showFilterButton?: boolean
  filterButtonText?: string
  onFilterToggle?: () => void
  showSettingsButton?: boolean
  showNewSessionButton?: boolean
  onNewSession?: () => void
  children?: React.ReactNode
}

export default function TopBar({
  title,
  subtitle,
  showFilterButton = false,
  filterButtonText = 'フィルタを表示',
  onFilterToggle,
  showSettingsButton = true,
  showNewSessionButton = false,
  onNewSession,
  children
}: TopBarProps) {
  const [showTeamDropdown, setShowTeamDropdown] = useState(false)
  const teamDropdownRef = useRef<HTMLDivElement>(null)

  const { selectedTeam, availableTeams, selectTeam, setAvailableTeams, isLoading: isTeamLoading } = useTeamScope()

  // Fetch user info to get available teams
  useEffect(() => {
    const fetchUserInfo = async () => {
      try {
        const response = await fetch('/api/user/info')
        if (response.ok) {
          const userInfo = await response.json()
          if (userInfo.proxy?.teams && Array.isArray(userInfo.proxy.teams)) {
            setAvailableTeams(userInfo.proxy.teams)
          }
        }
      } catch (error) {
        console.error('Failed to fetch user info for teams:', error)
      }
    }

    // Only fetch if we don't have teams yet
    if (availableTeams.length === 0) {
      fetchUserInfo()
    }
  }, [availableTeams.length, setAvailableTeams])

  // ドロップダウン外クリックで閉じる
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (teamDropdownRef.current && !teamDropdownRef.current.contains(event.target as Node)) {
        setShowTeamDropdown(false)
      }
    }

    if (showTeamDropdown) {
      document.addEventListener('mousedown', handleClickOutside)
    }

    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [showTeamDropdown])

  // Get display name for the current selection
  const getDisplayName = () => {
    if (isTeamLoading) return '...'
    if (selectedTeam) {
      // Show short form: team-slug (without org/)
      const parts = selectedTeam.split('/')
      return parts.length > 1 ? parts[1] : selectedTeam
    }
    return 'Personal'
  }

  return (
    <div className="sticky top-0 z-40 bg-white dark:bg-gray-800 shadow-sm border-b border-gray-200 dark:border-gray-700 relative">
      <div className="relative px-4 md:px-6 lg:px-8 py-3 md:py-4">
        {/* ヘッダーセクション */}
        <div className="flex items-center justify-between gap-4">
          <div className="flex-1">
            <h1 className="text-xl sm:text-2xl font-bold text-gray-900 dark:text-white">
              {title}
            </h1>
            {subtitle && (
              <p className="text-gray-600 dark:text-gray-400 mt-1">
                {subtitle}
              </p>
            )}
          </div>

          {/* 右側のボタン群 */}
          <div className="flex items-center gap-2 flex-shrink-0">
            {/* フィルタボタン */}
            {showFilterButton && onFilterToggle && (
              <button
                onClick={onFilterToggle}
                className="inline-flex items-center px-3 py-2 text-sm bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors"
              >
                <svg className="w-4 h-4 sm:mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 4a1 1 0 011-1h16a1 1 0 011 1v2.586a1 1 0 01-.293.707l-6.414 6.414a1 1 0 00-.293.707V17l-4 4v-6.586a1 1 0 00-.293-.707L3.293 7.414A1 1 0 013 6.707V4z" />
                </svg>
                <span className="hidden sm:inline">{filterButtonText}</span>
              </button>
            )}

            {/* 新しいセッションボタン */}
            {showNewSessionButton && onNewSession && (
              <button
                onClick={onNewSession}
                className="inline-flex items-center px-4 py-2 bg-main-color-dark hover:bg-main-color text-white font-medium rounded-md transition-colors focus:outline-none focus:ring-2 focus:ring-main-color focus:ring-offset-2 shadow-sm"
              >
                <svg className="w-4 h-4 sm:mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                </svg>
                <span className="hidden sm:inline">新しいセッション</span>
                <span className="sm:hidden">新規</span>
              </button>
            )}

            {/* チーム選択ドロップダウン */}
            {availableTeams.length > 0 && (
              <div className="relative" ref={teamDropdownRef}>
                <button
                  onClick={() => setShowTeamDropdown(!showTeamDropdown)}
                  className="inline-flex items-center px-3 py-2 text-sm bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors"
                  title="スコープを選択"
                >
                  {selectedTeam ? (
                    <svg className="w-4 h-4 mr-2 text-blue-600 dark:text-blue-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z" />
                    </svg>
                  ) : (
                    <svg className="w-4 h-4 mr-2 text-gray-500 dark:text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
                    </svg>
                  )}
                  <span className="hidden sm:inline max-w-24 truncate">{getDisplayName()}</span>
                  <svg className="w-4 h-4 ml-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                  </svg>
                </button>

                {/* チーム選択ドロップダウンメニュー */}
                {showTeamDropdown && (
                  <div className="absolute right-0 mt-2 w-64 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg z-50">
                    <div className="py-1">
                      <div className="px-3 py-2 text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider">
                        スコープを選択
                      </div>
                      
                      {/* Personal option */}
                      <button
                        onClick={() => {
                          selectTeam(null)
                          setShowTeamDropdown(false)
                        }}
                        className={`w-full flex items-center px-3 py-2 text-sm hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors ${
                          !selectedTeam ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300' : 'text-gray-900 dark:text-white'
                        }`}
                      >
                        <svg className="w-4 h-4 mr-3 text-gray-500 dark:text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
                        </svg>
                        <span>Personal</span>
                        {!selectedTeam && (
                          <svg className="w-4 h-4 ml-auto text-blue-600 dark:text-blue-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                          </svg>
                        )}
                      </button>

                      {/* Divider */}
                      <div className="my-1 border-t border-gray-200 dark:border-gray-700"></div>

                      <div className="px-3 py-2 text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider">
                        Teams
                      </div>

                      {/* Team options */}
                      {availableTeams.map((team) => (
                        <button
                          key={team}
                          onClick={() => {
                            selectTeam(team)
                            setShowTeamDropdown(false)
                          }}
                          className={`w-full flex items-center px-3 py-2 text-sm hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors ${
                            selectedTeam === team ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300' : 'text-gray-900 dark:text-white'
                          }`}
                        >
                          <svg className="w-4 h-4 mr-3 text-blue-600 dark:text-blue-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z" />
                          </svg>
                          <span className="truncate">{team}</span>
                          {selectedTeam === team && (
                            <svg className="w-4 h-4 ml-auto flex-shrink-0 text-blue-600 dark:text-blue-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                            </svg>
                          )}
                        </button>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}

            {/* メニュー（アカウント設定 / 環境設定 / Admin 設定 / ログアウト） */}
            {showSettingsButton && <GlobalMenu />}

          </div>
        </div>

        {/* 追加コンテンツ */}
        {children && <div className="mt-3">{children}</div>}
      </div>
    </div>
  )
}
