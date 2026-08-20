'use client'

import { useState, useEffect } from 'react'
import {
  Webhook,
  CreateWebhookRequest,
  WebhookTrigger,
  WebhookSessionConfig,
  GitHubConditions,
  WebhookType,
  WebhookSignatureType,
  GITHUB_EVENTS,
  GITHUB_ACTIONS,
} from '../../types/webhook'
import { createAgentAPIProxyClientFromStorage, AgentAPIProxyError } from '../../lib/agentapi-proxy-client'
import { OrganizationHistory } from '../../utils/organizationHistory'
import { useTeamScope } from '../../contexts/TeamScopeContext'
import SessionProfileSelect from './SessionProfileSelect'

interface WebhookFormModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess: () => void
  editingWebhook?: Webhook | null
}

interface TriggerFormData {
  id?: string
  name: string
  enabled: boolean
  events: string[]
  actions: string[]
  baseBranches: string[]
  initialMessageTemplate: string
  reuseMessageTemplate?: string
  reuseSession: boolean
  mountPayload: boolean
  oneshot: boolean
  credentialSource: '' | 'session_user' | 'triggered_user' | 'team' | 'none'
  goTemplate?: string
  environment: Record<string, string>
  tags: Record<string, string>
  showAdvanced: boolean
  sessionProfileId: string
}

const emptyTrigger: TriggerFormData = {
  name: '',
  enabled: true,
  events: [],
  actions: [],
  baseBranches: [],
  initialMessageTemplate: '',
  reuseMessageTemplate: '',
  reuseSession: false,
  mountPayload: false,
  oneshot: false,
  credentialSource: '',
  goTemplate: '',
  environment: {},
  tags: {},
  showAdvanced: false,
  sessionProfileId: '',
}

export default function WebhookFormModal({
  isOpen,
  onClose,
  onSuccess,
  editingWebhook,
}: WebhookFormModalProps) {
  const { getScopeParams } = useTeamScope()
  const [name, setName] = useState('')
  const [webhookType, setWebhookType] = useState<WebhookType>('github')
  const [secret, setSecret] = useState('')
  const [signatureHeader, setSignatureHeader] = useState('X-Signature')
  const [signatureType, setSignatureType] = useState<WebhookSignatureType>('hmac')
  const [signaturePrefix, setSignaturePrefix] = useState('')
  const [allowedEvents, setAllowedEvents] = useState<string[]>([])
  const [allowedRepositories, setAllowedRepositories] = useState('')
  const [triggers, setTriggers] = useState<TriggerFormData[]>([{ ...emptyTrigger }])
  const [repositorySuggestions, setRepositorySuggestions] = useState<string[]>([])
  const [showRepositorySuggestions, setShowRepositorySuggestions] = useState(false)

  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [expandedTrigger, setExpandedTrigger] = useState<number | null>(0)

  const isEditing = !!editingWebhook

  // Initialize form when editing
  useEffect(() => {
    if (editingWebhook) {
      setName(editingWebhook.name)
      setWebhookType(editingWebhook.type)
      setSecret('')
      setSignatureHeader(editingWebhook.signature_header || 'X-Signature')
      setSignatureType(editingWebhook.signature_type || 'hmac')
      setSignaturePrefix(editingWebhook.signature_prefix || '')
      setAllowedEvents(editingWebhook.github?.allowed_events || [])
      setAllowedRepositories(editingWebhook.github?.allowed_repositories?.join(', ') || '')

      if (editingWebhook.triggers && editingWebhook.triggers.length > 0) {
        setTriggers(
          editingWebhook.triggers.map((t) => ({
            id: t.id,
            name: t.name,
            enabled: t.enabled ?? true,
            events: t.conditions.github?.events || [],
            actions: t.conditions.github?.actions || [],
            baseBranches: t.conditions.github?.base_branches || [],
            initialMessageTemplate: t.session_config?.initial_message_template || '',
            reuseMessageTemplate: t.session_config?.reuse_message_template || '',
            reuseSession: t.session_config?.reuse_session ?? false,
            mountPayload: t.session_config?.mount_payload ?? false,
            oneshot: t.session_config?.params?.oneshot ?? false,
            credentialSource: t.session_config?.params?.credential_source || '',
            goTemplate: t.conditions.go_template || '',
            environment: t.session_config?.environment || {},
            tags: t.session_config?.tags || {},
            showAdvanced: false,
            sessionProfileId: t.session_config?.session_profile_id || '',
          }))
        )
      } else {
        setTriggers([{ ...emptyTrigger }])
      }
    } else {
      resetForm()
    }
  }, [editingWebhook, isOpen])

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
    setWebhookType('github')
    setSecret('')
    setSignatureHeader('X-Signature')
    setSignatureType('hmac')
    setSignaturePrefix('')
    setAllowedEvents([])
    setAllowedRepositories('')
    setTriggers([{ ...emptyTrigger }])
    setError(null)
    setExpandedTrigger(0)
  }

  const handleClose = () => {
    resetForm()
    onClose()
  }

  const handleRepositoryChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const value = e.target.value
    setAllowedRepositories(value)

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
    const current = allowedRepositories.trim()
    if (current) {
      setAllowedRepositories(current + ', ' + suggestion)
    } else {
      setAllowedRepositories(suggestion)
    }
    setShowRepositorySuggestions(false)
  }

  const toggleAllowedEvent = (event: string) => {
    setAllowedEvents((prev) =>
      prev.includes(event) ? prev.filter((e) => e !== event) : [...prev, event]
    )
  }

  const addTrigger = () => {
    setTriggers((prev) => [...prev, { ...emptyTrigger }])
    setExpandedTrigger(triggers.length)
  }

  const removeTrigger = (index: number) => {
    if (triggers.length <= 1) return
    setTriggers((prev) => prev.filter((_, i) => i !== index))
    if (expandedTrigger === index) {
      setExpandedTrigger(null)
    } else if (expandedTrigger !== null && expandedTrigger > index) {
      setExpandedTrigger(expandedTrigger - 1)
    }
  }

  const updateTrigger = (index: number, field: keyof TriggerFormData, value: unknown) => {
    setTriggers((prev) =>
      prev.map((t, i) => (i === index ? { ...t, [field]: value } : t))
    )
  }

  const updateCredentialSource = (index: number, credentialSource: TriggerFormData['credentialSource']) => {
    setTriggers((prev) =>
      prev.map((trigger, i) => {
        if (i !== index) return trigger
        if (credentialSource !== 'triggered_user' || trigger.tags.username) {
          return { ...trigger, credentialSource }
        }
        return {
          ...trigger,
          credentialSource,
          tags: {
            ...trigger.tags,
            username: webhookType === 'github' ? '{{.sender.login}}' : '{{.user.login}}',
          },
        }
      })
    )
  }

  const toggleTriggerEvent = (index: number, event: string) => {
    const trigger = triggers[index]
    const newEvents = trigger.events.includes(event)
      ? trigger.events.filter((e) => e !== event)
      : [...trigger.events, event]
    updateTrigger(index, 'events', newEvents)
  }

  const toggleTriggerAction = (index: number, action: string) => {
    const trigger = triggers[index]
    const newActions = trigger.actions.includes(action)
      ? trigger.actions.filter((a) => a !== action)
      : [...trigger.actions, action]
    updateTrigger(index, 'actions', newActions)
  }

  const getAvailableActions = (events: string[]): { label: string; value: string }[] => {
    const allActions: { label: string; value: string }[] = []
    const seen = new Set<string>()

    for (const event of events) {
      const actions = GITHUB_ACTIONS[event as keyof typeof GITHUB_ACTIONS]
      if (actions) {
        for (const action of actions) {
          if (!seen.has(action.value)) {
            seen.add(action.value)
            allActions.push(action)
          }
        }
      }
    }

    return allActions
  }

  // Helper function to add key-value pair
  const addKeyValuePair = (index: number, field: 'environment' | 'tags') => {
    const trigger = triggers[index]
    const newKey = `key_${Object.keys(trigger[field]).length + 1}`
    updateTrigger(index, field, { ...trigger[field], [newKey]: '' })
  }

  // Helper function to update key-value pair
  const updateKeyValuePair = (
    index: number,
    field: 'environment' | 'tags',
    oldKey: string,
    newKey: string,
    newValue: string
  ) => {
    const trigger = triggers[index]
    const updated = Object.fromEntries(
      Object.entries(trigger[field]).map(([key, value]) => (
        key === oldKey ? [newKey, newValue] : [key, value]
      ))
    )

    updateTrigger(index, field, updated)
  }

  // Helper function to remove key-value pair
  const removeKeyValuePair = (index: number, field: 'environment' | 'tags', key: string) => {
    const trigger = triggers[index]
    const updated = { ...trigger[field] }
    delete updated[key]
    updateTrigger(index, field, updated)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!name.trim()) {
      setError('Webhook名を入力してください')
      return
    }

    const validTriggers = triggers.filter((t) => t.name.trim())
    if (validTriggers.length === 0) {
      setError('少なくとも1つのトリガーを設定してください')
      return
    }

    try {
      setIsSubmitting(true)
      setError(null)

      const client = createAgentAPIProxyClientFromStorage()

      // Get scope parameters from context
      const scopeParams = getScopeParams()

      // Parse allowed repositories
      const repoList = allowedRepositories
        .split(',')
        .map((r) => r.trim())
        .filter((r) => r)

      const webhookTriggers: WebhookTrigger[] = validTriggers.map((t) => {
        const conditions: {
          github?: GitHubConditions
          go_template?: string
        } = {}

        // GitHub conditions
        if (webhookType === 'github') {
          conditions.github = {}
          if (t.events.length > 0) {
            conditions.github.events = t.events
          }
          if (t.actions.length > 0) {
            conditions.github.actions = t.actions
          }
          if (t.baseBranches.length > 0) {
            conditions.github.base_branches = t.baseBranches
          }
          // Go template for GitHub webhooks
          if (t.goTemplate?.trim()) {
            conditions.go_template = t.goTemplate.trim()
          }
        }

        // Custom webhook conditions
        if (webhookType === 'custom') {
          if (t.goTemplate?.trim()) {
            conditions.go_template = t.goTemplate.trim()
          }
        }

        // Build session_config
        const session_config: WebhookSessionConfig = {}
        if (t.initialMessageTemplate.trim()) {
          session_config.initial_message_template = t.initialMessageTemplate.trim()
        }
        if (t.reuseMessageTemplate?.trim()) {
          session_config.reuse_message_template = t.reuseMessageTemplate.trim()
        }
        // Always set reuse_session explicitly (true or false)
        session_config.reuse_session = t.reuseSession
        // Always set mount_payload explicitly (true or false)
        session_config.mount_payload = t.mountPayload

        // Add environment variables if present
        if (Object.keys(t.environment).length > 0) {
          session_config.environment = t.environment
        }

        // Add tags if present
        if (Object.keys(t.tags).length > 0) {
          session_config.tags = t.tags
        }

        // Add session profile if present
        if (t.sessionProfileId.trim()) {
          session_config.session_profile_id = t.sessionProfileId.trim()
        }

        // Build params
        if (!session_config.params) {
          session_config.params = {}
        }
        // Always set oneshot explicitly (true or false)
        session_config.params.oneshot = t.oneshot
        if (t.credentialSource) {
          session_config.params.credential_source = t.credentialSource
        }

        return {
          id: t.id,
          name: t.name.trim(),
          enabled: t.enabled,
          conditions,
          session_config: Object.keys(session_config).length > 0 ? session_config : undefined,
        }
      })

      const webhookData: CreateWebhookRequest = {
        name: name.trim(),
        type: webhookType,
        secret: secret.trim() || undefined,
        signature_header: signatureHeader.trim() || undefined,
        signature_type: signatureType,
        signature_prefix: signaturePrefix.trim() || undefined,
        github:
          webhookType === 'github'
            ? {
                allowed_events: allowedEvents.length > 0 ? allowedEvents : undefined,
                allowed_repositories: repoList.length > 0 ? repoList : undefined,
              }
            : undefined,
        triggers: webhookTriggers,
        ...scopeParams,
      }

      if (isEditing && editingWebhook) {
        await client.updateWebhook(editingWebhook.id, webhookData)
      } else {
        await client.createWebhook(webhookData)
      }

      onSuccess()
      handleClose()
    } catch (err) {
      console.error('Failed to save webhook:', err)

      // Handle AgentAPIProxyError with detailed error information
      if (err instanceof AgentAPIProxyError) {
        // Extract detailed error message
        let errorMessage = err.message

        // Add status code for context
        if (err.status === 400) {
          errorMessage = `入力エラー: ${errorMessage}`
        } else if (err.status === 403) {
          errorMessage = `権限エラー: ${errorMessage}`
        } else if (err.status === 500) {
          errorMessage = `サーバーエラー: ${errorMessage}`
        }

        setError(errorMessage)
      } else if (err instanceof Error) {
        setError(err.message)
      } else {
        setError('Webhookの保存に失敗しました')
      }
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
      <div className="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-2xl mx-4 max-h-[90vh] overflow-y-auto">
        <div className="flex justify-between items-center mb-6">
          <h2 className="text-xl font-semibold text-gray-900 dark:text-white">
            {isEditing ? 'Webhookを編集' : '新しいWebhook'}
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

        <form onSubmit={handleSubmit} className="space-y-6">
          {/* Basic Info */}
          <div className="space-y-4">
            <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 border-b border-gray-200 dark:border-gray-700 pb-2">
              基本情報
            </h3>

            {/* Webhook Name */}
            <div>
              <label htmlFor="name" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Webhook名 *
              </label>
              <input
                id="name"
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="例: PR Review Bot"
                disabled={isSubmitting}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                required
              />
            </div>

            {/* Webhook Type */}
            <div>
              <label htmlFor="webhookType" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Webhookタイプ *
              </label>
              <select
                id="webhookType"
                value={webhookType}
                onChange={(e) => setWebhookType(e.target.value as WebhookType)}
                disabled={isSubmitting || isEditing}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white disabled:opacity-50 disabled:cursor-not-allowed"
                required
              >
                <option value="github">GitHub</option>
                <option value="custom">Custom</option>
              </select>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                {isEditing ? 'タイプは編集時に変更できません' : 'GitHubまたはカスタムwebhookを選択'}
              </p>
            </div>

            {/* Signature Header */}
            <div>
              <label htmlFor="signatureHeader" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                署名ヘッダー
              </label>
              <input
                id="signatureHeader"
                type="text"
                value={signatureHeader}
                onChange={(e) => setSignatureHeader(e.target.value)}
                placeholder="X-Signature"
                disabled={isSubmitting}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
              />
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                署名を含むHTTPヘッダー名（デフォルト: X-Signature）
              </p>
            </div>

            {/* Signature Type */}
            <div>
              <label htmlFor="signatureType" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                署名タイプ
              </label>
              <select
                id="signatureType"
                value={signatureType}
                onChange={(e) => setSignatureType(e.target.value as WebhookSignatureType)}
                disabled={isSubmitting}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
              >
                <option value="hmac">HMAC</option>
                <option value="static">Static Token</option>
              </select>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                署名検証方式（HMAC推奨）
              </p>
            </div>

            {/* Signature Prefix */}
            {signatureType === 'hmac' && (
              <div>
                <label htmlFor="signaturePrefix" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  署名プレフィックス
                </label>
                <input
                  id="signaturePrefix"
                  type="text"
                  value={signaturePrefix}
                  onChange={(e) => setSignaturePrefix(e.target.value)}
                  placeholder="例: sha256= (GitHub形式) / 空欄で自動検出"
                  disabled={isSubmitting}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
                />
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                  署名ヘッダーの値から取り除くプレフィックス。空欄の場合は自動検出（<code className="bg-gray-100 dark:bg-gray-800 px-1 rounded">sha256=hexdigest</code> 形式も <code className="bg-gray-100 dark:bg-gray-800 px-1 rounded">hexdigest</code> のみの形式も対応）。Sentry など plain hex を送るサービスは空欄のままで動作します。
                </p>
              </div>
            )}

            {/* Secret */}
            <div>
              <label htmlFor="secret" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                シークレット（省略時は自動生成）
              </label>
              <input
                id="secret"
                type="password"
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
                placeholder="外部サービスの Client Secret（例: Sentry Client Secret）"
                disabled={isSubmitting}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
              />
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                外部サービスが指定するシークレットを入力します。空の場合はシークレットが自動生成されます。
              </p>
            </div>
          </div>

          {/* GitHub Settings */}
          {webhookType === 'github' && (
            <div className="space-y-4">
              <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 border-b border-gray-200 dark:border-gray-700 pb-2">
                GitHub設定
              </h3>

            {/* Allowed Events */}
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                許可イベント
              </label>
              <div className="flex flex-wrap gap-2">
                {GITHUB_EVENTS.map((event) => (
                  <button
                    key={event.value}
                    type="button"
                    onClick={() => toggleAllowedEvent(event.value)}
                    className={`px-3 py-1.5 text-sm rounded-md border transition-colors ${
                      allowedEvents.includes(event.value)
                        ? 'bg-blue-100 dark:bg-blue-900/30 border-blue-300 dark:border-blue-700 text-blue-700 dark:text-blue-300'
                        : 'bg-gray-50 dark:bg-gray-700 border-gray-200 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-600'
                    }`}
                    title={event.description}
                  >
                    {event.label}
                  </button>
                ))}
              </div>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                空の場合、すべてのイベントを許可します
              </p>
            </div>

            {/* Allowed Repositories */}
            <div className="relative">
              <label htmlFor="allowedRepositories" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                許可リポジトリ
              </label>
              <input
                id="allowedRepositories"
                type="text"
                value={allowedRepositories}
                onChange={handleRepositoryChange}
                onFocus={handleRepositoryFocus}
                onBlur={handleRepositoryBlur}
                placeholder="例: owner/repo, owner/*"
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
                カンマ区切りで複数指定可。空の場合、すべてのリポジトリを許可します
              </p>
            </div>
            </div>
          )}

          {/* Triggers */}
          <div className="space-y-4">
            <div className="flex items-center justify-between border-b border-gray-200 dark:border-gray-700 pb-2">
              <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300">
                トリガー
              </h3>
              <button
                type="button"
                onClick={addTrigger}
                className="text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1"
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                </svg>
                トリガーを追加
              </button>
            </div>

            <div className="space-y-3">
              {triggers.map((trigger, index) => (
                <div
                  key={index}
                  className="border border-gray-200 dark:border-gray-700 rounded-md overflow-hidden"
                >
                  {/* Trigger Header */}
                  <div
                    className="flex items-center justify-between px-4 py-3 bg-gray-50 dark:bg-gray-700/50 cursor-pointer"
                    onClick={() => setExpandedTrigger(expandedTrigger === index ? null : index)}
                  >
                    <div className="flex items-center gap-3">
                      <svg
                        className={`w-4 h-4 text-gray-500 transition-transform ${
                          expandedTrigger === index ? 'rotate-90' : ''
                        }`}
                        fill="none"
                        stroke="currentColor"
                        viewBox="0 0 24 24"
                      >
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                      </svg>
                      <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
                        {trigger.name || `トリガー ${index + 1}`}
                      </span>
                      {!trigger.enabled && (
                        <span className="text-xs px-2 py-0.5 bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300 rounded">
                          無効
                        </span>
                      )}
                    </div>
                    {triggers.length > 1 && (
                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation()
                          removeTrigger(index)
                        }}
                        className="text-red-500 hover:text-red-700 dark:hover:text-red-400"
                      >
                        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                        </svg>
                      </button>
                    )}
                  </div>

                  {/* Trigger Body */}
                  {expandedTrigger === index && (
                    <div className="px-4 py-4 space-y-4">
                      {/* Trigger Name */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                          トリガー名 *
                        </label>
                        <input
                          type="text"
                          value={trigger.name}
                          onChange={(e) => updateTrigger(index, 'name', e.target.value)}
                          placeholder="例: Review PR on open"
                          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm"
                        />
                      </div>

                      {/* Enabled Toggle */}
                      <div className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          id={`trigger-enabled-${index}`}
                          checked={trigger.enabled}
                          onChange={(e) => updateTrigger(index, 'enabled', e.target.checked)}
                          className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                        />
                        <label htmlFor={`trigger-enabled-${index}`} className="text-sm text-gray-700 dark:text-gray-300">
                          このトリガーを有効にする
                        </label>
                      </div>

                      {/* GitHub Conditions */}
                      {webhookType === 'github' && (
                        <>
                      {/* Events */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                          イベント
                        </label>
                        <div className="flex flex-wrap gap-2">
                          {GITHUB_EVENTS.map((event) => (
                            <button
                              key={event.value}
                              type="button"
                              onClick={() => toggleTriggerEvent(index, event.value)}
                              className={`px-2.5 py-1 text-xs rounded border transition-colors ${
                                trigger.events.includes(event.value)
                                  ? 'bg-green-100 dark:bg-green-900/30 border-green-300 dark:border-green-700 text-green-700 dark:text-green-300'
                                  : 'bg-gray-50 dark:bg-gray-700 border-gray-200 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-600'
                              }`}
                            >
                              {event.label}
                            </button>
                          ))}
                        </div>
                      </div>

                      {/* Actions */}
                      {trigger.events.length > 0 && (
                        <div>
                          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                            アクション
                          </label>
                          <div className="flex flex-wrap gap-2">
                            {getAvailableActions(trigger.events).map((action) => (
                              <button
                                key={action.value}
                                type="button"
                                onClick={() => toggleTriggerAction(index, action.value)}
                                className={`px-2.5 py-1 text-xs rounded border transition-colors ${
                                  trigger.actions.includes(action.value)
                                    ? 'bg-purple-100 dark:bg-purple-900/30 border-purple-300 dark:border-purple-700 text-purple-700 dark:text-purple-300'
                                    : 'bg-gray-50 dark:bg-gray-700 border-gray-200 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-600'
                                }`}
                              >
                                {action.label}
                              </button>
                            ))}
                          </div>
                        </div>
                      )}

                      {/* Base Branches */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                          ベースブランチ
                        </label>
                        <input
                          type="text"
                          value={trigger.baseBranches.join(', ')}
                          onChange={(e) =>
                            updateTrigger(
                              index,
                              'baseBranches',
                              e.target.value.split(',').map((b) => b.trim()).filter((b) => b)
                            )
                          }
                          placeholder="例: main, develop"
                          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm"
                        />
                        <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                          PRのベースブランチでフィルタ（カンマ区切り）
                        </p>
                      </div>

                      {/* Go Template Matcher for GitHub */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                          Go Template Matcher（追加フィルター）
                        </label>
                        <textarea
                          value={trigger.goTemplate || ''}
                          onChange={(e) => updateTrigger(index, 'goTemplate', e.target.value)}
                          placeholder={'例: {{ and (eq .action "opened") (not .pull_request.draft) }}'}
                          rows={3}
                          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm font-mono resize-y"
                        />
                        <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                          より高度な条件判定が必要な場合に使用します。trueまたはfalseを返すGo template式を記述してください。
                        </p>
                        <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                          例: {'{{ contains .pull_request.title "feat:" }}'}, {'{{ and (eq .action "opened") (not .pull_request.draft) }}'}
                        </p>
                      </div>
                        </>
                      )}

                      {/* Custom Webhook Conditions */}
                      {webhookType === 'custom' && (
                        <>
                          {/* Go Template */}
                          <div>
                            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                              Go Template Matcher
                            </label>
                            <textarea
                              value={trigger.goTemplate || ''}
                              onChange={(e) => updateTrigger(index, 'goTemplate', e.target.value)}
                              placeholder={'例: {{ eq .event.type "push" }}'}
                              rows={3}
                              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm font-mono resize-y"
                            />
                            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                              Goテンプレート式。trueまたはfalseを返す式を記述してください。
                            </p>
                            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                              例: {'{{ eq .event.type "push" }}'}, {'{{ and (eq .action "opened") (contains .title "fix") }}'}
                            </p>
                          </div>
                        </>
                      )}

                      {/* Initial Message Template */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                          初期メッセージテンプレート
                        </label>
                        <textarea
                          value={trigger.initialMessageTemplate}
                          onChange={(e) => updateTrigger(index, 'initialMessageTemplate', e.target.value)}
                          placeholder={'例: Review PR #{{.pull_request.Number}} in {{.repository.FullName}}'}
                          rows={2}
                          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm font-mono resize-y"
                        />
                        <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                          Goテンプレート形式。例: {'{{.pull_request.Number}}'}
                        </p>
                      </div>

                      {/* Session Profile */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                          セッションプロファイル
                        </label>
                        <SessionProfileSelect
                          value={trigger.sessionProfileId}
                          onChange={(val) => updateTrigger(index, 'sessionProfileId', val)}
                          disabled={isSubmitting}
                        />
                        <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                          プロファイルを選択すると、環境変数・タグ・テンプレートなどの設定を適用します
                        </p>
                      </div>

                      {/* Advanced Settings Toggle */}
                      <div>
                        <button
                          type="button"
                          onClick={() => updateTrigger(index, 'showAdvanced', !trigger.showAdvanced)}
                          className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white"
                        >
                          <svg
                            className={`w-4 h-4 transition-transform ${trigger.showAdvanced ? 'rotate-90' : ''}`}
                            fill="none"
                            stroke="currentColor"
                            viewBox="0 0 24 24"
                          >
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                          </svg>
                          高度な設定
                        </button>
                      </div>

                      {/* Advanced Settings Content */}
                      {trigger.showAdvanced && (
                        <div className="space-y-4 pl-6 border-l-2 border-gray-200 dark:border-gray-700">
                          {/* Environment Variables */}
                          <div>
                            <div className="flex items-center justify-between mb-2">
                              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                                環境変数
                              </label>
                              <button
                                type="button"
                                onClick={() => addKeyValuePair(index, 'environment')}
                                className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1"
                              >
                                <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                                </svg>
                                追加
                              </button>
                            </div>
                            {Object.entries(trigger.environment).length > 0 ? (
                              <div className="space-y-2">
                                {Object.entries(trigger.environment).map(([key, value], entryIndex) => (
                                  <div key={`environment-${entryIndex}`} className="flex flex-col sm:flex-row gap-2">
                                    <input
                                      type="text"
                                      value={key}
                                      onChange={(e) => updateKeyValuePair(index, 'environment', key, e.target.value, value)}
                                      placeholder="KEY"
                                      className="w-full sm:w-1/3 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs font-mono dark:bg-gray-700 dark:text-white"
                                    />
                                    <input
                                      type="text"
                                      value={value}
                                      onChange={(e) => updateKeyValuePair(index, 'environment', key, key, e.target.value)}
                                      placeholder="value (テンプレート可)"
                                      className="flex-1 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs font-mono dark:bg-gray-700 dark:text-white"
                                    />
                                    <button
                                      type="button"
                                      onClick={() => removeKeyValuePair(index, 'environment', key)}
                                      className="text-red-500 hover:text-red-700 px-2 self-start sm:self-center"
                                    >
                                      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                                      </svg>
                                    </button>
                                  </div>
                                ))}
                              </div>
                            ) : (
                              <p className="text-xs text-gray-500 dark:text-gray-400">
                                環境変数は設定されていません
                              </p>
                            )}
                            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                              値にGoテンプレート構文を使用できます。例: {'{{.pull_request.number}}'}
                            </p>
                          </div>

                          {/* Tags */}
                          <div>
                            <div className="flex items-center justify-between mb-2">
                              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                                タグ
                              </label>
                              <button
                                type="button"
                                onClick={() => addKeyValuePair(index, 'tags')}
                                className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1"
                              >
                                <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                                </svg>
                                追加
                              </button>
                            </div>
                            {Object.entries(trigger.tags).length > 0 ? (
                              <div className="space-y-2">
                                {Object.entries(trigger.tags).map(([key, value], entryIndex) => (
                                  <div key={`tags-${entryIndex}`} className="flex flex-col sm:flex-row gap-2">
                                    <input
                                      type="text"
                                      value={key}
                                      onChange={(e) => updateKeyValuePair(index, 'tags', key, e.target.value, value)}
                                      placeholder="key"
                                      className="w-full sm:w-1/3 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs font-mono dark:bg-gray-700 dark:text-white"
                                    />
                                    <input
                                      type="text"
                                      value={value}
                                      onChange={(e) => updateKeyValuePair(index, 'tags', key, key, e.target.value)}
                                      placeholder="value (テンプレート可)"
                                      className="flex-1 px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded text-xs font-mono dark:bg-gray-700 dark:text-white"
                                    />
                                    <button
                                      type="button"
                                      onClick={() => removeKeyValuePair(index, 'tags', key)}
                                      className="text-red-500 hover:text-red-700 px-2 self-start sm:self-center"
                                    >
                                      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                                      </svg>
                                    </button>
                                  </div>
                                ))}
                              </div>
                            ) : (
                              <p className="text-xs text-gray-500 dark:text-gray-400">
                                タグは設定されていません
                              </p>
                            )}
                            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                              値にGoテンプレート構文を使用できます。例: {'{{.pull_request.number}}'}
                            </p>
                          </div>
                        </div>
                      )}

                      {/* Reuse Session Toggle */}
                      <div className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          id={`trigger-reuse-session-${index}`}
                          checked={trigger.reuseSession}
                          onChange={(e) => updateTrigger(index, 'reuseSession', e.target.checked)}
                          className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                        />
                        <label htmlFor={`trigger-reuse-session-${index}`} className="text-sm text-gray-700 dark:text-gray-300">
                          セッションを再利用する
                        </label>
                      </div>
                      <p className="text-xs text-gray-500 dark:text-gray-400 -mt-3 ml-6">
                        同じ webhook と trigger の組み合わせで実行中のセッションがある場合、新しいセッションを作成せずに既存のセッションにメッセージを送信します
                      </p>

                      {/* Mount Payload Toggle */}
                      <div className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          id={`trigger-mount-payload-${index}`}
                          checked={trigger.mountPayload}
                          onChange={(e) => updateTrigger(index, 'mountPayload', e.target.checked)}
                          className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                        />
                        <label htmlFor={`trigger-mount-payload-${index}`} className="text-sm text-gray-700 dark:text-gray-300">
                          Webhookペイロードをマウントする
                        </label>
                      </div>
                      <p className="text-xs text-gray-500 dark:text-gray-400 -mt-3 ml-6">
                        有効にすると、Webhookペイロードが /opt/webhook/payload.json としてセッションコンテナにマウントされます
                      </p>

                      {/* Oneshot Toggle */}
                      <div className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          id={`trigger-oneshot-${index}`}
                          checked={trigger.oneshot}
                          onChange={(e) => updateTrigger(index, 'oneshot', e.target.checked)}
                          className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                        />
                        <label htmlFor={`trigger-oneshot-${index}`} className="text-sm text-gray-700 dark:text-gray-300">
                          ワンショットセッション
                        </label>
                      </div>
                      <p className="text-xs text-gray-500 dark:text-gray-400 -mt-3 ml-6">
                        有効にすると、セッション終了後に自動的に削除されます
                      </p>

                      {/* Credential Source */}
                      <div>
                        <label
                          htmlFor={`trigger-credential-source-${index}`}
                          className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
                        >
                          認証ファイルの参照元
                        </label>
                        <select
                          id={`trigger-credential-source-${index}`}
                          value={trigger.credentialSource}
                          onChange={(e) => updateCredentialSource(
                            index,
                            e.target.value as TriggerFormData['credentialSource']
                          )}
                          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-sm dark:bg-gray-700 dark:text-white"
                        >
                          <option value="">デフォルト（team scopeでは認証ファイルなし）</option>
                          <option value="triggered_user">Webhookをトリガーしたユーザー</option>
                          <option value="team">チーム</option>
                          <option value="session_user">Webhook設定の所有者</option>
                          <option value="none">使用しない</option>
                        </select>
                        <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                          「Webhookをトリガーしたユーザー」を選ぶと、未設定の場合は username: {webhookType === 'github' ? '{{.sender.login}}' : '{{.user.login}}'} を自動追加します。認証ファイルが見つからない場合はチームへフォールバックします。
                        </p>
                      </div>

                      {/* Reuse Message Template */}
                      {trigger.reuseSession && (
                        <div>
                          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                            セッション再利用時のメッセージテンプレート
                          </label>
                          <textarea
                            value={trigger.reuseMessageTemplate || ''}
                            onChange={(e) => updateTrigger(index, 'reuseMessageTemplate', e.target.value)}
                            placeholder={'例: New update for PR #{{.pull_request.Number}}'}
                            rows={2}
                            className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm font-mono resize-y"
                          />
                          <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                            セッション再利用時に送信するメッセージ。未設定の場合、初期メッセージテンプレートが使用されます
                          </p>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </div>

          {/* Error */}
          {error && (
            <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md p-3">
              <div className="flex items-start gap-2">
                <svg className="w-5 h-5 text-red-600 dark:text-red-400 flex-shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <p className="text-red-600 dark:text-red-400 text-sm whitespace-pre-wrap break-words flex-1">{error}</p>
              </div>
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
