'use client'

import { useState, useEffect } from 'react'

interface EnvVarsSettingsProps {
  envVarKeys?: string[]  // 環境変数のキーのみ（読み取り時）
  onChange: (envVars: Record<string, string>) => void
}

interface EnvVarEntry {
  key: string
  value: string
  isNew: boolean
}

export function EnvVarsSettings({ envVarKeys, onChange }: EnvVarsSettingsProps) {
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [editingEntry, setEditingEntry] = useState<EnvVarEntry | null>(null)
  const [editKey, setEditKey] = useState('')
  const [editValue, setEditValue] = useState('')
  const [keyError, setKeyError] = useState('')
  // ローカル状態で環境変数のキーを管理（UIに即座に反映するため）
  const [localKeys, setLocalKeys] = useState<string[]>(envVarKeys || [])

  // envVarKeys が変更されたらローカル状態を更新
  useEffect(() => {
    setLocalKeys(envVarKeys || [])
  }, [envVarKeys])

  const existingKeys = localKeys

  const handleAdd = () => {
    setEditingEntry({
      key: '',
      value: '',
      isNew: true,
    })
    setEditKey('')
    setEditValue('')
    setKeyError('')
    setIsModalOpen(true)
  }

  const handleEdit = (key: string) => {
    setEditingEntry({
      key,
      value: '',
      isNew: false,
    })
    setEditKey(key)
    setEditValue('')
    setKeyError('')
    setIsModalOpen(true)
  }

  const handleDelete = (key: string) => {
    if (confirm(`環境変数 "${key}" を削除してもよろしいですか？`)) {
      // ローカル状態から削除
      setLocalKeys(prev => prev.filter(k => k !== key))
      // 空文字列を送信して削除をマーク
      const updates: Record<string, string> = {}
      updates[key] = ''
      onChange(updates)
    }
  }

  const validateKey = (key: string): boolean => {
    if (!key.trim()) {
      setKeyError('環境変数名を入力してください')
      return false
    }
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) {
      setKeyError('環境変数名は英字またはアンダースコアで始まり、英数字とアンダースコアのみを含む必要があります')
      return false
    }
    if (editingEntry?.isNew && existingKeys.includes(key)) {
      setKeyError('この環境変数名は既に存在します')
      return false
    }
    if (!editingEntry?.isNew && editingEntry?.key !== key && existingKeys.includes(key)) {
      setKeyError('この環境変数名は既に存在します')
      return false
    }
    return true
  }

  const handleModalSave = () => {
    if (!validateKey(editKey)) {
      return
    }

    if (!editValue.trim()) {
      alert('環境変数の値を入力してください')
      return
    }

    const updates: Record<string, string> = {}

    // 名前が変更された場合は古いキーを削除
    if (editingEntry && !editingEntry.isNew && editingEntry.key !== editKey) {
      // ローカル状態から古いキーを削除
      setLocalKeys(prev => prev.filter(k => k !== editingEntry.key))
      updates[editingEntry.key] = ''
    }

    // 新しいキーの場合はローカル状態に追加
    if (editingEntry?.isNew) {
      setLocalKeys(prev => [...prev, editKey].sort())
    }

    updates[editKey] = editValue
    onChange(updates)
    setIsModalOpen(false)
    setEditingEntry(null)
  }

  const handleModalClose = () => {
    setIsModalOpen(false)
    setEditingEntry(null)
    setKeyError('')
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between mb-4">
        <div>
          <p className="text-sm text-gray-600 dark:text-gray-400">
            セッション起動時に自動的に読み込まれるカスタム環境変数を設定します。
            <br />
            <span className="text-yellow-600 dark:text-yellow-400">
              ⚠️ セキュリティのため、既存の環境変数の値は表示されません（キーのみ表示）
            </span>
          </p>
        </div>
        <button
          onClick={handleAdd}
          className="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700 transition-colors text-sm"
        >
          + 追加
        </button>
      </div>

      {existingKeys.length === 0 ? (
        <div className="text-center py-8 text-gray-500 dark:text-gray-400 bg-gray-50 dark:bg-gray-800 rounded-lg">
          環境変数が設定されていません
        </div>
      ) : (
        <div className="space-y-2">
          {existingKeys.map((key) => (
            <div
              key={key}
              className="flex items-center justify-between p-4 border border-gray-200 dark:border-gray-700 rounded-lg hover:border-gray-300 dark:hover:border-gray-600 transition-colors"
            >
              <div className="flex-1">
                <div className="flex items-center gap-2">
                  <span className="text-lg">🔒</span>
                  <div>
                    <p className="font-medium text-gray-900 dark:text-white font-mono">
                      {key}
                    </p>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      値: ••••••••（セキュリティのため非表示）
                    </p>
                  </div>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => handleEdit(key)}
                  className="px-3 py-1 text-sm text-blue-600 dark:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20 rounded transition-colors"
                >
                  編集
                </button>
                <button
                  onClick={() => handleDelete(key)}
                  className="px-3 py-1 text-sm text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 rounded transition-colors"
                >
                  削除
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Modal */}
      {isModalOpen && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-2xl w-full mx-4 max-h-[90vh] overflow-y-auto">
            <div className="p-6">
              <h3 className="text-xl font-bold text-gray-900 dark:text-white mb-4">
                {editingEntry?.isNew ? '環境変数を追加' : '環境変数を編集'}
              </h3>

              <div className="space-y-4">
                {/* 環境変数名 */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    環境変数名 *
                  </label>
                  <input
                    type="text"
                    value={editKey}
                    onChange={(e) => {
                      setEditKey(e.target.value)
                      setKeyError('')
                    }}
                    disabled={!editingEntry?.isNew}
                    placeholder="例: MY_API_KEY"
                    className={`w-full px-3 py-2 border rounded-md font-mono ${
                      keyError
                        ? 'border-red-500 focus:ring-red-500'
                        : 'border-gray-300 dark:border-gray-600 focus:ring-blue-500'
                    } dark:bg-gray-700 dark:text-white focus:ring-2 focus:border-transparent ${
                      !editingEntry?.isNew ? 'bg-gray-100 dark:bg-gray-900 cursor-not-allowed' : ''
                    }`}
                  />
                  {keyError && (
                    <p className="mt-1 text-sm text-red-600 dark:text-red-400">{keyError}</p>
                  )}
                  {!editingEntry?.isNew && (
                    <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      環境変数名は変更できません。変更する場合は削除して再作成してください。
                    </p>
                  )}
                </div>

                {/* 環境変数の値 */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    値 *
                  </label>
                  <textarea
                    value={editValue}
                    onChange={(e) => setEditValue(e.target.value)}
                    placeholder="環境変数の値を入力してください"
                    rows={3}
                    className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md dark:bg-gray-700 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent font-mono text-sm"
                  />
                  {!editingEntry?.isNew && (
                    <p className="mt-1 text-xs text-yellow-600 dark:text-yellow-400">
                      ⚠️ セキュリティのため、既存の値は表示されません。新しい値を入力してください。
                    </p>
                  )}
                </div>
              </div>

              <div className="flex justify-end gap-3 mt-6">
                <button
                  onClick={handleModalClose}
                  className="px-4 py-2 text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 rounded-md hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
                >
                  キャンセル
                </button>
                <button
                  onClick={handleModalSave}
                  className="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700 transition-colors"
                >
                  保存
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
