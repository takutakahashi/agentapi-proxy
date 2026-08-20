'use client'

import { useState, useEffect, useCallback } from 'react'
import {
  API_TOKEN_PERMISSIONS,
  API_TOKEN_PERMISSION_LABELS,
  ApiTokenPermission,
  ApiTokenScope,
  CreateApiTokenRequest,
  CreateApiTokenResponse,
} from '../../types/api_token'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'

interface ApiTokenCreateModalProps {
  isOpen: boolean
  scope: ApiTokenScope
  teamId?: string
  onClose: () => void
  onCreated: (response: CreateApiTokenResponse) => void
}

/** Expiry choices in days; `null` means no expiration. */
const EXPIRY_OPTIONS: Array<{ label: string; days: number | null }> = [
  { label: '30日', days: 30 },
  { label: '90日', days: 90 },
  { label: '365日', days: 365 },
  { label: '無期限', days: null },
]

const NAME_MAX_LENGTH = 64

export default function ApiTokenCreateModal({
  isOpen,
  scope,
  teamId,
  onClose,
  onCreated,
}: ApiTokenCreateModalProps) {
  const [name, setName] = useState('')
  const [selectedPermissions, setSelectedPermissions] = useState<string[]>([])
  const [expiryDays, setExpiryDays] = useState<number | null>(30)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const resetForm = useCallback(() => {
    setName('')
    setSelectedPermissions([])
    setExpiryDays(30)
    setError(null)
  }, [])

  useEffect(() => {
    if (isOpen) resetForm()
  }, [isOpen, resetForm])

  // ESC to close
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen && !isSubmitting) onClose()
    }
    if (isOpen) document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose, isSubmitting])

  const togglePermission = (perm: ApiTokenPermission) => {
    setSelectedPermissions((prev) =>
      prev.includes(perm) ? prev.filter((p) => p !== perm) : [...prev, perm]
    )
  }

  const trimmedName = name.trim()
  const nameValid = trimmedName.length >= 1 && trimmedName.length <= NAME_MAX_LENGTH
  const canSubmit = nameValid && !isSubmitting

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!nameValid) {
      setError('トークン名は1〜64文字で入力してください')
      return
    }

    // For team scope, teamId is required and fixed by the parent section.
    if (scope === 'team' && !teamId) {
      setError('チームが選択されていません')
      return
    }

    let expires_at: string | null = null
    if (expiryDays !== null) {
      const d = new Date()
      d.setDate(d.getDate() + expiryDays)
      expires_at = d.toISOString()
    }

    const payload: CreateApiTokenRequest = {
      name: trimmedName,
      scope,
      team_id: scope === 'team' ? teamId : undefined,
      permissions: selectedPermissions.length > 0 ? selectedPermissions : undefined,
      expires_at,
    }

    try {
      setIsSubmitting(true)
      setError(null)
      const client = createAgentAPIProxyClientFromStorage()
      const response = await client.createApiToken(payload)
      // Clear sensitive form state before handing off.
      resetForm()
      onCreated(response)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'トークンの作成に失敗しました')
    } finally {
      setIsSubmitting(false)
    }
  }

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 rounded-lg w-full max-w-lg shadow-xl overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
            {scope === 'team' ? 'チーム API トークンを作成' : 'API トークンを作成'}
          </h2>
          <button
            type="button"
            onClick={onClose}
            disabled={isSubmitting}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 disabled:opacity-50"
            aria-label="閉じる"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <form onSubmit={handleSubmit} className="px-6 py-4 space-y-4 max-h-[75vh] overflow-y-auto">
          {error && (
            <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md p-3 text-sm text-red-700 dark:text-red-400">
              {error}
            </div>
          )}

          {/* Name */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              トークン名 <span className="text-red-500">*</span>
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={NAME_MAX_LENGTH}
              placeholder="例: CI 用トークン"
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm"
              required
              autoFocus
            />
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {trimmedName.length}/{NAME_MAX_LENGTH} 文字
            </p>
          </div>

          {/* Scope (informational, fixed) */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              スコープ
            </label>
            <div className="px-3 py-2 text-sm bg-gray-50 dark:bg-gray-700/50 border border-gray-200 dark:border-gray-600 rounded-md text-gray-700 dark:text-gray-300">
              {scope === 'team' ? (
                <>チーム (Team) — {teamId || '未選択'}</>
              ) : (
                <>パーソナル (Personal)</>
              )}
            </div>
          </div>

          {/* Permissions */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              権限 (パーミッション)
            </label>
            <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
              付与する権限を選択してください。未選択の場合は既定の権限が適用されます。
            </p>
            <div className="space-y-2">
              {API_TOKEN_PERMISSIONS.map((perm) => (
                <label
                  key={perm}
                  className="flex items-center gap-2 cursor-pointer text-sm text-gray-700 dark:text-gray-300"
                >
                  <input
                    type="checkbox"
                    checked={selectedPermissions.includes(perm)}
                    onChange={() => togglePermission(perm)}
                    className="w-4 h-4 text-blue-600 border-gray-300 rounded focus:ring-blue-500"
                  />
                  <span>{API_TOKEN_PERMISSION_LABELS[perm]}</span>
                </label>
              ))}
            </div>
          </div>

          {/* Expiry */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              有効期限
            </label>
            <div className="flex flex-wrap gap-2">
              {EXPIRY_OPTIONS.map((opt) => {
                const active = expiryDays === opt.days
                return (
                  <button
                    key={opt.label}
                    type="button"
                    onClick={() => setExpiryDays(opt.days)}
                    className={`px-3 py-1.5 text-sm rounded-md border transition-colors ${
                      active
                        ? 'bg-blue-600 text-white border-blue-600'
                        : 'bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-300 border-gray-300 dark:border-gray-600 hover:border-blue-400'
                    }`}
                  >
                    {opt.label}
                  </button>
                )
              })}
            </div>
          </div>
        </form>

        {/* Footer */}
        <div className="flex justify-end gap-3 px-6 py-4 border-t border-gray-200 dark:border-gray-700">
          <button
            type="button"
            onClick={onClose}
            disabled={isSubmitting}
            className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors disabled:opacity-50"
          >
            キャンセル
          </button>
          <button
            type="submit"
            form=""
            onClick={handleSubmit}
            disabled={!canSubmit}
            className="px-4 py-2 text-sm text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 rounded-md transition-colors inline-flex items-center gap-2"
          >
            {isSubmitting && (
              <svg className="w-4 h-4 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
            )}
            {isSubmitting ? '作成中...' : '作成'}
          </button>
        </div>
      </div>
    </div>
  )
}