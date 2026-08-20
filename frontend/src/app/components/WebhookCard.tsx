'use client'

import { useState } from 'react'
import Link from 'next/link'
import { Webhook } from '../../types/webhook'
import WebhookStatusBadge from './WebhookStatusBadge'
import CopyableResourceId from './CopyableResourceId'

interface WebhookCardProps {
  webhook: Webhook
  onEdit: (webhook: Webhook) => void
  onDelete: (webhookId: string) => void
  onTogglePause: (webhookId: string, currentStatus: Webhook['status']) => void
  onRegenerateSecret: (webhookId: string) => void
}

function formatDateTime(isoString?: string): string {
  if (!isoString) return '-'
  try {
    const date = new Date(isoString)
    return date.toLocaleString('ja-JP', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    })
  } catch {
    return isoString
  }
}

export default function WebhookCard({
  webhook,
  onEdit,
  onDelete,
  onTogglePause,
  onRegenerateSecret,
}: WebhookCardProps) {
  const [isDeleting, setIsDeleting] = useState(false)
  const [isRegenerating, setIsRegenerating] = useState(false)
  const [copiedUrl, setCopiedUrl] = useState(false)
  const [copiedSecret, setCopiedSecret] = useState(false)

  const handleDelete = async () => {
    if (!confirm(`Webhook「${webhook.name}」を削除してもよろしいですか？`)) {
      return
    }
    setIsDeleting(true)
    try {
      await onDelete(webhook.id)
    } finally {
      setIsDeleting(false)
    }
  }

  const handleRegenerateSecret = async () => {
    if (!confirm('シークレットを再生成すると、既存のシークレットは無効になります。続行しますか？')) {
      return
    }
    setIsRegenerating(true)
    try {
      await onRegenerateSecret(webhook.id)
    } finally {
      setIsRegenerating(false)
    }
  }

  const handleCopyWebhookUrl = async () => {
    if (webhook.webhook_url) {
      try {
        await navigator.clipboard.writeText(webhook.webhook_url)
        setCopiedUrl(true)
        setTimeout(() => setCopiedUrl(false), 2000)
      } catch (err) {
        console.error('Failed to copy webhook URL:', err)
      }
    }
  }

  const handleCopySecret = async () => {
    if (webhook.secret) {
      try {
        await navigator.clipboard.writeText(webhook.secret)
        setCopiedSecret(true)
        setTimeout(() => setCopiedSecret(false), 2000)
      } catch (err) {
        console.error('Failed to copy secret:', err)
      }
    }
  }

  const isPaused = webhook.status === 'paused'

  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-4 hover:shadow-md transition-shadow">
      {/* Header */}
      <div className="flex items-start justify-between mb-3">
        <div className="flex-1 min-w-0">
          <h3 className="text-lg font-medium text-gray-900 dark:text-white truncate">
            {webhook.name}
          </h3>
          <CopyableResourceId id={webhook.id} className="mt-1" />
          <div className="flex items-center gap-2 mt-1">
            <WebhookStatusBadge status={webhook.status} />
            <span className="text-xs px-2 py-0.5 bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-400 rounded-full">
              {webhook.type === 'github' ? 'GitHub' : 'Custom'}
            </span>
          </div>
        </div>
      </div>

      {/* Webhook Info */}
      <div className="space-y-2 text-sm">
        {/* Webhook URL & Secret - Copy buttons */}
        <div className="flex items-center gap-2">
          {webhook.webhook_url && (
            <button
              onClick={handleCopyWebhookUrl}
              className={`flex items-center gap-1 px-3 py-1.5 text-sm rounded transition-colors ${
                copiedUrl
                  ? 'text-green-600 dark:text-green-400 bg-green-50 dark:bg-green-900/20'
                  : 'text-gray-600 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20'
              }`}
              title="URLをコピー"
            >
              {copiedUrl ? (
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                </svg>
              ) : (
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m2 4H10m0 0l3-3m-3 3l3 3" />
                </svg>
              )}
              {copiedUrl ? 'コピー済み' : 'URL をコピー'}
            </button>
          )}

          {webhook.secret && (
            <button
              onClick={handleCopySecret}
              className={`flex items-center gap-1 px-3 py-1.5 text-sm rounded transition-colors ${
                copiedSecret
                  ? 'text-green-600 dark:text-green-400 bg-green-50 dark:bg-green-900/20'
                  : 'text-gray-600 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20'
              }`}
              title="Secretをコピー"
            >
              {copiedSecret ? (
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                </svg>
              ) : (
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m2 4H10m0 0l3-3m-3 3l3 3" />
                </svg>
              )}
              {copiedSecret ? 'コピー済み' : 'Secretをコピー'}
            </button>
          )}
        </div>

        {/* GitHub Events */}
        {webhook.github?.allowed_events && webhook.github.allowed_events.length > 0 && (
          <div className="flex items-start gap-2 text-gray-600 dark:text-gray-400">
            <svg className="w-4 h-4 flex-shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
            </svg>
            <div className="flex flex-wrap gap-1">
              {webhook.github.allowed_events.map((event) => (
                <span
                  key={event}
                  className="px-1.5 py-0.5 bg-gray-100 dark:bg-gray-700 text-xs rounded"
                >
                  {event}
                </span>
              ))}
            </div>
          </div>
        )}

        {/* Allowed Repositories */}
        {webhook.github?.allowed_repositories && webhook.github.allowed_repositories.length > 0 && (
          <div className="flex items-start gap-2 text-gray-600 dark:text-gray-400">
            <svg className="w-4 h-4 flex-shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
            </svg>
            <div className="flex flex-wrap gap-1">
              {webhook.github.allowed_repositories.map((repo) => (
                <span
                  key={repo}
                  className="px-1.5 py-0.5 bg-gray-100 dark:bg-gray-700 text-xs rounded font-mono"
                >
                  {repo}
                </span>
              ))}
            </div>
          </div>
        )}

        {/* Triggers count */}
        {webhook.triggers && webhook.triggers.length > 0 && (
          <div className="flex items-center gap-2 text-gray-600 dark:text-gray-400">
            <svg className="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
            <span>トリガー: {webhook.triggers.length}件</span>
          </div>
        )}

        {/* Delivery Count */}
        {webhook.delivery_count !== undefined && webhook.delivery_count > 0 && (
          <div className="flex items-center gap-2 text-gray-600 dark:text-gray-400">
            <svg className="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" />
            </svg>
            <span>配信回数: {webhook.delivery_count}</span>
          </div>
        )}

        {/* Last Delivery */}
        {webhook.last_delivery && (
          <div className="flex items-center gap-2 text-gray-600 dark:text-gray-400">
            <svg className="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span>最終配信: {formatDateTime(webhook.last_delivery.received_at)}</span>
            <span className={`text-xs px-1.5 py-0.5 rounded ${
              webhook.last_delivery.status === 'processed'
                ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
                : webhook.last_delivery.status === 'skipped'
                ? 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-400'
                : 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
            }`}>
              {webhook.last_delivery.status}
            </span>
          </div>
        )}
      </div>

      {/* Actions */}
      <div className="flex items-center gap-2 mt-4 pt-3 border-t border-gray-100 dark:border-gray-700 flex-wrap">
        <Link
          href={`/chats?tag=webhook_id:${webhook.id}`}
          className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20 rounded transition-colors"
          title="このWebhookが作成したセッション一覧を表示"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
          </svg>
          セッション一覧
        </Link>

        <Link
          href={`/webhooks/${webhook.id}`}
          className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20 rounded transition-colors"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4" />
          </svg>
          テスト
        </Link>

        <button
          onClick={() => onEdit(webhook)}
          className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20 rounded transition-colors"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
          </svg>
          編集
        </button>

        <button
          onClick={() => onTogglePause(webhook.id, webhook.status)}
          className={`flex items-center gap-1 px-3 py-1.5 text-sm rounded transition-colors ${
            isPaused
              ? 'text-green-600 dark:text-green-400 hover:bg-green-50 dark:hover:bg-green-900/20'
              : 'text-yellow-600 dark:text-yellow-400 hover:bg-yellow-50 dark:hover:bg-yellow-900/20'
          }`}
        >
          {isPaused ? (
            <>
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              再開
            </>
          ) : (
            <>
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              一時停止
            </>
          )}
        </button>

        <button
          onClick={handleRegenerateSecret}
          disabled={isRegenerating}
          className="flex items-center gap-1 px-3 py-1.5 text-sm text-purple-600 dark:text-purple-400 hover:bg-purple-50 dark:hover:bg-purple-900/20 rounded transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {isRegenerating ? (
            <>
              <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              再生成中...
            </>
          ) : (
            <>
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
              Secret再生成
            </>
          )}
        </button>

        <button
          onClick={handleDelete}
          disabled={isDeleting}
          className="flex items-center gap-1 px-3 py-1.5 text-sm text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 rounded transition-colors disabled:opacity-50 ml-auto"
        >
          {isDeleting ? (
            <>
              <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              削除中...
            </>
          ) : (
            <>
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
              削除
            </>
          )}
        </button>
      </div>
    </div>
  )
}
