'use client'

import { useState, useEffect, useCallback } from 'react'
import {
  SlackBot,
  CreateSlackBotRequest,
  UpdateSlackBotRequest,
} from '../../types/slackbot'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import MemoryKeyInput, { MemoryKeyPair, memoryKeyPairsToRecord, recordToMemoryKeyPairs } from './MemoryKeyInput'
import SessionProfileSelect from './SessionProfileSelect'

interface SlackbotFormModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess: () => void
  editingSlackbot?: SlackBot | null
}

type KeyValuePair = { key: string; value: string }

export default function SlackbotFormModal({
  isOpen,
  onClose,
  onSuccess,
  editingSlackbot,
}: SlackbotFormModalProps) {
  const { getScopeParams } = useTeamScope()

  // Basic fields
  const [name, setName] = useState('')
  const [botToken, setBotToken] = useState('')
  const [appToken, setAppToken] = useState('')
  const [maxSessions, setMaxSessions] = useState<number>(10)
  const [notifyOnSessionCreated, setNotifyOnSessionCreated] = useState<boolean>(true)
  const [allowBotMessages, setAllowBotMessages] = useState<boolean>(false)

  // Allowed channel names (chip/tag input, partial match)
  const [allowedChannelNames, setAllowedChannelNames] = useState<string[]>([])
  const [channelNameInput, setChannelNameInput] = useState('')

  // Allowed user IDs (chip/tag input, exact match)
  const [allowedUserIDs, setAllowedUserIDs] = useState<string[]>([])
  const [userIDInput, setUserIDInput] = useState('')

  // Message templates
  const [initialMessageTemplate, setInitialMessageTemplate] = useState('{{ .event.text }}')
  const [reuseMessageTemplate, setReuseMessageTemplate] = useState('{{ .event.text }}')

  // Session config
  const [envPairs, setEnvPairs] = useState<KeyValuePair[]>([{ key: '', value: '' }])
  const [tagPairs, setTagPairs] = useState<KeyValuePair[]>([{ key: '', value: '' }])
  const [repoFullName, setRepoFullName] = useState('')

  // Memory key
  const [memoryKeyPairs, setMemoryKeyPairs] = useState<MemoryKeyPair[]>([{ key: '', value: '' }])
  const [showCustomMemory, setShowCustomMemory] = useState(false)
  const [sessionProfileId, setSessionProfileId] = useState('')

  // UI state
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showBotTokenSection, setShowBotTokenSection] = useState(false)
  const [showAdvanced, setShowAdvanced] = useState(false)

  const isEditing = !!editingSlackbot

  // Initialize form when editing
  useEffect(() => {
    if (editingSlackbot) {
      setName(editingSlackbot.name)
      setMaxSessions(editingSlackbot.max_sessions ?? 10)
      setNotifyOnSessionCreated(editingSlackbot.notify_on_session_created ?? true)
      setAllowBotMessages(editingSlackbot.allow_bot_messages ?? false)
      setAllowedChannelNames(editingSlackbot.allowed_channel_names || [])
      setAllowedUserIDs(editingSlackbot.allowed_user_ids || [])

      const sc = editingSlackbot.session_config
      setSessionProfileId(sc?.session_profile_id || '')
      setInitialMessageTemplate(sc?.initial_message_template ?? '{{ .event.text }}')
      setReuseMessageTemplate(sc?.reuse_message_template ?? '{{ .event.text }}')

      if (sc?.environment && Object.keys(sc.environment).length > 0) {
        setEnvPairs(Object.entries(sc.environment).map(([key, value]) => ({ key, value })))
        setShowAdvanced(true)
      } else {
        setEnvPairs([{ key: '', value: '' }])
      }

      if (sc?.tags && Object.keys(sc.tags).length > 0) {
        setTagPairs(Object.entries(sc.tags).map(([key, value]) => ({ key, value })))
        setShowAdvanced(true)
      } else {
        setTagPairs([{ key: '', value: '' }])
      }

      // Restore repo_full_name from session params
      if (sc?.params?.repo_full_name) {
        setRepoFullName(sc.params.repo_full_name)
        setShowAdvanced(true)
      } else {
        setRepoFullName('')
      }

      // Restore memory key pairs from existing memory_key
      if (sc?.memory_key && Object.keys(sc.memory_key).length > 0) {
        const mk = sc.memory_key as Record<string, string>
        setMemoryKeyPairs(recordToMemoryKeyPairs(mk))
        setShowAdvanced(true)
        // カスタム判定: 既知のプリセットに合致しない場合はカスタム表示
        const knownPresets = [
          { bot_id: '{{ .bot_id }}' },
          { channel: '{{ .event.channel }}' },
          { user: '{{ .event.user }}' },
        ]
        const isKnown = knownPresets.some(preset =>
          Object.keys(preset).length === Object.keys(mk).length &&
          Object.entries(preset).every(([k, v]) => mk[k] === v)
        )
        setShowCustomMemory(!isKnown)
      } else {
        setMemoryKeyPairs([{ key: '', value: '' }])
        setShowCustomMemory(false)
      }

      // Auto-expand sections if data exists
      const hasBotToken = !!editingSlackbot.bot_token_secret_name
      setShowBotTokenSection(hasBotToken)
      if (hasBotToken) {
        setShowAdvanced(true)
      }
    } else {
      // Reset form
      setName('')
      setBotToken('')
      setAppToken('')
      setMaxSessions(10)
      setNotifyOnSessionCreated(true)
      setAllowBotMessages(false)
      setAllowedChannelNames([])
      setChannelNameInput('')
      setAllowedUserIDs([])
      setUserIDInput('')
      setInitialMessageTemplate('{{ .event.text }}')
      setReuseMessageTemplate('{{ .event.text }}')
      setEnvPairs([{ key: '', value: '' }])
      setTagPairs([{ key: '', value: '' }])
      setMemoryKeyPairs([{ key: '', value: '' }])
      setShowCustomMemory(false)
      setShowBotTokenSection(false)
      setShowAdvanced(false)
      setRepoFullName('')
      setSessionProfileId('')
    }
    setError(null)
  }, [editingSlackbot, isOpen])

  // Keyboard handler
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    },
    [onClose]
  )

  useEffect(() => {
    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown)
    }
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, handleKeyDown])

  // Channel name chip input (partial match)
  const addChannelName = () => {
    const trimmed = channelNameInput.trim()
    if (trimmed && !allowedChannelNames.includes(trimmed)) {
      setAllowedChannelNames((prev) => [...prev, trimmed])
    }
    setChannelNameInput('')
  }

  const handleChannelNameKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      addChannelName()
    }
  }

  const removeChannelName = (name: string) => {
    setAllowedChannelNames((prev) => prev.filter((c) => c !== name))
  }

  // User ID chip input (exact match)
  const addUserID = () => {
    const trimmed = userIDInput.trim()
    if (trimmed && !allowedUserIDs.includes(trimmed)) {
      setAllowedUserIDs((prev) => [...prev, trimmed])
    }
    setUserIDInput('')
  }

  const handleUserIDKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      addUserID()
    }
  }

  const removeUserID = (id: string) => {
    setAllowedUserIDs((prev) => prev.filter((u) => u !== id))
  }

  // Key-value pair helpers
  const updatePair = (
    pairs: KeyValuePair[],
    setPairs: React.Dispatch<React.SetStateAction<KeyValuePair[]>>,
    index: number,
    field: 'key' | 'value',
    val: string
  ) => {
    const next = [...pairs]
    next[index] = { ...next[index], [field]: val }
    setPairs(next)
  }

  const addPair = (setPairs: React.Dispatch<React.SetStateAction<KeyValuePair[]>>) => {
    setPairs((prev) => [...prev, { key: '', value: '' }])
  }

  const removePair = (
    pairs: KeyValuePair[],
    setPairs: React.Dispatch<React.SetStateAction<KeyValuePair[]>>,
    index: number
  ) => {
    if (pairs.length === 1) {
      setPairs([{ key: '', value: '' }])
    } else {
      setPairs((prev) => prev.filter((_, i) => i !== index))
    }
  }

  const pairsToRecord = (pairs: KeyValuePair[]): Record<string, string> => {
    const record: Record<string, string> = {}
    pairs.forEach(({ key, value }) => {
      if (key.trim()) record[key.trim()] = value
    })
    return record
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)

    // Validation
    if (!name.trim()) {
      setError('名前は必須です')
      return
    }
    if (allowedChannelNames.length === 0) {
      setError('チャンネル名を1つ以上指定してください')
      return
    }
    setIsSubmitting(true)
    try {
      const client = createAgentAPIProxyClientFromStorage()

      const environment = pairsToRecord(envPairs)
      const tags = pairsToRecord(tagPairs)

      // Build memory_key from pairs (Go template values like {{ .bot_id }} are resolved server-side at runtime)
      const memoryKey = memoryKeyPairsToRecord(memoryKeyPairs)

      // Build session params (only include non-empty values)
      const sessionParams = {
        ...(repoFullName.trim() ? { repo_full_name: repoFullName.trim() } : {}),
      }

      const sessionConfig = {
        ...(initialMessageTemplate.trim() ? { initial_message_template: initialMessageTemplate.trim() } : {}),
        ...(reuseMessageTemplate.trim() ? { reuse_message_template: reuseMessageTemplate.trim() } : {}),
        ...(Object.keys(environment).length > 0 ? { environment } : {}),
        ...(Object.keys(tags).length > 0 ? { tags } : {}),
        ...(Object.keys(sessionParams).length > 0 ? { params: sessionParams } : {}),
        ...(sessionProfileId.trim() ? { session_profile_id: sessionProfileId.trim() } : {}),
      }

      if (isEditing && editingSlackbot) {
        // When showBotTokenSection is false, explicitly send empty string to clear the custom token.
        // When true, omit bot_token_secret_name — the backend auto-sets it from the bot ID
        // when bot_token/app_token are provided.
        const updateData: UpdateSlackBotRequest = {
          name: name.trim(),
          ...(botToken.trim() ? { bot_token: botToken.trim() } : {}),
          ...(appToken.trim() ? { app_token: appToken.trim() } : {}),
          ...(!showBotTokenSection ? { bot_token_secret_name: '', bot_token_secret_key: '' } : {}),
          allowed_channel_names: allowedChannelNames,
          allowed_user_ids: allowedUserIDs,
          max_sessions: maxSessions,
          notify_on_session_created: notifyOnSessionCreated,
          allow_bot_messages: allowBotMessages,
          session_config: {
            ...sessionConfig,
            ...(memoryKey ? { memory_key: memoryKey } : {}),
          },
        }
        await client.updateSlackBot(editingSlackbot.id, updateData)
      } else {
        const scopeParams = getScopeParams()
        const createData: CreateSlackBotRequest = {
          name: name.trim(),
          ...(botToken.trim() ? { bot_token: botToken.trim() } : {}),
          ...(appToken.trim() ? { app_token: appToken.trim() } : {}),
          allowed_channel_names: allowedChannelNames,
          ...(allowedUserIDs.length > 0 ? { allowed_user_ids: allowedUserIDs } : {}),
          max_sessions: maxSessions,
          notify_on_session_created: notifyOnSessionCreated,
          allow_bot_messages: allowBotMessages,
          ...(Object.keys(sessionConfig).length > 0 ? { session_config: sessionConfig } : {}),
          ...scopeParams,
        }
        await client.createSlackBot(createData)
      }

      onSuccess()
    } catch (err) {
      console.error('Failed to save slackbot:', err)
      setError(err instanceof Error ? err.message : 'Slackbotの保存に失敗しました')
    } finally {
      setIsSubmitting(false)
    }
  }

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-50 overflow-y-auto">
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black bg-opacity-50 transition-opacity"
        onClick={onClose}
      />

      {/* Modal */}
      <div className="relative min-h-full flex items-center justify-center p-4">
        <div
          className="relative bg-white dark:bg-gray-800 rounded-xl shadow-2xl w-full max-w-2xl"
          onClick={(e) => e.stopPropagation()}
        >
          {/* Header */}
          <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
            <div className="flex items-center gap-2">
              <div className="w-7 h-7 rounded-lg bg-purple-100 dark:bg-purple-900/30 flex items-center justify-center">
                <svg className="w-4 h-4 text-purple-600 dark:text-purple-400" fill="currentColor" viewBox="0 0 24 24">
                  <path d="M6 15a2 2 0 0 1-2 2 2 2 0 0 1-2-2 2 2 0 0 1 2-2h2v2zm1 0a2 2 0 0 1 2-2 2 2 0 0 1 2 2v5a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-5zm2-8a2 2 0 0 1-2-2 2 2 0 0 1 2-2 2 2 0 0 1 2 2v2H9zm0 1a2 2 0 0 1 2 2 2 2 0 0 1-2 2H4a2 2 0 0 1-2-2 2 2 0 0 1 2-2h5zm8 2a2 2 0 0 1 2-2 2 2 0 0 1 2 2 2 2 0 0 1-2 2h-2v-2zm-1 0a2 2 0 0 1-2 2 2 2 0 0 1-2-2V5a2 2 0 0 1 2-2 2 2 0 0 1 2 2v5zm-2 8a2 2 0 0 1 2 2 2 2 0 0 1-2 2 2 2 0 0 1-2-2v-2h2zm0-1a2 2 0 0 1-2-2 2 2 0 0 1 2-2h5a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-5z" />
                </svg>
              </div>
              <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
                {isEditing ? 'Slackbot を編集' : '新しいSlackbot'}
              </h2>
            </div>
            <button
              onClick={onClose}
              className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>

          {/* Form */}
          <form onSubmit={handleSubmit} className="px-6 py-5 space-y-5 max-h-[70vh] overflow-y-auto">
            {/* Error */}
            {error && (
              <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
                <p className="text-sm text-red-700 dark:text-red-400">{error}</p>
              </div>
            )}

            {/* Name */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                名前 <span className="text-red-500">*</span>
              </label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="例: my-slackbot"
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 dark:focus:ring-blue-400"
                required
              />
            </div>

            {/* Allowed Channel Names */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                チャンネル名フィルタ <span className="text-red-500">*</span>
              </label>
              <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
                処理するチャンネル名を1つ以上指定してください。チャンネル名の部分一致で絞り込みます。Enter またはカンマで追加
              </p>
              <div className="flex flex-wrap gap-1.5 mb-2">
                {allowedChannelNames.map((name) => (
                  <span
                    key={name}
                    className="inline-flex items-center px-2 py-0.5 rounded text-xs bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300"
                  >
                    {name}
                    <button
                      type="button"
                      onClick={() => removeChannelName(name)}
                      className="ml-1 text-purple-500 hover:text-purple-700 dark:hover:text-purple-200"
                    >
                      ×
                    </button>
                  </span>
                ))}
              </div>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={channelNameInput}
                  onChange={(e) => setChannelNameInput(e.target.value)}
                  onKeyDown={handleChannelNameKeyDown}
                  onBlur={addChannelName}
                  placeholder="例: dev-alerts, backend"
                  className={`flex-1 px-3 py-2 border rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 ${
                    allowedChannelNames.length === 0
                      ? 'border-red-400 dark:border-red-500'
                      : 'border-gray-300 dark:border-gray-600'
                  }`}
                />
                <button
                  type="button"
                  onClick={addChannelName}
                  className="px-3 py-2 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-lg transition-colors"
                >
                  追加
                </button>
              </div>
            </div>

            {/* Allowed User IDs */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                発話ユーザー制限
              </label>
              <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
                発話を許可する Slack ユーザー ID を指定します（例: U012AB3CD）。空の場合は全ユーザーを許可します。Enter またはカンマで追加
              </p>
              <div className="flex flex-wrap gap-1.5 mb-2">
                {allowedUserIDs.map((id) => (
                  <span
                    key={id}
                    className="inline-flex items-center px-2 py-0.5 rounded text-xs bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 font-mono"
                  >
                    {id}
                    <button
                      type="button"
                      onClick={() => removeUserID(id)}
                      className="ml-1 text-green-500 hover:text-green-700 dark:hover:text-green-200"
                    >
                      ×
                    </button>
                  </span>
                ))}
              </div>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={userIDInput}
                  onChange={(e) => setUserIDInput(e.target.value)}
                  onKeyDown={handleUserIDKeyDown}
                  onBlur={addUserID}
                  placeholder="例: U012AB3CD"
                  className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 font-mono"
                />
                <button
                  type="button"
                  onClick={addUserID}
                  className="px-3 py-2 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-lg transition-colors"
                >
                  追加
                </button>
              </div>
            </div>

            {/* Custom Slack App Checkbox */}
            <div>
              <label className="flex items-start gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={showBotTokenSection}
                  onChange={(e) => setShowBotTokenSection(e.target.checked)}
                  className="mt-0.5 h-4 w-4 rounded border-gray-300 dark:border-gray-600 text-blue-600 focus:ring-blue-500 cursor-pointer"
                />
                <div>
                  <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
                    独自の Slack App を利用する
                  </span>
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                    通常は必要ありません。独自の Slack App Token を設定する場合のみチェックしてください。
                  </p>
                </div>
              </label>

              {showBotTokenSection && (
                <div className="mt-4 space-y-4 pl-4 border-l-2 border-blue-200 dark:border-blue-700">
                  {/* Bot Token */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      Bot Token
                    </label>
                    {isEditing && (
                      <p className="text-xs text-yellow-600 dark:text-yellow-400 mb-1">
                        変更する場合のみ入力してください。空欄の場合は現在の値が保持されます。
                      </p>
                    )}
                    <input
                      type="password"
                      value={botToken}
                      onChange={(e) => setBotToken(e.target.value)}
                      placeholder={isEditing ? '変更する場合のみ入力 (xoxb-...)' : 'xoxb-...'}
                      autoComplete="new-password"
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 dark:focus:ring-blue-400 font-mono"
                    />
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      Slack Bot Token（xoxb- で始まるトークン）。レスポンスには返りません。
                    </p>
                  </div>

                  {/* App Token */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      App-Level Token
                    </label>
                    {isEditing && (
                      <p className="text-xs text-yellow-600 dark:text-yellow-400 mb-1">
                        変更する場合のみ入力してください。空欄の場合は現在の値が保持されます。
                      </p>
                    )}
                    <input
                      type="password"
                      value={appToken}
                      onChange={(e) => setAppToken(e.target.value)}
                      placeholder={isEditing ? '変更する場合のみ入力 (xapp-...)' : 'xapp-...'}
                      autoComplete="new-password"
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 dark:focus:ring-blue-400 font-mono"
                    />
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      Slack App-Level Token（xapp- で始まるトークン）。レスポンスには返りません。
                    </p>
                  </div>
                </div>
              )}
            </div>

            {/* Advanced Section */}
            <div>
              <button
                type="button"
                onClick={() => setShowAdvanced(!showAdvanced)}
                className="flex items-center gap-1 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200"
              >
                <svg
                  className={`w-4 h-4 transition-transform ${showAdvanced ? 'rotate-90' : ''}`}
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                </svg>
                詳細設定
              </button>

              {showAdvanced && (
                <div className="mt-4 space-y-5 pl-4 border-l-2 border-gray-200 dark:border-gray-700">
                  {/* Initial Message Template */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      初期メッセージテンプレート
                    </label>
                    <textarea
                      value={initialMessageTemplate}
                      onChange={(e) => setInitialMessageTemplate(e.target.value)}
                      placeholder="例: {{ .event.text }}"
                      rows={2}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 font-mono resize-y"
                    />
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      新規セッション作成時に送信されるメッセージ。Goテンプレート形式。例: <code className="bg-gray-100 dark:bg-gray-700 px-1 rounded">{'{{ .event.text }}'}</code>
                    </p>
                  </div>

                  {/* Reuse Message Template */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      再利用メッセージテンプレート
                    </label>
                    <textarea
                      value={reuseMessageTemplate}
                      onChange={(e) => setReuseMessageTemplate(e.target.value)}
                      placeholder="例: {{ .event.text }}"
                      rows={2}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 font-mono resize-y"
                    />
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      既存セッションを再利用する際に送信されるメッセージ。Goテンプレート形式。例: <code className="bg-gray-100 dark:bg-gray-700 px-1 rounded">{'{{ .event.text }}'}</code>
                    </p>
                  </div>

                  {/* Repository */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      初期リポジトリ
                    </label>
                    <input
                      type="text"
                      value={repoFullName}
                      onChange={(e) => setRepoFullName(e.target.value)}
                      placeholder="例: myorg/myrepo"
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 font-mono"
                    />
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      セッション起動時に自動でクローンする GitHub リポジトリ（<code className="bg-gray-100 dark:bg-gray-700 px-1 rounded">org/repo</code> 形式）。設定するとメッセージ内のリポジトリ自動検出より優先されます。
                    </p>
                  </div>

                  {/* Notify on session created */}
                  <div className="flex items-center justify-between">
                    <div>
                      <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                        セッション作成通知
                      </label>
                      <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                        セッション作成時に Slack へ URL 通知を投稿する
                      </p>
                    </div>
                    <button
                      type="button"
                      role="switch"
                      aria-checked={notifyOnSessionCreated}
                      onClick={() => setNotifyOnSessionCreated((prev) => !prev)}
                      className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 ${
                        notifyOnSessionCreated
                          ? 'bg-blue-600'
                          : 'bg-gray-300 dark:bg-gray-600'
                      }`}
                    >
                      <span
                        className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${
                          notifyOnSessionCreated ? 'translate-x-6' : 'translate-x-1'
                        }`}
                      />
                    </button>
                  </div>

                  {/* Allow bot messages */}
                  <div className="flex items-center justify-between">
                    <div>
                      <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                        ボットからのメンションに反応する
                      </label>
                      <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                        有効にすると、他のボットからのメンションにも反応する（デフォルト: 無効）
                      </p>
                    </div>
                    <button
                      type="button"
                      role="switch"
                      aria-checked={allowBotMessages}
                      onClick={() => setAllowBotMessages((prev) => !prev)}
                      className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 ${
                        allowBotMessages
                          ? 'bg-blue-600'
                          : 'bg-gray-300 dark:bg-gray-600'
                      }`}
                    >
                      <span
                        className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${
                          allowBotMessages ? 'translate-x-6' : 'translate-x-1'
                        }`}
                      />
                    </button>
                  </div>

                  {/* Max Sessions */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      最大セッション数
                    </label>
                    <input
                      type="number"
                      min={1}
                      max={100}
                      value={maxSessions}
                      onChange={(e) => setMaxSessions(Number(e.target.value))}
                      className="w-32 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                    />
                  </div>

                  {/* Environment Variables */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                      環境変数
                    </label>
                    <div className="space-y-2">
                      {envPairs.map((pair, idx) => (
                        <div key={idx} className="flex items-center gap-2">
                          <input
                            type="text"
                            value={pair.key}
                            onChange={(e) => updatePair(envPairs, setEnvPairs, idx, 'key', e.target.value)}
                            placeholder="KEY"
                            className="flex-1 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500"
                          />
                          <span className="text-gray-400 dark:text-gray-500 text-xs">=</span>
                          <input
                            type="text"
                            value={pair.value}
                            onChange={(e) => updatePair(envPairs, setEnvPairs, idx, 'value', e.target.value)}
                            placeholder="value"
                            className="flex-1 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500"
                          />
                          <button
                            type="button"
                            onClick={() => removePair(envPairs, setEnvPairs, idx)}
                            className="p-1 text-gray-400 hover:text-red-500 dark:hover:text-red-400"
                          >
                            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                            </svg>
                          </button>
                        </div>
                      ))}
                      <button
                        type="button"
                        onClick={() => addPair(setEnvPairs)}
                        className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1"
                      >
                        <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                        </svg>
                        追加
                      </button>
                    </div>
                  </div>

                  {/* Memory Key */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      セッション間の記憶を有効化する
                    </label>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
                      Slackのメッセージをきっかけに起動するセッション間で、記憶を引き継ぐ範囲を指定します。指定しない場合は記憶を引き継ぎません。
                    </p>
                    {/* Template preset buttons */}
                    <div className="flex flex-wrap gap-1.5">
                      <button
                        type="button"
                        onClick={() => { setMemoryKeyPairs([{ key: 'bot_id', value: '{{ .bot_id }}' }]); setShowCustomMemory(false) }}
                        className={`px-2.5 py-1 text-xs rounded-md border transition-colors ${!showCustomMemory && memoryKeyPairs.length === 1 && memoryKeyPairs[0].key === 'bot_id' && memoryKeyPairs[0].value === '{{ .bot_id }}' ? 'border-purple-500 bg-purple-50 dark:bg-purple-900/20 text-purple-700 dark:text-purple-300' : 'border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-700'}`}
                        title="このBotの全セッションで記憶を共有します"
                      >
                        Bot 全体で共有
                      </button>
                      <button
                        type="button"
                        onClick={() => { setMemoryKeyPairs([{ key: 'channel', value: '{{ .event.channel }}' }]); setShowCustomMemory(false) }}
                        className={`px-2.5 py-1 text-xs rounded-md border transition-colors ${!showCustomMemory && memoryKeyPairs.length === 1 && memoryKeyPairs[0].key === 'channel' && memoryKeyPairs[0].value === '{{ .event.channel }}' ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-300' : 'border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-700'}`}
                        title="同じチャンネルのセッション間で記憶を共有します"
                      >
                        チャンネルごとに分ける
                      </button>
                      <button
                        type="button"
                        onClick={() => { setMemoryKeyPairs([{ key: 'user', value: '{{ .event.user }}' }]); setShowCustomMemory(false) }}
                        className={`px-2.5 py-1 text-xs rounded-md border transition-colors ${!showCustomMemory && memoryKeyPairs.length === 1 && memoryKeyPairs[0].key === 'user' && memoryKeyPairs[0].value === '{{ .event.user }}' ? 'border-green-500 bg-green-50 dark:bg-green-900/20 text-green-700 dark:text-green-300' : 'border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-700'}`}
                        title="同じユーザーのセッション間で記憶を共有します"
                      >
                        ユーザーごとに分ける
                      </button>
                      <button
                        type="button"
                        onClick={() => setShowCustomMemory(true)}
                        className={`px-2.5 py-1 text-xs rounded-md border transition-colors ${showCustomMemory ? 'border-orange-500 bg-orange-50 dark:bg-orange-900/20 text-orange-700 dark:text-orange-300' : 'border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-700'}`}
                      >
                        カスタム
                      </button>
                      <button
                        type="button"
                        onClick={() => { setMemoryKeyPairs([{ key: '', value: '' }]); setShowCustomMemory(false) }}
                        className={`px-2.5 py-1 text-xs rounded-md border transition-colors ${!showCustomMemory && memoryKeyPairs.every(p => !p.key) ? 'border-gray-500 bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300' : 'border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-700'}`}
                      >
                        無効
                      </button>
                    </div>
                    {showCustomMemory && (
                      <div className="mt-3">
                        <MemoryKeyInput
                          pairs={memoryKeyPairs}
                          onChange={setMemoryKeyPairs}
                          disabled={isSubmitting}
                          helpText='記憶を識別するキーと値を入力します。Goテンプレート形式も使用できます（例: {{ .event.channel }} でチャンネルID、{{ .bot_id }} でBot ID）'
                        />
                      </div>
                    )}
                  </div>

                  {/* Session Profile */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                      セッションプロファイル
                    </label>
                    <SessionProfileSelect
                      value={sessionProfileId}
                      onChange={setSessionProfileId}
                      disabled={isSubmitting}
                    />
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      プロファイルを選択すると、環境変数・タグ・テンプレートなどの設定を適用します
                    </p>
                  </div>

                  {/* Tags */}
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                      タグ
                    </label>
                    <div className="space-y-2">
                      {tagPairs.map((pair, idx) => (
                        <div key={idx} className="flex items-center gap-2">
                          <input
                            type="text"
                            value={pair.key}
                            onChange={(e) => updatePair(tagPairs, setTagPairs, idx, 'key', e.target.value)}
                            placeholder="key"
                            className="flex-1 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500"
                          />
                          <span className="text-gray-400 dark:text-gray-500 text-xs">=</span>
                          <input
                            type="text"
                            value={pair.value}
                            onChange={(e) => updatePair(tagPairs, setTagPairs, idx, 'value', e.target.value)}
                            placeholder="value"
                            className="flex-1 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono focus:outline-none focus:ring-1 focus:ring-blue-500"
                          />
                          <button
                            type="button"
                            onClick={() => removePair(tagPairs, setTagPairs, idx)}
                            className="p-1 text-gray-400 hover:text-red-500 dark:hover:text-red-400"
                          >
                            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                            </svg>
                          </button>
                        </div>
                      ))}
                      <button
                        type="button"
                        onClick={() => addPair(setTagPairs)}
                        className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1"
                      >
                        <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                        </svg>
                        追加
                      </button>
                    </div>
                  </div>
                </div>
              )}
            </div>
          </form>

          {/* Footer */}
          <div className="flex items-center justify-end gap-3 px-6 py-4 border-t border-gray-200 dark:border-gray-700">
            <button
              type="button"
              onClick={onClose}
              disabled={isSubmitting}
              className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-600 transition-colors disabled:opacity-50"
            >
              キャンセル
            </button>
            <button
              type="submit"
              form="slackbot-form"
              onClick={handleSubmit}
              disabled={isSubmitting}
              className="px-4 py-2 text-sm text-white bg-blue-600 hover:bg-blue-700 rounded-lg transition-colors disabled:opacity-50 flex items-center gap-2"
            >
              {isSubmitting && (
                <svg className="w-4 h-4 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                </svg>
              )}
              {isEditing ? '保存' : '作成'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
