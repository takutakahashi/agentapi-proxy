'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { useRouter } from 'next/navigation'
import { LogOut, Menu, Settings, ShieldCheck, SlidersHorizontal } from 'lucide-react'
import PreferencesDialog from './PreferencesDialog'
import { UserInfo } from '@/types/user'

// ページ読み込みごとに 1 回だけ /api/user/info を取得する（メニューを開くたびに叩かない）
let userInfoPromise: Promise<UserInfo | null> | null = null

const fetchUserInfoOnce = (): Promise<UserInfo | null> => {
  if (!userInfoPromise) {
    userInfoPromise = fetch('/api/user/info')
      .then((res) => (res.ok ? (res.json() as Promise<UserInfo>) : null))
      .catch(() => null)
  }
  return userInfoPromise
}

export default function GlobalMenu() {
  const router = useRouter()
  const [isOpen, setIsOpen] = useState(false)
  const [isAdmin, setIsAdmin] = useState(false)
  const [showPreferences, setShowPreferences] = useState(false)
  const [loggingOut, setLoggingOut] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)

  // 管理者判定はメニューを最初に開いたときにだけ取得する
  useEffect(() => {
    if (!isOpen) return
    let cancelled = false
    fetchUserInfoOnce().then((info) => {
      if (!cancelled) setIsAdmin(info?.proxy?.is_admin === true)
    })
    return () => {
      cancelled = true
    }
  }, [isOpen])

  const closeMenu = useCallback(() => {
    setIsOpen(false)
    triggerRef.current?.focus()
  }, [])

  // 外側クリックで閉じる
  useEffect(() => {
    if (!isOpen) return
    const handleClickOutside = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [isOpen])

  // Esc で閉じる / 矢印キーで項目を移動する
  useEffect(() => {
    if (!isOpen) return
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        closeMenu()
        return
      }
      if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
      if (!menuRef.current) return

      e.preventDefault()
      const items = Array.from(
        menuRef.current.querySelectorAll<HTMLElement>('[role="menuitem"]')
      )
      if (items.length === 0) return

      const currentIndex = items.indexOf(document.activeElement as HTMLElement)
      const nextIndex =
        e.key === 'ArrowDown'
          ? (currentIndex + 1 + items.length) % items.length
          : (currentIndex - 1 + items.length) % items.length
      items[nextIndex]?.focus()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, closeMenu])

  // 開いたら先頭の項目にフォーカスする
  useEffect(() => {
    if (!isOpen || !menuRef.current) return
    menuRef.current.querySelector<HTMLElement>('[role="menuitem"]')?.focus()
  }, [isOpen])

  const handleToggle = () => {
    setIsOpen((prev) => !prev)
  }

  const handleAccountSettings = () => {
    setIsOpen(false)
    router.push('/settings')
  }

  const handlePreferences = () => {
    setIsOpen(false)
    setShowPreferences(true)
  }

  const handleAdminSettings = () => {
    setIsOpen(false)
    router.push('/admin')
  }

  const handleLogout = async () => {
    if (loggingOut) return
    if (!confirm('ログアウトしますか？')) return
    setLoggingOut(true)
    try {
      const response = await fetch('/api/auth/logout', { method: 'POST' })
      if (response.ok) {
        setIsOpen(false)
        router.push('/login')
      }
    } catch (error) {
      console.error('Logout failed:', error)
    } finally {
      setLoggingOut(false)
    }
  }

  const itemClass =
    'w-full flex items-start gap-3 px-3 py-2 text-sm text-left rounded-md text-gray-900 dark:text-white hover:bg-gray-100 dark:hover:bg-gray-700 focus:bg-gray-100 dark:focus:bg-gray-700 focus:outline-none transition-colors'

  return (
    <>
      <div className="relative" ref={containerRef}>
        <button
          ref={triggerRef}
          type="button"
          onClick={handleToggle}
          className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors"
          title="メニュー"
          aria-label="メニュー"
          aria-haspopup="menu"
          aria-expanded={isOpen}
        >
          <Menu className="w-5 h-5" />
        </button>

        {isOpen && (
          <div
            ref={menuRef}
            role="menu"
            aria-label="メニュー"
            className="absolute right-0 mt-2 w-60 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg z-50 p-1"
          >
            <button type="button" role="menuitem" onClick={handleAccountSettings} className={itemClass}>
              <Settings className="w-4 h-4 mt-0.5 flex-shrink-0 text-gray-500 dark:text-gray-400" />
              <span>
                <span className="block font-medium">アカウント設定</span>
                <span className="block text-xs text-gray-500 dark:text-gray-400">
                  AI プロバイダ、MCP、チーム設定など
                </span>
              </span>
            </button>

            <button type="button" role="menuitem" onClick={handlePreferences} className={itemClass}>
              <SlidersHorizontal className="w-4 h-4 mt-0.5 flex-shrink-0 text-gray-500 dark:text-gray-400" />
              <span>
                <span className="block font-medium">環境設定</span>
                <span className="block text-xs text-gray-500 dark:text-gray-400">
                  フォント、キー操作 — このブラウザのみ
                </span>
              </span>
            </button>

            {isAdmin && (
              <button type="button" role="menuitem" onClick={handleAdminSettings} className={itemClass}>
                <ShieldCheck className="w-4 h-4 mt-0.5 flex-shrink-0 text-gray-500 dark:text-gray-400" />
                <span>
                  <span className="block font-medium">Admin 設定</span>
                  <span className="block text-xs text-gray-500 dark:text-gray-400">
                    システム全体の設定
                  </span>
                </span>
              </button>
            )}

            <div className="my-1 border-t border-gray-200 dark:border-gray-700" />

            <button
              type="button"
              role="menuitem"
              onClick={handleLogout}
              disabled={loggingOut}
              className="w-full flex items-center gap-3 px-3 py-2 text-sm text-left rounded-md text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 focus:bg-red-50 dark:focus:bg-red-900/20 focus:outline-none disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              <LogOut className="w-4 h-4 flex-shrink-0" />
              <span className="font-medium">{loggingOut ? 'ログアウト中...' : 'ログアウト'}</span>
            </button>
          </div>
        )}
      </div>

      <PreferencesDialog isOpen={showPreferences} onClose={() => setShowPreferences(false)} />
    </>
  )
}
