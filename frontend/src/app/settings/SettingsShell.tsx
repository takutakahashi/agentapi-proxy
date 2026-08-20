'use client'

import Link from 'next/link'
import { usePathname, useRouter } from 'next/navigation'
import { useEffect, useMemo, useState } from 'react'
import { ArrowLeft, PanelLeft, X } from 'lucide-react'
import { SideNav, SideNavGroup } from '@/components/settings/ui/SideNav'
import { ScopeSwitcher } from './ScopeSwitcher'
import { SaveBar } from './SaveBar'
import { SettingsScopeProvider, useSettingsScope } from './SettingsScopeContext'
import {
  DEFAULT_SETTINGS_SLUG,
  SettingsScopeKind,
  navItemsForScope,
  settingsHref,
} from './navConfig'

interface ResolvedScope {
  scopeKind: SettingsScopeKind
  teamId?: string
  slug: string
}

/** /settings/personal/xxx と /settings/team/<team>/xxx からスコープを読み取る */
const resolveScope = (pathname: string): ResolvedScope | null => {
  const segments = pathname.split('/').filter(Boolean) // ['settings', ...]
  if (segments[0] !== 'settings') return null

  if (segments[1] === 'personal') {
    return { scopeKind: 'personal', slug: segments[2] ?? DEFAULT_SETTINGS_SLUG }
  }
  if (segments[1] === 'team' && segments[2]) {
    return {
      scopeKind: 'team',
      teamId: decodeURIComponent(segments[2]),
      slug: segments[3] ?? DEFAULT_SETTINGS_SLUG,
    }
  }
  return null
}

const buildGroups = (
  scope: ResolvedScope,
  dirtyFields: string[]
): SideNavGroup[] => {
  const dirty = new Set(dirtyFields)
  const groups: SideNavGroup[] = []

  for (const item of navItemsForScope(scope.scopeKind)) {
    const existing = groups.find((group) => group.title === item.group)
    const entry = {
      href: settingsHref(scope.scopeKind, item.slug, scope.teamId),
      label: item.label,
      icon: item.icon,
      dirty: item.fields.some((field) => dirty.has(field)),
    }
    if (existing) {
      existing.items.push(entry)
    } else {
      groups.push({ title: item.group, items: [entry] })
    }
  }
  return groups
}

/** スコープの状態を読んでサイドバーと保存バーを描画する内側のシェル */
function ScopedShell({ scope, children }: { scope: ResolvedScope; children: React.ReactNode }) {
  const pathname = usePathname()
  const router = useRouter()
  const { dirty, dirtyFields, userName, userTeams } = useSettingsScope()
  const [drawerOpen, setDrawerOpen] = useState(false)

  useEffect(() => {
    setDrawerOpen(false)
  }, [pathname])

  const groups = buildGroups(scope, dirtyFields as string[])
  const navHrefs = useMemo(
    () =>
      navItemsForScope(scope.scopeKind).map((item) =>
        settingsHref(scope.scopeKind, item.slug, scope.teamId)
      ),
    [scope.scopeKind, scope.teamId]
  )
  const activeLabel = groups
    .flatMap((group) => group.items)
    .find((item) => item.href === pathname)?.label

  // 設定項目はそれぞれ別ルートなので、初回クリック時にページの JS を取得すると
  // 遷移が重く感じられる。ブラウザが暇になった時点で同一スコープの各ページを
  // 明示的に先読みし、設定を開いている間の項目切り替えを即座に行えるようにする。
  useEffect(() => {
    const prefetch = () => {
      for (const href of navHrefs) {
        if (href !== pathname) router.prefetch(href)
      }
    }

    if ('requestIdleCallback' in window) {
      const idleId = window.requestIdleCallback(prefetch, { timeout: 1500 })
      return () => window.cancelIdleCallback(idleId)
    }

    const timeoutId = globalThis.setTimeout(prefetch, 0)
    return () => globalThis.clearTimeout(timeoutId)
  }, [pathname, router, navHrefs])

  // スコープをまたぐ移動だけ確認する。同じスコープ内は編集内容が保持される
  const confirmLeave = () =>
    !dirty || confirm('未保存の変更があります。破棄して移動しますか？')

  const sidebarHeader = (
    <ScopeSwitcher
      scopeKind={scope.scopeKind}
      teamId={scope.teamId}
      userName={userName}
      userTeams={userTeams}
      currentSlug={scope.slug}
      confirmLeave={confirmLeave}
    />
  )

  return (
    <>
      <div className="mb-4 flex items-center gap-3">
        <button
          type="button"
          onClick={() => setDrawerOpen(true)}
          className="rounded-md p-2 text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800 md:hidden"
          aria-label="設定メニューを開く"
        >
          <PanelLeft className="h-5 w-5" />
        </button>
        <Link
          href="/chats"
          onClick={(e) => {
            if (!confirmLeave()) e.preventDefault()
          }}
          className="inline-flex items-center gap-1.5 text-sm text-gray-600 transition-colors hover:text-gray-900 dark:text-gray-400 dark:hover:text-white"
        >
          <ArrowLeft className="h-4 w-4" />
          チャットに戻る
        </Link>
        {activeLabel && (
          <span className="ml-auto text-sm font-medium text-gray-900 dark:text-white md:hidden">
            {activeLabel}
          </span>
        )}
      </div>

      <div className="flex gap-8">
        <div className="hidden md:block">
          <SideNav groups={groups} activeHref={pathname} header={sidebarHeader} />
        </div>
        <div className="min-w-0 flex-1">
          {children}
          <SaveBar />
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
              <span className="text-sm font-semibold text-gray-900 dark:text-white">設定</span>
              <button
                type="button"
                onClick={() => setDrawerOpen(false)}
                className="rounded-md p-1 text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800"
                aria-label="閉じる"
              >
                <X className="h-5 w-5" />
              </button>
            </div>
            <SideNav groups={groups} activeHref={pathname} header={sidebarHeader} />
          </div>
        </div>
      )}
    </>
  )
}

export function SettingsShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const scope = resolveScope(pathname)

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-950">
      <div className="container mx-auto max-w-6xl px-4 py-6">
        {scope ? (
          // key でスコープが変わったときだけ編集状態をリセットする
          <SettingsScopeProvider
            key={`${scope.scopeKind}:${scope.teamId ?? ''}`}
            scopeKind={scope.scopeKind}
            teamId={scope.teamId}
          >
            <ScopedShell scope={scope}>{children}</ScopedShell>
          </SettingsScopeProvider>
        ) : (
          children
        )}
      </div>
    </main>
  )
}
