'use client'

import { useState, useEffect, useCallback, useRef } from 'react'
import { Webhook, WebhookStatus, WebhookType } from '../../types/webhook'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import WebhookCard from './WebhookCard'
import { useTeamScope } from '../../contexts/TeamScopeContext'

interface WebhookListViewProps {
  statusFilter: WebhookStatus | null
  typeFilter: WebhookType | null
  onWebhookEdit: (webhook: Webhook) => void
  onWebhooksUpdate?: () => void
}

export default function WebhookListView({
  statusFilter,
  typeFilter,
  onWebhookEdit,
  onWebhooksUpdate,
}: WebhookListViewProps) {
  const { getScopeParams } = useTeamScope()
  const [webhooks, setWebhooks] = useState<Webhook[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const fetchIdRef = useRef(0)

  const fetchWebhooks = useCallback(async () => {
    const fetchId = ++fetchIdRef.current
    try {
      setLoading(true)
      setError(null)

      // Get scope parameters for filtering
      const scopeParams = getScopeParams()

      const client = createAgentAPIProxyClientFromStorage()
      const response = await client.getWebhooks({
        ...(statusFilter ? { status: statusFilter } : {}),
        ...(typeFilter ? { type: typeFilter } : {}),
        ...scopeParams,
      })

      if (fetchId !== fetchIdRef.current) return
      setWebhooks(response.webhooks || [])
    } catch (err) {
      if (fetchId !== fetchIdRef.current) return
      console.error('Failed to fetch webhooks:', err)
      setError(err instanceof Error ? err.message : 'Webhookの取得に失敗しました')
    } finally {
      if (fetchId === fetchIdRef.current) {
        setLoading(false)
      }
    }
  }, [statusFilter, typeFilter, getScopeParams])

  // Refetch webhooks when team scope changes
  useEffect(() => {
    fetchWebhooks()
  }, [fetchWebhooks])

  const handleDelete = async (webhookId: string) => {
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.deleteWebhook(webhookId)
      setWebhooks((prev) => prev.filter((w) => w.id !== webhookId))
      onWebhooksUpdate?.()
    } catch (err) {
      console.error('Failed to delete webhook:', err)
      alert(err instanceof Error ? err.message : 'Webhookの削除に失敗しました')
    }
  }

  const handleTogglePause = async (webhookId: string, currentStatus: WebhookStatus) => {
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const newStatus: WebhookStatus = currentStatus === 'paused' ? 'active' : 'paused'
      const updated = await client.updateWebhook(webhookId, { status: newStatus })
      setWebhooks((prev) =>
        prev.map((w) => (w.id === webhookId ? updated : w))
      )
      onWebhooksUpdate?.()
    } catch (err) {
      console.error('Failed to toggle webhook pause:', err)
      alert(err instanceof Error ? err.message : 'Webhookの状態変更に失敗しました')
    }
  }

  const handleRegenerateSecret = async (webhookId: string) => {
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.regenerateWebhookSecret(webhookId)
      // Refresh to get updated webhook data
      await fetchWebhooks()
      onWebhooksUpdate?.()
    } catch (err) {
      console.error('Failed to regenerate webhook secret:', err)
      alert(err instanceof Error ? err.message : 'シークレットの再生成に失敗しました')
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <svg className="animate-spin h-8 w-8 text-blue-600" fill="none" viewBox="0 0 24 24">
          <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
          <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
      </div>
    )
  }

  if (error) {
    return (
      <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-4">
        <div className="flex items-center gap-2">
          <svg className="w-5 h-5 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span className="text-red-700 dark:text-red-300">{error}</span>
        </div>
        <button
          onClick={fetchWebhooks}
          className="mt-3 text-sm text-red-600 dark:text-red-400 hover:underline"
        >
          再試行
        </button>
      </div>
    )
  }

  if (webhooks.length === 0) {
    return (
      <div className="text-center py-12">
        <svg className="w-16 h-16 mx-auto text-gray-300 dark:text-gray-600 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
        </svg>
        <h3 className="text-lg font-medium text-gray-900 dark:text-white mb-2">
          Webhookがありません
        </h3>
        <p className="text-gray-500 dark:text-gray-400">
          {statusFilter || typeFilter
            ? 'フィルタ条件に一致するWebhookはありません。'
            : '新しいWebhookを作成してください。'}
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
          Webhook一覧 ({webhooks.length})
        </h2>
        <button
          onClick={fetchWebhooks}
          className="text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white flex items-center gap-1"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          更新
        </button>
      </div>

      {/* Webhook Cards */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-1 xl:grid-cols-2">
        {webhooks.map((webhook) => (
          <WebhookCard
            key={webhook.id}
            webhook={webhook}
            onEdit={onWebhookEdit}
            onDelete={handleDelete}
            onTogglePause={handleTogglePause}
            onRegenerateSecret={handleRegenerateSecret}
          />
        ))}
      </div>
    </div>
  )
}
