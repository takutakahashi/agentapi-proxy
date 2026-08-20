'use client'

import { AlertTriangle } from 'lucide-react'
import { useSettingsScope } from './SettingsScopeContext'
import { pagesForDirtyFields } from './navConfig'

export function SaveBar() {
  const { scopeKind, dirty, dirtyFields, save, discard, saving } = useSettingsScope()

  if (!dirty) return null

  // 設定は 1 つのまとまりとして保存されるため、他ページの変更も一緒に反映される
  const changedPages = pagesForDirtyFields(scopeKind, dirtyFields)

  return (
    <div className="sticky bottom-0 z-30 -mx-4 mt-8 border-t border-gray-200 bg-white/90 px-4 py-3 backdrop-blur dark:border-gray-700 dark:bg-gray-900/90">
      <div className="flex flex-wrap items-center justify-end gap-3">
        <div className="mr-auto flex items-center gap-2 text-sm text-amber-600 dark:text-amber-400">
          <AlertTriangle className="h-4 w-4 flex-shrink-0" />
          <span>
            未保存の変更が {changedPages.length || 1} 件
            {changedPages.length > 0 && (
              <span className="text-gray-500 dark:text-gray-400">（{changedPages.join('、')}）</span>
            )}
          </span>
        </div>
        <button
          type="button"
          onClick={discard}
          disabled={saving}
          className="rounded-md border border-gray-300 px-4 py-1.5 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-100 disabled:opacity-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-800"
        >
          破棄
        </button>
        <button
          type="button"
          onClick={save}
          disabled={saving}
          className="flex items-center gap-2 rounded-md bg-blue-600 px-5 py-1.5 text-sm font-semibold text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {saving && (
            <span className="h-3.5 w-3.5 animate-spin rounded-full border-b-2 border-white" />
          )}
          {saving ? '保存中...' : '保存'}
        </button>
      </div>
    </div>
  )
}
