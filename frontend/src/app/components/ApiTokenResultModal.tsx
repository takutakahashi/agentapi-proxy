'use client'

import { useState, useEffect, useCallback } from 'react'
import { CreateApiTokenResponse } from '../../types/api_token'

interface ApiTokenResultModalProps {
  /** The creation response to display. When provided the modal is open. */
  response: CreateApiTokenResponse | null
  onClose: () => void
}

/**
 * Shows the plaintext API token exactly once after creation.
 *
 * Security requirements:
 *  - The plaintext token is kept only in component state (never localStorage).
 *  - On close, the parent clears `response` so the token is garbage-collected.
 *  - A strong one-time warning is shown and the copy action is provided.
 */
export default function ApiTokenResultModal({ response, onClose }: ApiTokenResultModalProps) {
  const [copied, setCopied] = useState(false)
  const [dismissed, setDismissed] = useState(false)

  // Reset transient state whenever a new token is shown.
  useEffect(() => {
    if (response) {
      setCopied(false)
      setDismissed(false)
    }
  }, [response])

  const handleCopy = useCallback(async () => {
    if (!response) return
    try {
      await navigator.clipboard.writeText(response.plaintext_token)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard may be unavailable; the user can still manually select the text.
      setCopied(false)
    }
  }, [response])

  // ESC to close (after acknowledging)
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && response && dismissed) handleClose()
    }
    if (response) document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [response, dismissed])

  // handleClose clears local state and notifies the parent to drop the token.
  const handleClose = useCallback(() => {
    setDismissed(false)
    setCopied(false)
    onClose()
  }, [onClose])

  if (!response) return null

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 rounded-lg w-full max-w-lg shadow-xl overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
            API トークンを作成しました
          </h2>
          <button
            type="button"
            onClick={handleClose}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
            aria-label="閉じる"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="px-6 py-4 space-y-4">
          {/* One-time warning */}
          <div className="bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-300 dark:border-yellow-700 rounded-md p-3">
            <div className="flex items-start gap-2">
              <svg className="w-5 h-5 text-yellow-600 dark:text-yellow-400 flex-shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01M5 19h14a2 2 0 001.66-3.12L13.66 4.8a2 2 0 00-3.32 0L3.34 15.88A2 2 0 005 19z" />
              </svg>
              <p className="text-sm text-yellow-800 dark:text-yellow-300">
                <strong>重要:</strong> このトークンは<strong>今度一度だけ</strong>表示されます。
                閉じると二度と確認できません。今すぐ安全な場所にコピー・保存してください。
              </p>
            </div>
          </div>

          {/* Token name */}
          <div>
            <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
              トークン名
            </label>
            <div className="text-sm text-gray-900 dark:text-white">
              {response.token.name}
            </div>
          </div>

          {/* Plaintext token */}
          <div>
            <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
              API トークン
            </label>
            <div className="flex items-center gap-2">
              <code
                data-testid="api-token-plaintext"
                className="flex-1 px-3 py-2 text-sm font-mono bg-gray-100 dark:bg-gray-900 border border-gray-300 dark:border-gray-600 rounded-md text-gray-900 dark:text-white break-all select-all"
              >
                {response.plaintext_token}
              </code>
              <button
                type="button"
                onClick={handleCopy}
                className="flex-shrink-0 px-3 py-2 text-sm bg-blue-600 text-white rounded-md hover:bg-blue-700 transition-colors inline-flex items-center gap-1"
              >
                {copied ? (
                  <>
                    <svg className="w-4 h-4 text-green-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                    </svg>
                    コピー済み
                  </>
                ) : (
                  <>
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                    </svg>
                    コピー
                  </>
                )}
              </button>
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="flex justify-end px-6 py-4 border-t border-gray-200 dark:border-gray-700">
          <button
            type="button"
            onClick={handleClose}
            className="px-4 py-2 text-sm text-white bg-gray-700 hover:bg-gray-800 dark:bg-gray-600 dark:hover:bg-gray-500 rounded-md transition-colors"
          >
            閉じる（トークンを非表示）
          </button>
        </div>
      </div>
    </div>
  )
}