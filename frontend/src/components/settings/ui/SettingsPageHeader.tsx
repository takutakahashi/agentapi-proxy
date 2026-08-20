'use client'

interface SettingsPageHeaderProps {
  title: string
  description?: React.ReactNode
  /** 見出し右端のプライマリ操作（「追加」ボタンなど） */
  action?: React.ReactNode
}

export function SettingsPageHeader({ title, description, action }: SettingsPageHeaderProps) {
  return (
    <div className="mb-5">
      <div className="flex items-center justify-between gap-4 border-b border-gray-200 dark:border-gray-700 pb-2">
        <h2 className="text-2xl font-semibold text-gray-900 dark:text-white">{title}</h2>
        {action && <div className="flex-shrink-0">{action}</div>}
      </div>
      {description && (
        <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">{description}</p>
      )}
    </div>
  )
}

/** ページ内をさらに分けるときの小見出し */
export function SettingsSubsection({
  title,
  description,
  children,
}: {
  title: string
  description?: string
  children: React.ReactNode
}) {
  return (
    <section className="mt-8 first:mt-0">
      <h3 className="text-base font-semibold text-gray-900 dark:text-white">{title}</h3>
      {description && (
        <p className="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{description}</p>
      )}
      <div className="mt-3">{children}</div>
    </section>
  )
}

/** 即時反映される操作であることを伝える注記 */
export function ImmediateSaveNotice({ children }: { children?: React.ReactNode }) {
  return (
    <p className="mb-4 rounded-md border border-blue-200 dark:border-blue-900 bg-blue-50 dark:bg-blue-900/20 px-3 py-2 text-xs text-blue-700 dark:text-blue-300">
      {children ?? 'このページの操作は保存ボタンを押さずにすぐ反映されます。'}
    </p>
  )
}

/** 破壊的な操作をまとめる枠 */
export function DangerZone({ title = 'Danger Zone', children }: { title?: string; children: React.ReactNode }) {
  return (
    <section className="mt-10 rounded-lg border border-red-200 dark:border-red-900 overflow-hidden">
      <h3 className="border-b border-red-200 dark:border-red-900 bg-red-50 dark:bg-red-900/20 px-4 py-2 text-sm font-semibold text-red-700 dark:text-red-400">
        {title}
      </h3>
      <div className="px-4 py-3">{children}</div>
    </section>
  )
}
