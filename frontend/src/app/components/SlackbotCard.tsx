'use client'

import { SlackBot, SlackBotStatus } from '../../types/slackbot'
import SlackbotStatusBadge from './SlackbotStatusBadge'
import CopyableResourceId from './CopyableResourceId'

interface SlackbotCardProps {
  slackbot: SlackBot
  onEdit: (slackbot: SlackBot) => void
  onDelete: (slackbotId: string) => void
  onToggleStatus: (slackbotId: string, newStatus: SlackBotStatus) => void
  isDeleting?: boolean
  isTogglingStatus?: boolean
}

export default function SlackbotCard({
  slackbot,
  onEdit,
  onDelete,
  onToggleStatus,
  isDeleting = false,
  isTogglingStatus = false,
}: SlackbotCardProps) {
  const handleDelete = () => {
    if (confirm(`"${slackbot.name}" を削除してもよいですか？`)) {
      onDelete(slackbot.id)
    }
  }

  const handleToggleStatus = () => {
    const newStatus: SlackBotStatus = slackbot.status === 'active' ? 'paused' : 'active'
    onToggleStatus(slackbot.id, newStatus)
  }

  const formatDate = (dateStr: string) => {
    try {
      return new Date(dateStr).toLocaleString('ja-JP', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      })
    } catch {
      return dateStr
    }
  }

  return (
    <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-5 shadow-sm hover:shadow-md transition-shadow">
      {/* Header */}
      <div className="flex items-start justify-between mb-4">
        <div className="flex items-center gap-2 flex-1 min-w-0">
          {/* Slack icon */}
          <div className="flex-shrink-0 w-8 h-8 rounded-lg bg-purple-100 dark:bg-purple-900/30 flex items-center justify-center">
            <svg className="w-4 h-4 text-purple-600 dark:text-purple-400" fill="currentColor" viewBox="0 0 24 24">
              <path d="M6 15a2 2 0 0 1-2 2 2 2 0 0 1-2-2 2 2 0 0 1 2-2h2v2zm1 0a2 2 0 0 1 2-2 2 2 0 0 1 2 2v5a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-5zm2-8a2 2 0 0 1-2-2 2 2 0 0 1 2-2 2 2 0 0 1 2 2v2H9zm0 1a2 2 0 0 1 2 2 2 2 0 0 1-2 2H4a2 2 0 0 1-2-2 2 2 0 0 1 2-2h5zm8 2a2 2 0 0 1 2-2 2 2 0 0 1 2 2 2 2 0 0 1-2 2h-2v-2zm-1 0a2 2 0 0 1-2 2 2 2 0 0 1-2-2V5a2 2 0 0 1 2-2 2 2 0 0 1 2 2v5zm-2 8a2 2 0 0 1 2 2 2 2 0 0 1-2 2 2 2 0 0 1-2-2v-2h2zm0-1a2 2 0 0 1-2-2 2 2 0 0 1 2-2h5a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-5z" />
            </svg>
          </div>
          <h3 className="text-base font-semibold text-gray-900 dark:text-white truncate">
            {slackbot.name}
          </h3>
        </div>
        <SlackbotStatusBadge status={slackbot.status} />
      </div>

      <CopyableResourceId id={slackbot.id} className="mb-3" />

      {/* Allowed Channel Names */}
      {slackbot.allowed_channel_names && slackbot.allowed_channel_names.length > 0 && (
        <div className="mb-3">
          <div className="text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">許可チャンネル（部分一致）</div>
          <div className="flex flex-wrap gap-1">
            {slackbot.allowed_channel_names.slice(0, 3).map((channelName) => (
              <span
                key={channelName}
                className="inline-flex items-center px-2 py-0.5 rounded text-xs bg-purple-50 dark:bg-purple-900/20 text-purple-700 dark:text-purple-300 border border-purple-200 dark:border-purple-800"
              >
                {channelName}
              </span>
            ))}
            {slackbot.allowed_channel_names.length > 3 && (
              <span className="inline-flex items-center px-2 py-0.5 rounded text-xs bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400">
                +{slackbot.allowed_channel_names.length - 3}
              </span>
            )}
          </div>
        </div>
      )}

      {/* Max Sessions & Bot Token Secret */}
      <div className="flex items-center gap-4 mb-3 text-xs text-gray-500 dark:text-gray-400">
        <span>最大セッション: <strong className="text-gray-700 dark:text-gray-300">{slackbot.max_sessions ?? 10}</strong></span>
        {slackbot.bot_token_secret_name && (
          <span className="truncate">
            Secret: <strong className="text-gray-700 dark:text-gray-300 font-mono">{slackbot.bot_token_secret_name}</strong>
          </span>
        )}
      </div>

      {/* Initial message template */}
      {slackbot.session_config?.initial_message_template && (
        <div className="mb-3">
          <div className="text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">初期メッセージ</div>
          <p className="text-xs text-gray-700 dark:text-gray-300 line-clamp-2 bg-gray-50 dark:bg-gray-700/50 px-2 py-1.5 rounded">
            {slackbot.session_config.initial_message_template}
          </p>
        </div>
      )}

      {/* Timestamps */}
      <div className="text-xs text-gray-400 dark:text-gray-500 mt-3 mb-4">
        作成: {formatDate(slackbot.created_at)}
        {slackbot.updated_at !== slackbot.created_at && (
          <span className="ml-3">更新: {formatDate(slackbot.updated_at)}</span>
        )}
      </div>

      {/* Action Buttons */}
      <div className="flex items-center gap-2 pt-3 border-t border-gray-100 dark:border-gray-700">
        {/* Edit */}
        <button
          onClick={() => onEdit(slackbot)}
          className="flex-1 inline-flex items-center justify-center px-3 py-1.5 text-sm text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors"
        >
          <svg className="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
          </svg>
          編集
        </button>

        {/* Toggle Status */}
        <button
          onClick={handleToggleStatus}
          disabled={isTogglingStatus}
          className={`flex-1 inline-flex items-center justify-center px-3 py-1.5 text-sm rounded-md transition-colors disabled:opacity-50 ${
            slackbot.status === 'active'
              ? 'text-yellow-700 dark:text-yellow-300 bg-yellow-50 dark:bg-yellow-900/20 hover:bg-yellow-100 dark:hover:bg-yellow-900/40'
              : 'text-green-700 dark:text-green-300 bg-green-50 dark:bg-green-900/20 hover:bg-green-100 dark:hover:bg-green-900/40'
          }`}
        >
          {isTogglingStatus ? (
            <svg className="w-4 h-4 mr-1 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
          ) : slackbot.status === 'active' ? (
            <svg className="w-4 h-4 mr-1" fill="currentColor" viewBox="0 0 24 24">
              <path d="M6 19h4V5H6v14zm8-14v14h4V5h-4z" />
            </svg>
          ) : (
            <svg className="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
            </svg>
          )}
          {slackbot.status === 'active' ? '一時停止' : '再開'}
        </button>

        {/* Delete */}
        <button
          onClick={handleDelete}
          disabled={isDeleting}
          className="flex-1 inline-flex items-center justify-center px-3 py-1.5 text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-900/20 hover:bg-red-100 dark:hover:bg-red-900/40 rounded-md transition-colors disabled:opacity-50"
        >
          {isDeleting ? (
            <svg className="w-4 h-4 mr-1 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
          ) : (
            <svg className="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
          )}
          削除
        </button>
      </div>
    </div>
  )
}
