'use client'

import { useState } from 'react'
import { CheckCircle2, Lock } from 'lucide-react'
import { CredentialsMetadata, createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import { useToast } from '@/contexts/ToastContext'
import { CodexDeviceAuthSettings } from './CodexDeviceAuthSettings'
import { DangerZone, SettingsSubsection } from './ui/SettingsPageHeader'

interface CodexCredentialsSettingsProps {
  /** CodexDeviceAuthSettings に渡すスコープ */
  scope: 'user' | 'team'
  /** credentials API に渡す名前（ユーザー名またはチーム名） */
  scopeName: string
  teamId?: string
  credentialsMetadata: CredentialsMetadata | null
  /** アップロードや認証完了でメタデータを取り直す */
  onChanged: () => void | Promise<void>
  /** 削除に成功したときに表示状態を消す */
  onDeleted: () => void
}

export function CodexCredentialsSettings({
  scope,
  scopeName,
  teamId,
  credentialsMetadata,
  onChanged,
  onDeleted,
}: CodexCredentialsSettingsProps) {
  const [credentialsJson, setCredentialsJson] = useState('')
  const [jsonError, setJsonError] = useState<string | null>(null)
  const [uploading, setUploading] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const { showToast } = useToast()

  const hasCredentials = !!credentialsMetadata?.has_data

  const handleJsonChange = (value: string) => {
    setCredentialsJson(value)
    setJsonError(null)
    if (value.trim()) {
      try {
        JSON.parse(value)
      } catch {
        setJsonError('有効な JSON を入力してください')
      }
    }
  }

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = (ev) => {
      const text = ev.target?.result as string
      setCredentialsJson(text)
      setJsonError(null)
      try {
        JSON.parse(text)
      } catch {
        setJsonError('有効な JSON ファイルを選択してください')
      }
    }
    reader.readAsText(file)
    // 同じファイルを選び直せるように入力をリセットする
    e.target.value = ''
  }

  const handleUpload = async () => {
    if (!credentialsJson.trim() || jsonError) return
    setUploading(true)
    try {
      const parsed = JSON.parse(credentialsJson)
      const client = createAgentAPIProxyClientFromStorage()
      await client.uploadCredentials(scopeName, parsed)
      setCredentialsJson('')
      await onChanged()
      showToast('auth.json をアップロードしました', 'success')
    } catch (err) {
      console.error('Failed to upload credentials:', err)
      showToast('アップロードに失敗しました', 'error')
    } finally {
      setUploading(false)
    }
  }

  const handleDelete = async () => {
    if (!confirm('auth.json を削除しますか？')) return
    setDeleting(true)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.deleteCredentials(scopeName)
      setCredentialsJson('')
      onDeleted()
      showToast('auth.json を削除しました', 'success')
    } catch (err) {
      console.error('Failed to delete credentials:', err)
      showToast('削除に失敗しました', 'error')
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div>
      <div className="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 dark:border-gray-700 dark:bg-gray-800/50">
        {hasCredentials ? (
          <>
            <CheckCircle2 className="h-4 w-4 flex-shrink-0 text-green-500" />
            <span className="text-sm text-gray-700 dark:text-gray-300">
              auth.json はアップロード済みです
              {credentialsMetadata?.updated_at && (
                <span className="ml-2 text-xs text-gray-400 dark:text-gray-500">
                  (更新: {new Date(credentialsMetadata.updated_at).toLocaleString()})
                </span>
              )}
            </span>
          </>
        ) : (
          <>
            <Lock className="h-4 w-4 flex-shrink-0 text-gray-400" />
            <span className="text-sm text-gray-500 dark:text-gray-400">auth.json は未設定です</span>
          </>
        )}
      </div>

      <SettingsSubsection
        title="デバイス認証"
        description="ブラウザで Codex にログインして認証情報を登録します（推奨）"
      >
        <CodexDeviceAuthSettings
          scope={scope}
          teamId={teamId}
          hasCredentials={hasCredentials}
          onAuthComplete={() => {
            void onChanged()
          }}
        />
      </SettingsSubsection>

      <SettingsSubsection
        title="手動でアップロード"
        description="手元の ~/.codex/auth.json をそのまま登録します"
      >
        <div className="space-y-4">
          <div>
            <label className="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">
              ファイルを選択
            </label>
            <input
              type="file"
              accept=".json,application/json"
              onChange={handleFileChange}
              className="block w-full text-sm text-gray-500 file:mr-3 file:cursor-pointer file:rounded file:border file:border-gray-300 file:bg-white file:px-3 file:py-1.5 file:text-xs file:font-medium file:text-gray-700 hover:file:bg-gray-50 dark:text-gray-400 dark:file:border-gray-600 dark:file:bg-gray-700 dark:file:text-gray-300 dark:hover:file:bg-gray-600"
            />
          </div>

          <div>
            <label
              htmlFor="codex-credentials-json"
              className="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300"
            >
              または JSON を直接貼り付け
            </label>
            <textarea
              id="codex-credentials-json"
              value={credentialsJson}
              onChange={(e) => handleJsonChange(e.target.value)}
              rows={6}
              className="w-full resize-y rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-xs text-gray-900 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800 dark:text-white"
            />
            {jsonError && <p className="mt-1 text-xs text-red-600 dark:text-red-400">{jsonError}</p>}
          </div>

          <button
            type="button"
            onClick={handleUpload}
            disabled={!credentialsJson.trim() || !!jsonError || uploading}
            className="flex items-center gap-2 rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {uploading && (
              <span className="h-3.5 w-3.5 animate-spin rounded-full border-b-2 border-white" />
            )}
            {uploading ? 'アップロード中...' : 'アップロード'}
          </button>
        </div>
      </SettingsSubsection>

      {hasCredentials && (
        <DangerZone>
          <div className="flex items-center justify-between gap-4">
            <div>
              <p className="text-sm font-medium text-gray-900 dark:text-white">auth.json を削除</p>
              <p className="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                削除するとこのスコープのセッションで Codex 認証が使えなくなります
              </p>
            </div>
            <button
              type="button"
              onClick={handleDelete}
              disabled={deleting}
              className="flex-shrink-0 rounded-md border border-red-300 px-3 py-1.5 text-sm text-red-600 transition-colors hover:bg-red-50 disabled:opacity-50 dark:border-red-700 dark:text-red-400 dark:hover:bg-red-900/20"
            >
              {deleting ? '削除中...' : '削除'}
            </button>
          </div>
        </DangerZone>
      )}
    </div>
  )
}
