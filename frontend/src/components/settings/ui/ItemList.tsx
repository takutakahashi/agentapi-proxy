'use client'

/** 一覧を囲む枠線ボックス。行は divide-y で区切られる */
export function ItemList({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-gray-200 dark:border-gray-700 divide-y divide-gray-200 dark:divide-gray-700 overflow-hidden">
      {children}
    </div>
  )
}

interface ItemListRowProps {
  /** 行の主題（サーバー名、ファイル名など） */
  name: React.ReactNode
  /** 名前の下に置くメタ情報。等幅で表示される */
  meta?: React.ReactNode
  /** 名前の右に並ぶ状態バッジ */
  badges?: React.ReactNode
  /** 行の右端に並ぶ操作 */
  actions?: React.ReactNode
  /** 行の下に広がる追加コンテンツ（編集フォームなど） */
  children?: React.ReactNode
}

export function ItemListRow({ name, meta, badges, actions, children }: ItemListRowProps) {
  return (
    <div className="px-4 py-3">
      <div className="flex items-center gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-semibold text-gray-900 dark:text-white truncate">
              {name}
            </span>
            {badges}
          </div>
          {meta && (
            <div className="mt-0.5 text-xs font-mono text-gray-500 dark:text-gray-400 truncate">
              {meta}
            </div>
          )}
        </div>
        {actions && <div className="flex flex-shrink-0 items-center gap-1.5">{actions}</div>}
      </div>
      {children}
    </div>
  )
}

/** 一覧が空のときに枠内へ出すメッセージ */
export function ItemListEmpty({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-4 py-6 text-center text-sm text-gray-500 dark:text-gray-400">
      {children}
    </div>
  )
}

type BadgeTone = 'neutral' | 'blue' | 'green' | 'amber' | 'violet'

const badgeToneClass: Record<BadgeTone, string> = {
  neutral: 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300',
  blue: 'bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300',
  green: 'bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300',
  amber: 'bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300',
  violet: 'bg-violet-100 dark:bg-violet-900/40 text-violet-700 dark:text-violet-300',
}

export function StatusBadge({ tone = 'neutral', children }: { tone?: BadgeTone; children: React.ReactNode }) {
  return (
    <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${badgeToneClass[tone]}`}>
      {children}
    </span>
  )
}

type ActionTone = 'default' | 'danger'

interface RowActionProps {
  onClick: () => void
  tone?: ActionTone
  disabled?: boolean
  title?: string
  children: React.ReactNode
}

export function RowAction({ onClick, tone = 'default', disabled, title, children }: RowActionProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={title}
      className={`rounded-md border px-2 py-1 text-xs transition-colors disabled:opacity-50 disabled:cursor-not-allowed ${
        tone === 'danger'
          ? 'border-red-200 dark:border-red-800 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20'
          : 'border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'
      }`}
    >
      {children}
    </button>
  )
}
