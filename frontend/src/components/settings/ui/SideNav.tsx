'use client'

import Link from 'next/link'
import { LucideIcon } from 'lucide-react'

export interface SideNavItem {
  href: string
  label: string
  icon: LucideIcon
  /** 未保存の変更があることを示すドットを出す */
  dirty?: boolean
}

export interface SideNavGroup {
  /** 見出し。空文字ならグループ見出しなしで先頭に並ぶ */
  title: string
  items: SideNavItem[]
}

interface SideNavProps {
  groups: SideNavGroup[]
  /** 現在表示中のパス */
  activeHref: string
  /** ナビ最上部に置くスコープスイッチャーなど */
  header?: React.ReactNode
  /** リンク遷移前に確認する。false を返すと遷移をキャンセルする */
  onNavigate?: (href: string) => boolean
  ariaLabel?: string
  className?: string
}

export function SideNav({
  groups,
  activeHref,
  header,
  onNavigate,
  ariaLabel = '設定',
  className = '',
}: SideNavProps) {
  return (
    <nav className={`w-full flex-shrink-0 md:w-60 ${className}`} aria-label={ariaLabel}>
      {header}
      <div className="mt-3">
        {groups.map((group, groupIndex) => (
          <div
            key={group.title || `group-${groupIndex}`}
            className={groupIndex > 0 ? 'mt-2 pt-2 border-t border-gray-200 dark:border-gray-700' : ''}
          >
            {group.title && (
              <p className="px-3 pt-2 pb-1 text-xs font-semibold text-gray-500 dark:text-gray-400">
                {group.title}
              </p>
            )}
            <ul>
              {group.items.map((item) => {
                const isActive = activeHref === item.href
                const Icon = item.icon
                return (
                  <li key={item.href} className="relative">
                    {isActive && (
                      <span
                        aria-hidden="true"
                        className="absolute left-0 top-1 bottom-1 w-[3px] rounded-full bg-blue-500"
                      />
                    )}
                    <Link
                      href={item.href}
                      aria-current={isActive ? 'page' : undefined}
                      onClick={(e) => {
                        if (onNavigate && !onNavigate(item.href)) {
                          e.preventDefault()
                        }
                      }}
                      className={`flex items-center gap-2.5 rounded-md px-3 py-1.5 text-sm transition-colors ${
                        isActive
                          ? 'bg-gray-200/60 dark:bg-gray-800 font-semibold text-gray-900 dark:text-white'
                          : 'text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800'
                      }`}
                    >
                      <Icon
                        className={`h-4 w-4 flex-shrink-0 ${
                          isActive ? 'text-gray-900 dark:text-white' : 'text-gray-500 dark:text-gray-400'
                        }`}
                      />
                      <span className="truncate">{item.label}</span>
                      {item.dirty && (
                        <span
                          title="未保存の変更があります"
                          className="ml-auto h-1.5 w-1.5 flex-shrink-0 rounded-full bg-blue-500"
                        />
                      )}
                    </Link>
                  </li>
                )
              })}
            </ul>
          </div>
        ))}
      </div>
    </nav>
  )
}
