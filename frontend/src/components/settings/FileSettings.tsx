'use client'

import { useState, useEffect, useCallback } from 'react'
import { UserFile, CreateFileRequest, UpdateFileRequest } from '../../types/user_file'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'

interface FileSettingsProps {
  userName: string
}

type EditingState =
  | { kind: 'none' }
  | { kind: 'new' }
  | { kind: 'edit'; file: UserFile }

const emptyForm = (): CreateFileRequest => ({
  name: '',
  path: '',
  content: '',
  permissions: '0600',
})

export function FileSettings({ userName }: FileSettingsProps) {
  const [files, setFiles] = useState<UserFile[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState<EditingState>({ kind: 'none' })
  const [form, setForm] = useState<CreateFileRequest>(emptyForm())
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [deletingIds, setDeletingIds] = useState<Set<string>>(new Set())
  const [showContent, setShowContent] = useState<Set<string>>(new Set())

  const fetchFiles = useCallback(async () => {
    if (!userName) return
    setLoading(true)
    setError(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      const response = await client.listFiles()
      setFiles(response.files || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'ファイルの取得に失敗しました')
    } finally {
      setLoading(false)
    }
  }, [userName])

  useEffect(() => {
    fetchFiles()
  }, [fetchFiles])

  const handleNew = () => {
    setForm(emptyForm())
    setSubmitError(null)
    setEditing({ kind: 'new' })
  }

  const handleEdit = (file: UserFile) => {
    setForm({
      name: file.name,
      path: file.path,
      content: file.content,
      permissions: file.permissions || '0600',
    })
    setSubmitError(null)
    setEditing({ kind: 'edit', file })
  }

  const handleCancel = () => {
    setEditing({ kind: 'none' })
    setSubmitError(null)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!form.path.trim()) return
    setSubmitting(true)
    setSubmitError(null)
    try {
      const client = createAgentAPIProxyClientFromStorage()
      if (editing.kind === 'new') {
        const created = await client.createFile({
          name: form.name?.trim() || undefined,
          path: form.path.trim(),
          content: form.content,
          permissions: form.permissions?.trim() || undefined,
        })
        setFiles((prev) => [...prev, created])
      } else if (editing.kind === 'edit') {
        const update: UpdateFileRequest = {
          name: form.name?.trim(),
          path: form.path.trim(),
          content: form.content,
          permissions: form.permissions?.trim(),
        }
        const updated = await client.updateFile(editing.file.id, update)
        setFiles((prev) => prev.map((f) => (f.id === updated.id ? updated : f)))
      }
      setEditing({ kind: 'none' })
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : '保存に失敗しました')
    } finally {
      setSubmitting(false)
    }
  }

  const handleDelete = async (file: UserFile) => {
    if (!confirm(`「${file.name || file.path}」を削除しますか？\nセッション起動時にこのファイルは配置されなくなります。`)) return
    setDeletingIds((prev) => new Set(prev).add(file.id))
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.deleteFile(file.id)
      setFiles((prev) => prev.filter((f) => f.id !== file.id))
    } catch (err) {
      alert(err instanceof Error ? err.message : '削除に失敗しました')
    } finally {
      setDeletingIds((prev) => {
        const next = new Set(prev)
        next.delete(file.id)
        return next
      })
    }
  }

  const toggleContent = (id: string) => {
    setShowContent((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const isFormOpen = editing.kind !== 'none'

  return (
    <div className="space-y-4">
      {/* Description */}
      <div className="p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
        <p className="text-xs text-blue-700 dark:text-blue-300">
          SSH 鍵や設定ファイルなど任意のファイルを登録すると、セッション起動時に指定したパスへ自動配置されます。パーミッションは octal 形式（例: <code className="bg-blue-100 dark:bg-blue-800 px-1 rounded">0600</code>）で指定でき、未指定の場合は <code className="bg-blue-100 dark:bg-blue-800 px-1 rounded">0600</code> が使われます。
        </p>
      </div>

      {/* Error */}
      {error && (
        <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
          <p className="text-xs text-red-600 dark:text-red-400">{error}</p>
          <button onClick={fetchFiles} className="mt-1 text-xs text-blue-600 dark:text-blue-400 hover:underline">
            再試行
          </button>
        </div>
      )}

      {/* Loading */}
      {loading && (
        <div className="flex items-center gap-2 py-4 text-sm text-gray-500 dark:text-gray-400">
          <svg className="w-4 h-4 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          読み込み中...
        </div>
      )}

      {/* File list */}
      {!loading && files.length === 0 && !isFormOpen && (
        <p className="text-sm text-gray-500 dark:text-gray-400">
          登録済みのファイルがありません
        </p>
      )}

      {!loading && files.length > 0 && (
        <div className="space-y-2">
          {files.map((file) => {
            const isDeleting = deletingIds.has(file.id)
            const isEditing = editing.kind === 'edit' && editing.file.id === file.id
            const contentVisible = showContent.has(file.id)

            return (
              <div
                key={file.id}
                className={`border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden transition-opacity ${isDeleting ? 'opacity-50' : ''}`}
              >
                {/* Row */}
                <div className="flex items-center gap-3 p-3">
                  {/* Icon */}
                  <div className="flex-shrink-0 w-7 h-7 rounded-full bg-gray-100 dark:bg-gray-700 flex items-center justify-center">
                    <svg className="w-3.5 h-3.5 text-yellow-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z" />
                    </svg>
                  </div>

                  {/* Info */}
                  <div className="flex-1 min-w-0">
                    {file.name && (
                      <p className="text-sm font-medium text-gray-900 dark:text-white truncate">
                        {file.name}
                      </p>
                    )}
                    <code className="text-xs font-mono text-gray-600 dark:text-gray-400 break-all">
                      {file.path}
                    </code>
                    <div className="flex items-center gap-2 mt-0.5">
                      {file.permissions && (
                        <span className="text-xs text-gray-400 dark:text-gray-500">{file.permissions}</span>
                      )}
                      {file.content ? (
                        <span className="text-xs text-green-600 dark:text-green-400">
                          {file.content.length} bytes
                        </span>
                      ) : (
                        <span className="text-xs text-gray-400 dark:text-gray-500">内容なし</span>
                      )}
                    </div>
                  </div>

                  {/* Actions */}
                  <div className="flex items-center gap-1 flex-shrink-0">
                    {file.content && (
                      <button
                        type="button"
                        onClick={() => toggleContent(file.id)}
                        className="text-xs px-2 py-1 rounded border border-gray-300 dark:border-gray-600 text-gray-500 dark:text-gray-400 hover:border-blue-300 dark:hover:border-blue-700 hover:text-blue-600 dark:hover:text-blue-400 transition-colors"
                        title={contentVisible ? '内容を隠す' : '内容を表示'}
                      >
                        {contentVisible ? '隠す' : '表示'}
                      </button>
                    )}
                    <button
                      type="button"
                      onClick={() => handleEdit(file)}
                      disabled={isDeleting || isFormOpen}
                      className="text-xs px-2 py-1 rounded border border-gray-300 dark:border-gray-600 text-gray-500 dark:text-gray-400 hover:border-blue-300 dark:hover:border-blue-700 hover:text-blue-600 dark:hover:text-blue-400 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                    >
                      編集
                    </button>
                    <button
                      type="button"
                      onClick={() => handleDelete(file)}
                      disabled={isDeleting || isFormOpen}
                      className="text-xs px-2 py-1 rounded border border-red-200 dark:border-red-700 text-red-500 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                    >
                      {isDeleting ? '削除中...' : '削除'}
                    </button>
                  </div>
                </div>

                {/* Content preview */}
                {contentVisible && file.content && (
                  <div className="border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900 p-3">
                    <pre className="text-xs font-mono text-gray-700 dark:text-gray-300 whitespace-pre-wrap break-all max-h-40 overflow-y-auto">
                      {file.content}
                    </pre>
                  </div>
                )}

                {/* Inline edit form */}
                {isEditing && (
                  <div className="border-t border-gray-200 dark:border-gray-700 bg-blue-50 dark:bg-blue-900/10 p-3">
                    <FileForm
                      form={form}
                      onChange={setForm}
                      onSubmit={handleSubmit}
                      onCancel={handleCancel}
                      submitting={submitting}
                      error={submitError}
                      isEdit
                    />
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}

      {/* New file form */}
      {editing.kind === 'new' && (
        <div className="p-3 border border-blue-200 dark:border-blue-800 rounded-lg bg-blue-50 dark:bg-blue-900/10 space-y-3">
          <p className="text-xs font-medium text-gray-700 dark:text-gray-300">新しいファイルを登録</p>
          <FileForm
            form={form}
            onChange={setForm}
            onSubmit={handleSubmit}
            onCancel={handleCancel}
            submitting={submitting}
            error={submitError}
            isEdit={false}
          />
        </div>
      )}

      {/* Add button */}
      {!isFormOpen && !loading && (
        <button
          type="button"
          onClick={handleNew}
          className="flex items-center gap-1 text-sm text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 transition-colors"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          ファイルを追加
        </button>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Inner form component
// ---------------------------------------------------------------------------

interface FileFormProps {
  form: CreateFileRequest
  onChange: (f: CreateFileRequest) => void
  onSubmit: (e: React.FormEvent) => void
  onCancel: () => void
  submitting: boolean
  error: string | null
  isEdit: boolean
}

function FileForm({ form, onChange, onSubmit, onCancel, submitting, error, isEdit }: FileFormProps) {
  const inputClass =
    'w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500'

  return (
    <form onSubmit={onSubmit} className="space-y-3">
      {/* Name */}
      <div>
        <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
          表示名 <span className="text-gray-400 font-normal">(任意)</span>
        </label>
        <input
          type="text"
          value={form.name ?? ''}
          onChange={(e) => onChange({ ...form, name: e.target.value })}
          placeholder="例: GitHub SSH Key"
          className={inputClass}
        />
      </div>

      {/* Path */}
      <div>
        <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
          配置パス <span className="text-red-500">*</span>
        </label>
        <input
          type="text"
          value={form.path}
          onChange={(e) => onChange({ ...form, path: e.target.value })}
          placeholder="例: /home/agentapi/.ssh/id_ed25519"
          required
          className={inputClass}
        />
        <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
          セッション内コンテナでのファイルパス
        </p>
      </div>

      {/* Permissions */}
      <div>
        <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
          パーミッション
        </label>
        <div className="flex items-center gap-2">
          <input
            type="text"
            value={form.permissions ?? ''}
            onChange={(e) => onChange({ ...form, permissions: e.target.value })}
            placeholder="0600"
            className={`${inputClass} w-28 font-mono`}
          />
          <div className="flex gap-1">
            {(['0600', '0644', '0700', '0755'] as const).map((p) => (
              <button
                key={p}
                type="button"
                onClick={() => onChange({ ...form, permissions: p })}
                className={`px-2 py-1.5 text-xs rounded border font-mono transition-colors ${
                  form.permissions === p
                    ? 'bg-blue-600 text-white border-blue-600'
                    : 'border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:border-blue-400 dark:hover:border-blue-600'
                }`}
              >
                {p}
              </button>
            ))}
          </div>
        </div>
        <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
          octal 形式で指定します。未指定の場合は 0600 が使われます。
        </p>
      </div>

      {/* Content */}
      <div>
        <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
          ファイル内容
        </label>
        <textarea
          value={form.content ?? ''}
          onChange={(e) => onChange({ ...form, content: e.target.value })}
          placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;..."
          rows={8}
          className={`${inputClass} font-mono text-xs resize-y`}
        />
        <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
          SSH 秘密鍵などを貼り付けてください
        </p>
      </div>

      {/* Error */}
      {error && (
        <p className="text-xs text-red-500 dark:text-red-400">{error}</p>
      )}

      {/* Buttons */}
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={!form.path.trim() || submitting}
          className="px-3 py-1.5 text-xs bg-blue-600 text-white rounded-md hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors flex items-center gap-1.5"
        >
          {submitting && (
            <svg className="w-3 h-3 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
          )}
          {submitting ? '保存中...' : isEdit ? '更新' : '追加'}
        </button>
        <button
          type="button"
          onClick={onCancel}
          disabled={submitting}
          className="px-3 py-1.5 text-xs bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-300 rounded-md hover:bg-gray-300 dark:hover:bg-gray-600 disabled:opacity-50 transition-colors"
        >
          キャンセル
        </button>
      </div>
    </form>
  )
}
