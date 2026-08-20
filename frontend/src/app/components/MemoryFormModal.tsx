'use client'

import { useState, useEffect, useCallback } from 'react'
import {
  Memory,
  MemoryScope,
  CreateMemoryRequest,
  UpdateMemoryRequest,
} from '../../types/memory'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import { useTeamScope } from '../../contexts/TeamScopeContext'

interface MemoryFormModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess: () => void
  editingMemory?: Memory | null
}

type TagPair = { key: string; value: string }

function tagsToRecord(pairs: TagPair[]): Record<string, string> {
  const result: Record<string, string> = {}
  for (const { key, value } of pairs) {
    if (key.trim()) result[key.trim()] = value
  }
  return result
}

export default function MemoryFormModal({
  isOpen,
  onClose,
  onSuccess,
  editingMemory,
}: MemoryFormModalProps) {
  const { getScopeParams } = useTeamScope()

  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [scope, setScope] = useState<MemoryScope>('user')
  const [teamId, setTeamId] = useState('')
  const [tagPairs, setTagPairs] = useState<TagPair[]>([{ key: '', value: '' }])

  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const isEditing = !!editingMemory

  const resetForm = useCallback(() => {
    setTitle('')
    setContent('')
    setScope('user')
    setTeamId('')
    setTagPairs([{ key: '', value: '' }])
    setError(null)
  }, [])

  // Initialize form from editingMemory or defaults
  useEffect(() => {
    if (!isOpen) return
    if (editingMemory) {
      setTitle(editingMemory.title)
      setContent(editingMemory.content || '')
      setScope(editingMemory.scope)
      setTeamId(editingMemory.team_id || '')
      const pairs = Object.entries(editingMemory.tags || {}).map(([key, value]) => ({ key, value }))
      setTagPairs(pairs.length > 0 ? pairs : [{ key: '', value: '' }])
      setError(null)
    } else {
      resetForm()
      // Auto-fill team_id from context when creating
      const scopeParams = getScopeParams()
      if (scopeParams.scope === 'team' && scopeParams.team_id) {
        setScope('team')
        setTeamId(scopeParams.team_id)
      }
    }
  }, [editingMemory, isOpen, resetForm, getScopeParams])

  // ESC key handler
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen) onClose()
    }
    if (isOpen) document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose])

  const handleTagChange = (index: number, field: 'key' | 'value', val: string) => {
    setTagPairs((prev) => prev.map((p, i) => (i === index ? { ...p, [field]: val } : p)))
  }

  const handleAddTagRow = () => {
    setTagPairs((prev) => [...prev, { key: '', value: '' }])
  }

  const handleRemoveTagRow = (index: number) => {
    setTagPairs((prev) => prev.filter((_, i) => i !== index))
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!title.trim()) {
      setError('タイトルは必須です')
      return
    }
    if (scope === 'team' && !teamId.trim()) {
      setError('チームスコープの場合はチームIDが必要です')
      return
    }

    try {
      setIsSubmitting(true)
      setError(null)

      const client = createAgentAPIProxyClientFromStorage()
      const tags = tagsToRecord(tagPairs)

      if (isEditing && editingMemory) {
        const updateData: UpdateMemoryRequest = {
          title: title.trim(),
          content: content.trim(),
          tags: Object.keys(tags).length > 0 ? tags : {},
        }
        await client.updateMemory(editingMemory.id, updateData)
      } else {
        const createData: CreateMemoryRequest = {
          title: title.trim(),
          scope,
          content: content.trim() || undefined,
          team_id: scope === 'team' ? teamId.trim() : undefined,
          tags: Object.keys(tags).length > 0 ? tags : undefined,
        }
        await client.createMemory(createData)
      }

      onSuccess()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存に失敗しました')
    } finally {
      setIsSubmitting(false)
    }
  }

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 rounded-lg w-full max-w-lg shadow-xl overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
            {isEditing ? 'メモリを編集' : '新しいメモリ'}
          </h2>
          <button
            onClick={onClose}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        {/* Form */}
        <form onSubmit={handleSubmit} className="px-6 py-4 space-y-4 max-h-[75vh] overflow-y-auto">
          {/* Error */}
          {error && (
            <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md p-3 text-sm text-red-700 dark:text-red-400">
              {error}
            </div>
          )}

          {/* Title */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              タイトル <span className="text-red-500">*</span>
            </label>
            <input
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="メモリのタイトル"
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm"
              required
            />
          </div>

          {/* Scope (create only) */}
          {!isEditing && (
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                スコープ <span className="text-red-500">*</span>
              </label>
              <select
                value={scope}
                onChange={(e) => setScope(e.target.value as MemoryScope)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm"
              >
                <option value="user">Personal（個人）</option>
                <option value="team">Team（チーム）</option>
              </select>
            </div>
          )}

          {/* Team ID (when scope=team and creating) */}
          {!isEditing && scope === 'team' && (
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                チームID <span className="text-red-500">*</span>
              </label>
              <input
                type="text"
                value={teamId}
                onChange={(e) => setTeamId(e.target.value)}
                placeholder="例: org/team-slug"
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm font-mono"
              />
            </div>
          )}

          {/* Content */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              コンテンツ
            </label>
            <textarea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder="メモリの内容を入力してください..."
              rows={8}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm resize-y"
            />
          </div>

          {/* Tags */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              タグ
            </label>
            <div className="space-y-2">
              {tagPairs.map((pair, index) => (
                <div key={index} className="flex gap-2 items-center">
                  <input
                    type="text"
                    placeholder="キー"
                    value={pair.key}
                    onChange={(e) => handleTagChange(index, 'key', e.target.value)}
                    className="flex-1 px-3 py-1.5 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm font-mono"
                  />
                  <span className="text-gray-400 text-sm">=</span>
                  <input
                    type="text"
                    placeholder="値"
                    value={pair.value}
                    onChange={(e) => handleTagChange(index, 'value', e.target.value)}
                    className="flex-1 px-3 py-1.5 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm font-mono"
                  />
                  <button
                    type="button"
                    onClick={() => handleRemoveTagRow(index)}
                    className="text-gray-400 hover:text-red-500 transition-colors"
                    aria-label="このタグを削除"
                  >
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                    </svg>
                  </button>
                </div>
              ))}
              <button
                type="button"
                onClick={handleAddTagRow}
                className="text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1"
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                </svg>
                タグを追加
              </button>
            </div>
          </div>
        </form>

        {/* Footer */}
        <div className="flex justify-end gap-3 px-6 py-4 border-t border-gray-200 dark:border-gray-700">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors"
          >
            キャンセル
          </button>
          <button
            type="submit"
            form=""
            onClick={handleSubmit}
            disabled={isSubmitting}
            className="px-4 py-2 text-sm text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 rounded-md transition-colors inline-flex items-center gap-2"
          >
            {isSubmitting && (
              <svg className="w-4 h-4 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
            )}
            {isSubmitting ? '保存中...' : isEditing ? '更新' : '作成'}
          </button>
        </div>
      </div>
    </div>
  )
}
