'use client'

import { useEffect, useRef, useState } from 'react'
import { useRouter } from 'next/navigation'
import { Check, ChevronDown, User, Users } from 'lucide-react'
import { DEFAULT_SETTINGS_SLUG, SettingsScopeKind, navItemsForScope, settingsHref } from './navConfig'

interface ScopeSwitcherProps {
  scopeKind: SettingsScopeKind
  /** 現在のチーム名（scopeKind が 'team' のとき） */
  teamId?: string
  userName: string
  userTeams: string[]
  /** 現在開いているページの slug。切替後も同じページを開くために使う */
  currentSlug: string
  /** 未保存の変更があるときに遷移してよいか確認する */
  confirmLeave: () => boolean
}

export function ScopeSwitcher({
  scopeKind,
  teamId,
  userName,
  userTeams,
  currentSlug,
  confirmLeave,
}: ScopeSwitcherProps) {
  const router = useRouter()
  const [isOpen, setIsOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!isOpen) return
    const handleClickOutside = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false)
      }
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setIsOpen(false)
    }
    document.addEventListener('mousedown', handleClickOutside)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isOpen])

  const switchTo = (nextScope: SettingsScopeKind, nextTeamId?: string) => {
    setIsOpen(false)
    if (nextScope === scopeKind && nextTeamId === teamId) return
    if (!confirmLeave()) return

    // 切替先に同じページが無ければ先頭のページを開く
    const hasSameSlug = navItemsForScope(nextScope).some((item) => item.slug === currentSlug)
    const slug = hasSameSlug ? currentSlug : DEFAULT_SETTINGS_SLUG
    router.push(settingsHref(nextScope, slug, nextTeamId))
  }

  const currentLabel = scopeKind === 'personal' ? userName || 'Personal' : teamId || 'Team'
  const CurrentIcon = scopeKind === 'personal' ? User : Users

  return (
    <div className="relative" ref={containerRef}>
      <button
        type="button"
        onClick={() => setIsOpen((prev) => !prev)}
        aria-haspopup="listbox"
        aria-expanded={isOpen}
        className="flex w-full items-center gap-2 rounded-lg border border-gray-300 bg-white px-2.5 py-2 text-left transition-colors hover:bg-gray-50 dark:border-gray-700 dark:bg-gray-900 dark:hover:bg-gray-800"
      >
        <span className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md bg-blue-50 text-blue-600 dark:bg-blue-900/40 dark:text-blue-300">
          <CurrentIcon className="h-3.5 w-3.5" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-semibold text-gray-900 dark:text-white">
            {currentLabel}
          </span>
          <span className="block text-[10px] leading-tight text-gray-500 dark:text-gray-400">
            {scopeKind === 'personal' ? 'Personal' : 'Team'}
          </span>
        </span>
        <ChevronDown className="h-3.5 w-3.5 flex-shrink-0 text-gray-500 dark:text-gray-400" />
      </button>

      {isOpen && (
        <div
          role="listbox"
          className="absolute left-0 right-0 z-40 mt-1 overflow-hidden rounded-lg border border-gray-200 bg-white shadow-lg dark:border-gray-700 dark:bg-gray-800"
        >
          <button
            type="button"
            role="option"
            aria-selected={scopeKind === 'personal'}
            onClick={() => switchTo('personal')}
            className={`flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition-colors hover:bg-gray-100 dark:hover:bg-gray-700 ${
              scopeKind === 'personal'
                ? 'font-semibold text-blue-700 dark:text-blue-300'
                : 'text-gray-900 dark:text-white'
            }`}
          >
            <User className="h-3.5 w-3.5 flex-shrink-0 text-gray-500 dark:text-gray-400" />
            <span className="truncate">{userName || 'Personal'}</span>
            {scopeKind === 'personal' && <Check className="ml-auto h-3.5 w-3.5 flex-shrink-0" />}
          </button>

          {userTeams.length > 0 && (
            <>
              <p className="border-t border-gray-200 px-3 pt-2 pb-1 text-[10px] font-semibold uppercase tracking-wider text-gray-500 dark:border-gray-700 dark:text-gray-400">
                Teams
              </p>
              {userTeams.map((team) => (
                <button
                  key={team}
                  type="button"
                  role="option"
                  aria-selected={scopeKind === 'team' && teamId === team}
                  onClick={() => switchTo('team', team)}
                  className={`flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition-colors hover:bg-gray-100 dark:hover:bg-gray-700 ${
                    scopeKind === 'team' && teamId === team
                      ? 'font-semibold text-blue-700 dark:text-blue-300'
                      : 'text-gray-900 dark:text-white'
                  }`}
                >
                  <Users className="h-3.5 w-3.5 flex-shrink-0 text-gray-500 dark:text-gray-400" />
                  <span className="truncate">{team}</span>
                  {scopeKind === 'team' && teamId === team && (
                    <Check className="ml-auto h-3.5 w-3.5 flex-shrink-0" />
                  )}
                </button>
              ))}
            </>
          )}
        </div>
      )}
    </div>
  )
}
