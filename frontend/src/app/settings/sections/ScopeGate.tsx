'use client'

import { useRouter } from 'next/navigation'
import { useState } from 'react'
import { useSettingsScope } from '../SettingsScopeContext'

/**
 * 設定の読み込みが終わるまでスピナーを、失敗したらエラーを出す。
 * 読み込めた後は保存エラーをバナーとして上に出しつつ中身を描画する。
 */
export function ScopeGate({ children }: { children: React.ReactNode }) {
  const { loading, ready, error, isAuthError } = useSettingsScope()
  const router = useRouter()
  const [loggingOut, setLoggingOut] = useState(false)

  const handleLogout = async () => {
    if (loggingOut) return
    setLoggingOut(true)
    try {
      const response = await fetch('/api/auth/logout', { method: 'POST' })
      if (response.ok) router.push('/login')
    } catch (err) {
      console.error('Logout failed:', err)
    } finally {
      setLoggingOut(false)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-blue-600" />
      </div>
    )
  }

  const errorBanner = error ? (
    <div className="mb-5 rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-800 dark:bg-red-900/20">
      <p className="text-sm text-red-600 dark:text-red-400">{error}</p>
      {isAuthError && (
        <button
          type="button"
          onClick={handleLogout}
          disabled={loggingOut}
          className="mt-3 inline-flex items-center rounded-md bg-red-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-red-700 disabled:opacity-50"
        >
          {loggingOut ? 'ログアウト中...' : 'ログアウト'}
        </button>
      )}
    </div>
  ) : null

  if (!ready) {
    return errorBanner ?? (
      <p className="py-16 text-center text-sm text-gray-500 dark:text-gray-400">
        設定を表示できませんでした
      </p>
    )
  }

  return (
    <>
      {errorBanner}
      {children}
    </>
  )
}
