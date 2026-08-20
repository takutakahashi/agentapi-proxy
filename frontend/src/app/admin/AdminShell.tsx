'use client'

import Link from 'next/link'
import { usePathname, useRouter } from 'next/navigation'
import { useEffect, useState } from 'react'
import { ArrowLeft, PanelLeft, X } from 'lucide-react'
import { createCurrentDeploymentAgentAPIProxyClient } from '@/lib/agentapi-proxy-client'
import { SideNav, SideNavGroup } from '@/components/settings/ui/SideNav'
import { adminNavExtras, adminSections } from './config'

const buildGroups = (): SideNavGroup[] => {
  const groups: SideNavGroup[] = []
  const push = (title: string, item: SideNavGroup['items'][number]) => {
    const existing = groups.find((group) => group.title === title)
    if (existing) existing.items.push(item)
    else groups.push({ title, items: [item] })
  }

  for (const section of adminSections) {
    push(section.group, {
      href: `/admin/${section.id}`,
      label: section.title,
      icon: section.icon,
    })
  }
  for (const extra of adminNavExtras) {
    push(extra.group, { href: extra.href, label: extra.title, icon: extra.icon })
  }
  return groups
}

export function AdminShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const router = useRouter()
  const [allowed, setAllowed] = useState<boolean | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)

  useEffect(() => {
    createCurrentDeploymentAgentAPIProxyClient()
      .getUserInfo()
      .then((user) => setAllowed(user?.is_admin === true))
      .catch(() => setAllowed(false))
  }, [])

  useEffect(() => {
    setDrawerOpen(false)
  }, [pathname])

  if (allowed === null) {
    return (
      <main className="min-h-dvh bg-gray-50 dark:bg-gray-950">
        <div className="flex items-center justify-center py-24">
          <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-blue-600" />
        </div>
      </main>
    )
  }

  if (!allowed) {
    return (
      <main className="min-h-dvh bg-gray-50 dark:bg-gray-950">
        <div className="mx-auto max-w-xl px-4 py-24 text-center">
          <h1 className="text-2xl font-semibold text-gray-900 dark:text-white">アクセスできません</h1>
          <p className="mt-3 text-sm text-gray-600 dark:text-gray-400">
            このページには管理者権限が必要です。
          </p>
          <button
            type="button"
            onClick={() => router.push('/chats')}
            className="mt-6 rounded-md bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition-colors hover:bg-blue-700"
          >
            チャットに戻る
          </button>
        </div>
      </main>
    )
  }

  const groups = buildGroups()
  const activeLabel = groups
    .flatMap((group) => group.items)
    .find((item) => item.href === pathname)?.label

  const navHeader = (
    <div className="rounded-lg border border-gray-300 bg-white px-2.5 py-2 dark:border-gray-700 dark:bg-gray-900">
      <span className="block text-sm font-semibold text-gray-900 dark:text-white">システム設定</span>
      <span className="block text-[10px] leading-tight text-gray-500 dark:text-gray-400">
        Administration
      </span>
    </div>
  )

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-950">
      <div className="container mx-auto max-w-6xl px-4 py-6">
        <div className="mb-4 flex items-center gap-3">
          <button
            type="button"
            onClick={() => setDrawerOpen(true)}
            className="rounded-md p-2 text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800 md:hidden"
            aria-label="管理メニューを開く"
          >
            <PanelLeft className="h-5 w-5" />
          </button>
          <Link
            href="/chats"
            className="inline-flex items-center gap-1.5 text-sm text-gray-600 transition-colors hover:text-gray-900 dark:text-gray-400 dark:hover:text-white"
          >
            <ArrowLeft className="h-4 w-4" />
            チャットに戻る
          </Link>
          <Link
            href="/settings"
            className="ml-auto hidden text-sm text-blue-600 hover:underline dark:text-blue-400 md:inline"
          >
            アカウント設定へ
          </Link>
          {activeLabel && (
            <span className="ml-auto text-sm font-medium text-gray-900 dark:text-white md:hidden">
              {activeLabel}
            </span>
          )}
        </div>

        <div className="flex gap-8">
          <div className="hidden md:block">
            <SideNav
              groups={groups}
              activeHref={pathname}
              header={navHeader}
              ariaLabel="システム設定"
            />
          </div>
          <div className="min-w-0 flex-1">{children}</div>
        </div>
      </div>

      {drawerOpen && (
        <div className="fixed inset-0 z-50 md:hidden" role="dialog" aria-modal="true">
          <div
            className="absolute inset-0 bg-black bg-opacity-50"
            onClick={() => setDrawerOpen(false)}
          />
          <div className="absolute left-0 top-0 h-full w-72 overflow-y-auto bg-white p-4 shadow-xl dark:bg-gray-900">
            <div className="mb-3 flex items-center justify-between">
              <span className="text-sm font-semibold text-gray-900 dark:text-white">システム設定</span>
              <button
                type="button"
                onClick={() => setDrawerOpen(false)}
                className="rounded-md p-1 text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800"
                aria-label="閉じる"
              >
                <X className="h-5 w-5" />
              </button>
            </div>
            <SideNav
              groups={groups}
              activeHref={pathname}
              header={navHeader}
              ariaLabel="システム設定"
            />
            <Link
              href="/settings"
              className="mt-4 block text-sm text-blue-600 hover:underline dark:text-blue-400"
            >
              アカウント設定へ
            </Link>
          </div>
        </div>
      )}
    </main>
  )
}
