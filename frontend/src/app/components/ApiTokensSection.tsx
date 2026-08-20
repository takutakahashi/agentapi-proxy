'use client'

import { useState, useEffect, useCallback } from 'react'
import {
  ApiToken,
  ApiTokenScope,
  CreateApiTokenResponse,
} from '../../types/api_token'
import { createAgentAPIProxyClientFromStorage, AgentAPIProxyError } from '../../lib/agentapi-proxy-client'
import { useToast } from '../../contexts/ToastContext'
import ApiTokenCreateModal from './ApiTokenCreateModal'
import ApiTokenResultModal from './ApiTokenResultModal'
import ApiTokenDeleteConfirmModal from './ApiTokenDeleteConfirmModal'

interface ApiTokensSectionProps {
  /** Which settings page this section is rendered in. */
  scope: ApiTokenScope
  /** Required and fixed for team scope; ignored for personal scope. */
  teamId?: string
}

function formatDate(iso?: string | null): string {
  if (!iso) return '無期限'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

export default function ApiTokensSection({
  scope,
  teamId,
}: ApiTokensSectionProps) {
  const { showToast } = useToast()
  const [tokens, setTokens] = useState<ApiToken[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [showCreate, setShowCreate] = useState(false)
  // Plaintext token shown once. Stored only in memory; cleared on close.
  const [createdResponse, setCreatedResponse] = useState<CreateApiTokenResponse | null>(null)

  const [deleteTarget, setDeleteTarget] = useState<ApiToken | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const loadTokens = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const params =
        scope === 'team'
          ? { scope, team_id: teamId }
          : { scope }
      const res = await client.getApiTokens(params)
      setTokens(res.items || [])
    } catch (err) {
      if (err instanceof AgentAPIProxyError && err.status === 401) {
        // Auth flow already triggered in makeRequest; surface a message.
        setError('認証が必要です。ログインし直してください。')
      } else {
        setError(err instanceof Error ? err.message : 'API トークンの取得に失敗しました')
      }
    } finally {
      setLoading(false)
    }
  }, [scope, teamId])

  useEffect(() => {
    // For team scope, only load when a team is selected.
    if (scope === 'team' && !teamId) return
    loadTokens()
  }, [scope, teamId, loadTokens])

  const handleCreated = (response: CreateApiTokenResponse) => {
    setShowCreate(false)
    // Keep plaintext only in memory; it will be cleared when the result modal closes.
    setCreatedResponse(response)
    // Refresh the list so the new token's metadata appears.
    loadTokens()
    showToast('API トークンを作成しました', 'success')
  }

  const handleResultClose = useCallback(() => {
    // Discard the plaintext token immediately.
    setCreatedResponse(null)
  }, [])

  const handleDeleteRequest = (token: ApiToken) => {
    setDeleteError(null)
    setDeleteTarget(token)
  }

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return
    const targetId = deleteTarget.id
    setDeleting(true)
    setDeleteError(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.deleteApiToken(targetId)
      setDeleteTarget(null)
      showToast('API トークンを削除しました', 'success')
      // Refresh the list. If the deleted token backed this UI session, the
      // next request may return 401; the existing auth flow (logout → /login)
      // handles that. We do not compare plaintext tokens.
      await loadTokens()
    } catch (err) {
      if (err instanceof AgentAPIProxyError && err.status === 401) {
        // Session lost (likely the deleted token was in use). Auth flow handled.
        setDeleteError('認証エラーが発生しました。ログインし直してください。')
      } else {
        setDeleteError(err instanceof Error ? err.message : '削除に失敗しました')
      }
    } finally {
      setDeleting(false)
    }
  }

  const handleDeleteCancel = () => {
    if (deleting) return
    setDeleteTarget(null)
    setDeleteError(null)
  }

  return (
    <>
      <div className="space-y-4">
        {/* Toolbar */}
        <div className="flex items-center justify-between">
          <p className="text-xs text-gray-500 dark:text-gray-400">
            トークンは作成時に一度だけ表示されます。安全な場所に保存してください。
          </p>
          <button
            type="button"
            onClick={() => setShowCreate(true)}
            disabled={scope === 'team' && !teamId}
            className="flex items-center gap-1 text-sm text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
            トークンを作成
          </button>
        </div>

        {/* Error */}
        {error && (
          <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md p-3 text-sm text-red-700 dark:text-red-400">
            {error}
          </div>
        )}

        {/* Loading */}
        {loading && tokens.length === 0 && (
          <div className="flex items-center justify-center py-6">
            <div className="animate-spin rounded-full h-6 w-6 border-b-2 border-blue-600"></div>
          </div>
        )}

        {/* Empty */}
        {!loading && !error && tokens.length === 0 && (
          <p className="text-sm text-gray-500 dark:text-gray-400 py-4 text-center">
            API トークンがありません
          </p>
        )}

        {/* Token list */}
        {tokens.length > 0 && (
          <div className="space-y-2">
            {tokens.map((token) => (
              <div
                key={token.id}
                data-testid={`api-token-row-${token.id}`}
                className="border border-gray-200 dark:border-gray-700 rounded-lg p-3"
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-sm font-medium text-gray-900 dark:text-white truncate">
                        {token.name}
                      </span>
                      <code className="text-xs font-mono text-gray-500 dark:text-gray-400 bg-gray-100 dark:bg-gray-700 px-1.5 py-0.5 rounded">
                        {token.token_prefix}
                      </code>
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
                      {scope === 'team' && token.created_by && (
                        <span>作成者: {token.created_by}</span>
                      )}
                      <span>作成: {formatDate(token.created_at)}</span>
                      <span>有効期限: {formatDate(token.expires_at)}</span>
                    </div>
                    {token.permissions && token.permissions.length > 0 && (
                      <div className="mt-1.5 flex flex-wrap gap-1">
                        {token.permissions.map((p) => (
                          <span
                            key={p}
                            className="px-1.5 py-0.5 text-xs bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 rounded"
                          >
                            {p}
                          </span>
                        ))}
                      </div>
                    )}
                  </div>
                  <button
                    type="button"
                    onClick={() => handleDeleteRequest(token)}
                    disabled={deleting && deleteTarget?.id === token.id}
                    className="flex-shrink-0 text-xs px-2 py-1 rounded border border-red-200 dark:border-red-700 text-red-500 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50 transition-colors"
                    aria-label={`トークン ${token.name} を削除`}
                  >
                    削除
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Create modal */}
      <ApiTokenCreateModal
        isOpen={showCreate}
        scope={scope}
        teamId={teamId}
        onClose={() => setShowCreate(false)}
        onCreated={handleCreated}
      />

      {/* One-time result modal (plaintext in memory only) */}
      <ApiTokenResultModal response={createdResponse} onClose={handleResultClose} />

      {/* Delete confirm modal */}
      <ApiTokenDeleteConfirmModal
        token={deleteTarget}
        isDeleting={deleting}
        error={deleteError}
        onConfirm={handleDeleteConfirm}
        onCancel={handleDeleteCancel}
      />
    </>
  )
}