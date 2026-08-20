'use client'

import { useState, useEffect, useCallback, useRef } from 'react'
import { Memory, MemoryScope } from '../../types/memory'
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client'
import MemoryCard from './MemoryCard'
import { useTeamScope } from '../../contexts/TeamScopeContext'

interface MemoryListViewProps {
  scopeFilter: MemoryScope | null
  tagFilter: Record<string, string>
  onMemoryEdit: (memory: Memory) => void
  onMemoriesUpdate?: () => void
}

export default function MemoryListView({
  scopeFilter,
  tagFilter,
  onMemoryEdit,
  onMemoriesUpdate,
}: MemoryListViewProps) {
  const { getScopeParams } = useTeamScope()
  const [memories, setMemories] = useState<Memory[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [deletingIds, setDeletingIds] = useState<Set<string>>(new Set())
  const fetchIdRef = useRef(0)

  const fetchMemories = useCallback(async () => {
    const fetchId = ++fetchIdRef.current
    try {
      setLoading(true)
      setError(null)

      const scopeParams = getScopeParams()
      const client = createAgentAPIProxyClientFromStorage()

      // Build params: scope from filter, team_id from scopeParams, includeTags from tagFilter
      const params: Parameters<typeof client.listMemories>[0] = {
        ...(scopeFilter ? { scope: scopeFilter } : {}),
        ...(scopeParams.scope === 'team' && scopeParams.team_id
          ? { team_id: scopeParams.team_id }
          : {}),
        ...(Object.keys(tagFilter).length > 0 ? { includeTags: tagFilter } : {}),
      }

      const response = await client.listMemories(params)
      if (fetchId !== fetchIdRef.current) return
      setMemories(response.memories || [])
    } catch (err) {
      if (fetchId !== fetchIdRef.current) return
      console.error('Failed to fetch memories:', err)
      setError(err instanceof Error ? err.message : 'メモリの取得に失敗しました')
    } finally {
      if (fetchId === fetchIdRef.current) {
        setLoading(false)
      }
    }
  }, [scopeFilter, tagFilter, getScopeParams])

  useEffect(() => {
    fetchMemories()
  }, [fetchMemories])

  const handleDelete = async (memoryId: string) => {
    setDeletingIds((prev) => new Set(prev).add(memoryId))
    try {
      const client = createAgentAPIProxyClientFromStorage()
      await client.deleteMemory(memoryId)
      setMemories((prev) => prev.filter((m) => m.id !== memoryId))
      onMemoriesUpdate?.()
    } catch (err) {
      console.error('Failed to delete memory:', err)
      alert(err instanceof Error ? err.message : 'メモリの削除に失敗しました')
    } finally {
      setDeletingIds((prev) => {
        const next = new Set(prev)
        next.delete(memoryId)
        return next
      })
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="flex items-center gap-3 text-gray-500 dark:text-gray-400">
          <svg className="w-5 h-5 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          <span className="text-sm">メモリを読み込み中...</span>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="text-center">
          <div className="w-12 h-12 mx-auto mb-4 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center">
            <svg className="w-6 h-6 text-red-600 dark:text-red-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </div>
          <p className="text-sm text-red-600 dark:text-red-400 mb-3">{error}</p>
          <button
            onClick={fetchMemories}
            className="text-sm text-blue-600 dark:text-blue-400 hover:underline"
          >
            再試行
          </button>
        </div>
      </div>
    )
  }

  if (memories.length === 0) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="text-center">
          <div className="w-12 h-12 mx-auto mb-4 rounded-full bg-gray-100 dark:bg-gray-700 flex items-center justify-center">
            <svg className="w-6 h-6 text-gray-400 dark:text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
            </svg>
          </div>
          <p className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
            {scopeFilter || Object.keys(tagFilter).length > 0
              ? 'フィルター条件に一致するメモリはありません'
              : 'メモリがまだありません'}
          </p>
          <p className="text-xs text-gray-500 dark:text-gray-400">
            右上の「新しいメモリ」ボタンから作成してください
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      {memories.map((memory) => (
        <MemoryCard
          key={memory.id}
          memory={memory}
          onEdit={onMemoryEdit}
          onDelete={handleDelete}
          isDeleting={deletingIds.has(memory.id)}
        />
      ))}
    </div>
  )
}
