'use client'

import { useEffect } from 'react'
import { ApiToken } from '../../types/api_token'

interface ApiTokenDeleteConfirmModalProps {
  token: ApiToken | null
  isDeleting?: boolean
  error?: string | null
  onConfirm: () => void
  onCancel: () => void
}

export default function ApiTokenDeleteConfirmModal({
  token,
  isDeleting,
  error,
  onConfirm,
  onCancel,
}: ApiTokenDeleteConfirmModalProps) {
  // ESC to cancel (not while deleting)
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && token && !isDeleting) onCancel()
    }
    if (token) document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [token, isDeleting, onCancel])

  if (!token) return null

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 rounded-lg w-full max-w-md shadow-xl overflow-hidden">
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
            トークンを削除
          </h2>
          <button
            type="button"
            onClick={onCancel}
            disabled={isDeleting}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 disabled:opacity-50"
            aria-label="閉じる"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="px-6 py-4 space-y-3">
          <p className="text-sm text-gray-700 dark:text-gray-300">
            以下の API トークンを削除します。この操作は取り消せません。
          </p>
          <div className="bg-gray-50 dark:bg-gray-700/50 border border-gray-200 dark:border-gray-600 rounded-md p-3">
            <div className="text-sm font-medium text-gray-900 dark:text-white">
              {token.name}
            </div>
            <div className="mt-1 text-xs font-mono text-gray-500 dark:text-gray-400">
              {token.token_prefix}
            </div>
          </div>
          <div className="bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-700 rounded-md p-3">
            <p className="text-xs text-yellow-800 dark:text-yellow-300">
              削除したトークンが現在の UI セッションで使用されている場合、以降の操作で認証エラーとなりログイン画面に遷移します。
            </p>
          </div>
          {error && (
            <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md p-3 text-sm text-red-700 dark:text-red-400">
              {error}
            </div>
          )}
        </div>

        <div className="flex justify-end gap-3 px-6 py-4 border-t border-gray-200 dark:border-gray-700">
          <button
            type="button"
            onClick={onCancel}
            disabled={isDeleting}
            className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors disabled:opacity-50"
          >
            キャンセル
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={isDeleting}
            className="px-4 py-2 text-sm text-white bg-red-600 hover:bg-red-700 disabled:opacity-50 rounded-md transition-colors inline-flex items-center gap-2"
          >
            {isDeleting && (
              <svg className="w-4 h-4 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
            )}
            {isDeleting ? '削除中...' : '削除'}
          </button>
        </div>
      </div>
    </div>
  )
}