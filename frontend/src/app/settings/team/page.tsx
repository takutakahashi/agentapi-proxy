'use client'

import { useEffect, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { ArrowLeft, Users } from 'lucide-react'
import { createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import { ItemList, ItemListEmpty, ItemListRow, SettingsPageHeader } from '@/components/settings'
import { DEFAULT_SETTINGS_SLUG, settingsHref } from '../navConfig'

export default function TeamSettingsIndexPage() {
  const router = useRouter()
  const [teams, setTeams] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    const loadTeams = async () => {
      try {
        const client = createAgentAPIProxyClientFromStorage()
        const info = await client.getUserInfo()
        if (cancelled) return
        const list = info?.teams ?? []

        // 所属チームが 1 つだけならそのチームの設定を直接開く
        if (list.length === 1) {
          router.replace(settingsHref('team', DEFAULT_SETTINGS_SLUG, list[0]))
          return
        }
        setTeams(list)
        setLoading(false)
      } catch (err) {
        if (cancelled) return
        console.error('Failed to load teams:', err)
        setError('チーム情報の取得に失敗しました')
        setLoading(false)
      }
    }
    loadTeams()
    return () => {
      cancelled = true
    }
  }, [router])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-blue-600" />
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-2xl">
      <Link
        href="/settings/personal/ai-providers"
        className="mb-4 inline-flex items-center gap-1.5 text-sm text-gray-600 transition-colors hover:text-gray-900 dark:text-gray-400 dark:hover:text-white"
      >
        <ArrowLeft className="h-4 w-4" />
        パーソナル設定に戻る
      </Link>

      <SettingsPageHeader
        title="チームを選択"
        description="設定を表示するチームを選んでください。"
      />

      {error && (
        <div className="mb-5 rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-800 dark:bg-red-900/20">
          <p className="text-sm text-red-600 dark:text-red-400">{error}</p>
        </div>
      )}

      <ItemList>
        {teams.length === 0 && !error && (
          <ItemListEmpty>所属しているチームがありません</ItemListEmpty>
        )}
        {teams.map((team) => (
          <ItemListRow
            key={team}
            name={
              <span className="flex items-center gap-2">
                <Users className="h-4 w-4 text-gray-500 dark:text-gray-400" />
                {team}
              </span>
            }
            actions={
              <Link
                href={settingsHref('team', DEFAULT_SETTINGS_SLUG, team)}
                className="rounded-md border border-gray-300 px-3 py-1 text-xs text-gray-600 transition-colors hover:bg-gray-100 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700"
              >
                設定を開く
              </Link>
            }
          />
        ))}
      </ItemList>
    </div>
  )
}
