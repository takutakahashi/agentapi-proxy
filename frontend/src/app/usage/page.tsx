'use client'

import { useMemo, useState } from 'react'
import { Download, FileArchive } from 'lucide-react'
import NavigationTabs from '../components/NavigationTabs'
import TopBar from '../components/TopBar'
import { useTeamScope } from '../../contexts/TeamScopeContext'

type Range = '7d' | '30d' | '90d'

const ranges: Array<{ value: Range; label: string; days: number }> = [
  { value: '7d', label: '過去7日', days: 7 },
  { value: '30d', label: '過去30日', days: 30 },
  { value: '90d', label: '過去90日', days: 90 },
]

export default function UsageExportPage() {
  const { selectedTeam } = useTeamScope()
  const [range, setRange] = useState<Range>('30d')

  const exportUrl = useMemo(() => {
    const selectedRange = ranges.find((candidate) => candidate.value === range) ?? ranges[1]
    const from = new Date(Date.now() - selectedRange.days * 86_400_000).toISOString()
    const params = new URLSearchParams({ from })
    if (selectedTeam) params.set('team_id', selectedTeam)
    return `/api/proxy/usage/export.parquet?${params}`
  }, [range, selectedTeam])

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-950">
      <TopBar
        title="Usage export"
        subtitle={selectedTeam ? `Team: ${selectedTeam}` : 'Personal usage'}
        showSettingsButton
      >
        <NavigationTabs className="w-44" />
      </TopBar>

      <div className="mx-auto max-w-3xl px-4 py-8 md:px-8">
        <section className="rounded-2xl border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
          <div className="flex items-start gap-4">
            <div className="rounded-xl bg-indigo-50 p-3 text-indigo-600 dark:bg-indigo-950/40 dark:text-indigo-300">
              <FileArchive className="h-6 w-6" />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-gray-900 dark:text-white">Parquetをダウンロード</h2>
              <p className="mt-1 text-sm leading-6 text-gray-500 dark:text-gray-400">
                選択中のスコープと期間に含まれるUsageデータをParquet形式で取得します。
              </p>
            </div>
          </div>

          <fieldset className="mt-6">
            <legend className="text-sm font-medium text-gray-700 dark:text-gray-200">対象期間</legend>
            <div className="mt-2 grid gap-2 sm:grid-cols-3">
              {ranges.map((option) => (
                <button
                  key={option.value}
                  type="button"
                  onClick={() => setRange(option.value)}
                  className={`rounded-xl border px-4 py-3 text-sm font-medium transition-colors ${
                    range === option.value
                      ? 'border-indigo-600 bg-indigo-50 text-indigo-700 dark:bg-indigo-950/40 dark:text-indigo-300'
                      : 'border-gray-200 text-gray-600 hover:border-indigo-300 dark:border-gray-700 dark:text-gray-300'
                  }`}
                  aria-pressed={range === option.value}
                >
                  {option.label}
                </button>
              ))}
            </div>
          </fieldset>

          <div className="mt-6 rounded-xl bg-gray-50 px-4 py-3 text-sm text-gray-600 dark:bg-gray-950/50 dark:text-gray-300">
            出力対象: <span className="font-medium">{selectedTeam ?? 'Personal'}</span>
          </div>

          <a
            href={exportUrl}
            className="mt-6 inline-flex w-full items-center justify-center gap-2 rounded-xl bg-indigo-600 px-5 py-3 text-sm font-semibold text-white transition-colors hover:bg-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 sm:w-auto"
          >
            <Download className="h-4 w-4" />
            Parquetをダウンロード
          </a>

          <p className="mt-4 text-xs leading-5 text-gray-400">
            ファイルの閲覧やSQL分析には、DuckDB、Python、BIツールなどParquet対応のクライアントを使用してください。
          </p>
        </section>
      </div>
    </main>
  )
}
