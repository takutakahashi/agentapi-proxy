'use client'

import { useState, useEffect } from 'react'
import { Schedule, CreateScheduleRequest, UpdateScheduleRequest, ScheduleSessionConfig, COMMON_TIMEZONES } from '../../types/schedule'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import CronExpressionInput from './CronExpressionInput'
import { OrganizationHistory } from '../../utils/organizationHistory'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import MemoryKeyInput, { MemoryKeyPair, memoryKeyPairsToRecord, recordToMemoryKeyPairs } from './MemoryKeyInput'
import SessionProfileSelect from './SessionProfileSelect'

interface ScheduleFormModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess: () => void
  editingSchedule?: Schedule | null
}

type ExecutionType = 'recurring' | 'onetime'

export default function ScheduleFormModal({
  isOpen,
  onClose,
  onSuccess,
  editingSchedule,
}: ScheduleFormModalProps) {
  const { getScopeParams } = useTeamScope()
  const [name, setName] = useState('')
  const [executionType, setExecutionType] = useState<ExecutionType>('recurring')
  const [cronExpr, setCronExpr] = useState('')
  const [scheduledAt, setScheduledAt] = useState('')
  const [timezone, setTimezone] = useState('Asia/Tokyo')
  const [message, setMessage] = useState('')
  const [repository, setRepository] = useState('')
  const [repositorySuggestions, setRepositorySuggestions] = useState<string[]>([])
  const [showRepositorySuggestions, setShowRepositorySuggestions] = useState(false)

  const [sessionProfileId, setSessionProfileId] = useState('')
  const [oneshot, setOneshot] = useState(false)
  const [reuseSession, setReuseSession] = useState(false)
  const [reuseMessage, setReuseMessage] = useState('')
  const [memoryKeyPairs, setMemoryKeyPairs] = useState<MemoryKeyPair[]>([{ key: '', value: '' }])
  const [showCustomMemory, setShowCustomMemory] = useState(false)
  const [envVarPairs, setEnvVarPairs] = useState<MemoryKeyPair[]>([{ key: '', value: '' }])
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const isEditing = !!editingSchedule

  // Initialize form when editing
  useEffect(() => {
    if (editingSchedule) {
      setName(editingSchedule.name)
      setTimezone(editingSchedule.timezone || 'Asia/Tokyo')
      setMessage(editingSchedule.session_config?.params?.message || '')
      setRepository(editingSchedule.session_config?.tags?.repository || '')
      setSessionProfileId(editingSchedule.session_config?.session_profile_id || '')
      setOneshot(editingSchedule.session_config?.params?.oneshot === true)
      setReuseSession(editingSchedule.session_config?.reuse_session === true)
      setReuseMessage(editingSchedule.session_config?.reuse_message || '')
      const loadedPairs = recordToMemoryKeyPairs(editingSchedule.session_config?.memory_key as Record<string, string> | undefined)
      setMemoryKeyPairs(loadedPairs)
      // カスタム判定: presetに合致しないがキーがある場合はカスタム表示
      const mk = editingSchedule.session_config?.memory_key as Record<string, string> | undefined
      const isKnownPreset = !mk || Object.keys(mk).length === 0
        || (Object.keys(mk).length === 1 && mk['schedule_id'] === '{{ .schedule_id }}')
      setShowCustomMemory(!isKnownPreset)
      setEnvVarPairs(recordToMemoryKeyPairs(editingSchedule.session_config?.environment))

      if (editingSchedule.cron_expr) {
        setExecutionType('recurring')
        setCronExpr(editingSchedule.cron_expr)
        setScheduledAt('')
      } else if (editingSchedule.scheduled_at) {
        setExecutionType('onetime')
        setScheduledAt(formatDateTimeLocal(editingSchedule.scheduled_at))
        setCronExpr('')
      }
    } else {
      resetForm()
    }
  }, [editingSchedule, isOpen])

  // ESC key to close modal
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isOpen) {
        handleClose()
      }
    }

    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown)
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isOpen])

  const resetForm = () => {
    setName('')
    setExecutionType('recurring')
    setCronExpr('')
    setScheduledAt('')
    setTimezone('Asia/Tokyo')
    setMessage('')
    setRepository('')
    setSessionProfileId('')
    setOneshot(false)
    setReuseSession(false)
    setReuseMessage('')
    setMemoryKeyPairs([{ key: '', value: '' }])
    setShowCustomMemory(false)
    setEnvVarPairs([{ key: '', value: '' }])
    setError(null)
  }

  const handleClose = () => {
    resetForm()
    onClose()
  }

  const formatDateTimeLocal = (isoString: string): string => {
    try {
      const date = new Date(isoString)
      const year = date.getFullYear()
      const month = String(date.getMonth() + 1).padStart(2, '0')
      const day = String(date.getDate()).padStart(2, '0')
      const hours = String(date.getHours()).padStart(2, '0')
      const minutes = String(date.getMinutes()).padStart(2, '0')
      return `${year}-${month}-${day}T${hours}:${minutes}`
    } catch {
      return ''
    }
  }

  const handleRepositoryChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const value = e.target.value
    setRepository(value)

    if (value.trim()) {
      const suggestions = OrganizationHistory.getRepositorySuggestions(value)
      setRepositorySuggestions(suggestions)
      setShowRepositorySuggestions(suggestions.length > 0)
    } else {
      setShowRepositorySuggestions(false)
    }
  }

  const handleRepositoryFocus = () => {
    const suggestions = OrganizationHistory.getRepositorySuggestions()
    setRepositorySuggestions(suggestions)
    setShowRepositorySuggestions(suggestions.length > 0)
  }

  const handleRepositoryBlur = () => {
    setTimeout(() => setShowRepositorySuggestions(false), 150)
  }

  const selectRepositorySuggestion = (suggestion: string) => {
    setRepository(suggestion)
    setShowRepositorySuggestions(false)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!name.trim()) {
      setError('スケジュール名を入力してください')
      return
    }

    if (executionType === 'recurring' && !cronExpr.trim()) {
      setError('cron式を入力してください')
      return
    }

    if (executionType === 'onetime' && !scheduledAt) {
      setError('実行日時を選択してください')
      return
    }

    try {
      setIsSubmitting(true)
      setError(null)

      const client = createAgentAPIProxyClientFromStorage()

      // Get scope parameters from context
      const scopeParams = getScopeParams()

      const sessionParams: ScheduleSessionConfig['params'] = {}
      if (message.trim()) sessionParams.message = message.trim()
      if (oneshot) sessionParams.oneshot = true
      const hasParams = Object.keys(sessionParams).length > 0

      const environment = memoryKeyPairsToRecord(envVarPairs)

      const scheduleData: CreateScheduleRequest = {
        name: name.trim(),
        timezone,
        session_config: {
          params: hasParams ? sessionParams : undefined,
          tags: repository.trim() ? { repository: repository.trim() } : undefined,
          reuse_session: reuseSession || undefined,
          reuse_message: (reuseSession && reuseMessage.trim()) ? reuseMessage.trim() : undefined,
          environment: environment || undefined,
          session_profile_id: sessionProfileId.trim() || undefined,
        },
        ...scopeParams,
      }

      if (executionType === 'recurring') {
        scheduleData.cron_expr = cronExpr.trim()
      } else {
        scheduleData.scheduled_at = new Date(scheduledAt).toISOString()
      }

      // Build memory_key from pairs (Go template values like {{ .schedule_id }} are resolved server-side at runtime)
      const memoryKey = memoryKeyPairsToRecord(memoryKeyPairs)
      if (memoryKey) {
        scheduleData.session_config = {
          ...scheduleData.session_config,
          memory_key: memoryKey,
        }
      }

      if (isEditing && editingSchedule) {
        const updateData: UpdateScheduleRequest = {
          ...scheduleData,
          // 完了済みスケジュールを更新する場合はアクティブ状態に戻す
          ...(editingSchedule.status === 'completed' ? { status: 'active' } : {}),
        }
        await client.updateSchedule(editingSchedule.id, updateData)
      } else {
        await client.createSchedule(scheduleData)
      }

      onSuccess()
      handleClose()
    } catch (err) {
      console.error('Failed to save schedule:', err)
      setError(err instanceof Error ? err.message : 'スケジュールの保存に失敗しました')
    } finally {
      setIsSubmitting(false)
    }
  }

  if (!isOpen) return null

  return (
    <div
      className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50"
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          handleClose()
        }
      }}
    >
      <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-lg mx-4 max-h-[90vh] overflow-y-auto">
        <div className="flex justify-between items-center mb-6">
          <h2 className="text-xl font-semibold text-gray-900 dark:text-white">
            {isEditing ? 'スケジュールを編集' : '新しいスケジュール'}
          </h2>
          <button
            onClick={handleClose}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
          >
            <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Completed schedule notice */}
          {isEditing && editingSchedule?.status === 'completed' && (
            <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md p-3">
              <div className="flex items-start gap-2">
                <svg className="w-4 h-4 text-blue-600 dark:text-blue-400 flex-shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <p className="text-sm text-blue-700 dark:text-blue-300">
                  完了済みのスケジュールです。更新すると再度アクティブ状態になり、設定した日時に実行されます。
                </p>
              </div>
            </div>
          )}

          {/* Schedule Name */}
          <div>
            <label htmlFor="name" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              スケジュール名 *
            </label>
            <input
              id="name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="例: 日次レポート生成"
              disabled={isSubmitting}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
              required
            />
          </div>

          {/* Execution Type */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              実行タイプ
            </label>
            <div className="flex space-x-4">
              <label className="flex items-center">
                <input
                  type="radio"
                  name="executionType"
                  value="recurring"
                  checked={executionType === 'recurring'}
                  onChange={() => setExecutionType('recurring')}
                  className="mr-2 text-blue-600"
                  disabled={isSubmitting}
                />
                <span className="text-sm text-gray-700 dark:text-gray-300">
                  繰り返し
                </span>
              </label>
              <label className="flex items-center">
                <input
                  type="radio"
                  name="executionType"
                  value="onetime"
                  checked={executionType === 'onetime'}
                  onChange={() => setExecutionType('onetime')}
                  className="mr-2 text-blue-600"
                  disabled={isSubmitting}
                />
                <span className="text-sm text-gray-700 dark:text-gray-300">
                  一回限り
                </span>
              </label>
            </div>
          </div>

          {/* Cron Expression or DateTime */}
          {executionType === 'recurring' ? (
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                cron式 *
              </label>
              <CronExpressionInput
                value={cronExpr}
                onChange={setCronExpr}
                disabled={isSubmitting}
              />
            </div>
          ) : (
            <div>
              <label htmlFor="scheduledAt" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                実行日時 *
              </label>
              <input
                id="scheduledAt"
                type="datetime-local"
                value={scheduledAt}
                onChange={(e) => setScheduledAt(e.target.value)}
                disabled={isSubmitting}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                required
              />
            </div>
          )}

          {/* Timezone */}
          <div>
            <label htmlFor="timezone" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              タイムゾーン
            </label>
            <select
              id="timezone"
              value={timezone}
              onChange={(e) => setTimezone(e.target.value)}
              disabled={isSubmitting}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
            >
              {COMMON_TIMEZONES.map((tz) => (
                <option key={tz.value} value={tz.value}>
                  {tz.label}
                </option>
              ))}
            </select>
          </div>

          {/* Initial Message */}
          <div>
            <label htmlFor="message" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              初期メッセージ
            </label>
            <textarea
              id="message"
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              placeholder="セッション開始時に送信するメッセージ..."
              disabled={isSubmitting}
              rows={3}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white resize-y"
            />
          </div>

          {/* Repository */}
          <div className="relative">
            <label htmlFor="repository" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              リポジトリ
            </label>
            <input
              id="repository"
              type="text"
              value={repository}
              onChange={handleRepositoryChange}
              onFocus={handleRepositoryFocus}
              onBlur={handleRepositoryBlur}
              placeholder="例: owner/repository-name"
              disabled={isSubmitting}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
            />

            {/* Repository Suggestions */}
            {showRepositorySuggestions && repositorySuggestions.length > 0 && (
              <div className="absolute z-10 mt-1 w-full bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md shadow-lg max-h-40 overflow-y-auto">
                {repositorySuggestions.map((suggestion, index) => (
                  <button
                    key={index}
                    type="button"
                    onClick={() => selectRepositorySuggestion(suggestion)}
                    className="w-full text-left px-3 py-2 text-sm hover:bg-gray-50 dark:hover:bg-gray-700 text-gray-900 dark:text-white font-mono"
                  >
                    {suggestion}
                  </button>
                ))}
              </div>
            )}

            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              owner/repository-name の形式で入力してください
            </p>
          </div>

          {/* Oneshot Option */}
          <div className="flex items-start gap-3">
            <input
              type="checkbox"
              id="oneshot"
              checked={oneshot}
              onChange={(e) => setOneshot(e.target.checked)}
              className="mt-0.5 h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              disabled={isSubmitting}
            />
            <div>
              <label htmlFor="oneshot" className="block text-sm font-medium text-gray-700 dark:text-gray-300 cursor-pointer">
                エージェントが終了したら自動的に削除する
              </label>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                セッションが停止した後、自動的にセッションを削除します。
              </p>
            </div>
          </div>

          {/* Session Reuse Option */}
          <div className="space-y-3">
            <div className="flex items-start gap-3">
              <input
                type="checkbox"
                id="reuseSession"
                checked={reuseSession}
                onChange={(e) => setReuseSession(e.target.checked)}
                className="mt-0.5 h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                disabled={isSubmitting}
              />
              <div>
                <label htmlFor="reuseSession" className="block text-sm font-medium text-gray-700 dark:text-gray-300 cursor-pointer">
                  実行中のセッションを再利用する
                </label>
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                  スケジュール実行時に既存のアクティブなセッションが見つかった場合、新しいセッションを作成する代わりにそのセッションにメッセージを送信します。
                </p>
              </div>
            </div>
            {reuseSession && (
              <div className="ml-7">
                <label htmlFor="reuseMessage" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  再利用時のメッセージ
                  <span className="ml-1 text-xs text-gray-400 dark:text-gray-500 font-normal">（省略時は初期メッセージを使用）</span>
                </label>
                <textarea
                  id="reuseMessage"
                  value={reuseMessage}
                  onChange={(e) => setReuseMessage(e.target.value)}
                  placeholder="既存セッションに送信するメッセージ（省略可）..."
                  disabled={isSubmitting}
                  rows={2}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white resize-y text-sm"
                />
              </div>
            )}
          </div>

          {/* Memory Key */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              セッション間の記憶を有効化する
            </label>
            <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
              スケジュールを繰り返し実行するとき、前回のセッションの記憶を引き継ぐことができます。
            </p>
            {/* Preset + Custom buttons */}
            <div className="flex flex-wrap gap-1.5">
              {/* このスケジュールで共有 */}
              <button
                type="button"
                onClick={() => { setMemoryKeyPairs([{ key: 'schedule_id', value: '{{ .schedule_id }}' }]); setShowCustomMemory(false) }}
                disabled={isSubmitting}
                className={`px-2.5 py-1 text-xs rounded-md border transition-colors disabled:opacity-50 ${
                  !showCustomMemory && memoryKeyPairs.some(p => p.key === 'schedule_id')
                    ? 'bg-blue-50 dark:bg-blue-900/30 border-blue-400 dark:border-blue-500 text-blue-700 dark:text-blue-300 font-medium'
                    : 'border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-700'
                }`}
                title="このスケジュールの実行ごとに同じ記憶を引き継ぎます"
              >
                このスケジュールで共有
              </button>
              {/* カスタム */}
              <button
                type="button"
                onClick={() => setShowCustomMemory(true)}
                disabled={isSubmitting}
                className={`px-2.5 py-1 text-xs rounded-md border transition-colors disabled:opacity-50 ${
                  showCustomMemory
                    ? 'bg-blue-50 dark:bg-blue-900/30 border-blue-400 dark:border-blue-500 text-blue-700 dark:text-blue-300 font-medium'
                    : 'border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-700'
                }`}
              >
                カスタム
              </button>
              {/* 無効 */}
              <button
                type="button"
                onClick={() => { setMemoryKeyPairs([{ key: '', value: '' }]); setShowCustomMemory(false) }}
                disabled={isSubmitting}
                className={`px-2.5 py-1 text-xs rounded-md border transition-colors disabled:opacity-50 ${
                  !showCustomMemory && memoryKeyPairs.every(p => !p.key)
                    ? 'bg-gray-100 dark:bg-gray-600 border-gray-400 dark:border-gray-500 text-gray-700 dark:text-gray-200 font-medium'
                    : 'border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-700'
                }`}
              >
                無効
              </button>
            </div>
            {/* カスタム入力欄 */}
            {showCustomMemory && (
              <div className="mt-3">
                <MemoryKeyInput
                  pairs={memoryKeyPairs}
                  onChange={setMemoryKeyPairs}
                  disabled={isSubmitting}
                  helpText='記憶を識別するキーと値を入力します。Goテンプレート形式も使用できます（例: {{ .schedule_id }}）'
                />
              </div>
            )}
          </div>

          {/* Environment Variables */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              環境変数
            </label>
            <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
              セッション実行時に渡す環境変数を設定します。
            </p>
            <MemoryKeyInput
              pairs={envVarPairs}
              onChange={setEnvVarPairs}
              disabled={isSubmitting}
              helpText="KEY=VALUE 形式で環境変数を入力してください"
            />
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

          {/* Error */}
          {error && (
            <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md p-3">
              <p className="text-red-600 dark:text-red-400 text-sm">{error}</p>
            </div>
          )}

          {/* Actions */}
          <div className="flex justify-end space-x-3 pt-4">
            <button
              type="button"
              onClick={handleClose}
              disabled={isSubmitting}
              className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md disabled:opacity-50"
            >
              キャンセル
            </button>
            <button
              type="submit"
              disabled={isSubmitting}
              className="px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 disabled:bg-gray-400 disabled:cursor-not-allowed rounded-md transition-colors"
            >
              {isSubmitting ? (
                <>
                  <svg className="animate-spin -ml-1 mr-2 h-4 w-4 text-white inline" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                  </svg>
                  保存中...
                </>
              ) : (
                isEditing ? '更新' : '作成'
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
