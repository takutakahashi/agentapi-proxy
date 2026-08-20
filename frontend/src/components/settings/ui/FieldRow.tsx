'use client'

interface FieldRowProps {
  label: string
  description?: string
  htmlFor?: string
  /** 右側のコントロール。省略すると children を下段に広く配置する */
  control?: React.ReactNode
  children?: React.ReactNode
}

/**
 * 設定 1 項目分の行。左にラベルと説明、右にコントロールを置く。
 * 幅の要る入力は control ではなく children に渡すと下段いっぱいに広がる。
 */
export function FieldRow({ label, description, htmlFor, control, children }: FieldRowProps) {
  return (
    <div className="py-4">
      <div className="flex items-start justify-between gap-6">
        <div className="min-w-0">
          <label
            htmlFor={htmlFor}
            className="block text-sm font-medium text-gray-900 dark:text-white"
          >
            {label}
          </label>
          {description && (
            <p className="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{description}</p>
          )}
        </div>
        {control && <div className="flex-shrink-0">{control}</div>}
      </div>
      {children && <div className="mt-3">{children}</div>}
    </div>
  )
}

/** FieldRow を縦に並べて区切り線を入れるコンテナ */
export function FieldGroup({ children }: { children: React.ReactNode }) {
  return <div className="divide-y divide-gray-200 dark:divide-gray-700">{children}</div>
}
