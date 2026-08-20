'use client'

import Link from 'next/link'
import { MarketplaceConfig } from '@/types/settings'
import { MarketplaceSettings, SettingsPageHeader } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'
import { settingsHref } from '../navConfig'

export function MarketplacesSection() {
  const { scopeKind, scopeId, settings, update } = useSettingsScope()

  const handleChange = (marketplaces: Record<string, MarketplaceConfig>) => update({ marketplaces })
  const pluginsHref = settingsHref(scopeKind, 'plugins', scopeKind === 'team' ? scopeId : undefined)

  return (
    <>
      <SettingsPageHeader
        title="Marketplace"
        description={
          <>
            プラグインの配布元を登録します。登録したマーケットプレイスのプラグインは{' '}
            <Link href={pluginsHref} className="text-blue-600 hover:underline dark:text-blue-400">
              Plugins
            </Link>{' '}
            ページで有効にできます。
          </>
        }
      />
      <MarketplaceSettings marketplaces={settings.marketplaces} onChange={handleChange} />
    </>
  )
}
