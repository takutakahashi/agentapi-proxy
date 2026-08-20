'use client'

import Link from 'next/link'
import { PluginSettings, SettingsPageHeader } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'
import { settingsHref } from '../navConfig'

export function PluginsSection() {
  const { scopeKind, scopeId, settings, update } = useSettingsScope()

  const marketplacesHref = settingsHref(
    scopeKind,
    'marketplaces',
    scopeKind === 'team' ? scopeId : undefined
  )

  return (
    <>
      <SettingsPageHeader
        title="Plugins"
        description={
          <>
            公式および{' '}
            <Link href={marketplacesHref} className="text-blue-600 hover:underline dark:text-blue-400">
              登録済みマーケットプレイス
            </Link>{' '}
            のプラグインを有効にします。
          </>
        }
      />
      <PluginSettings
        enabledPlugins={settings.enabled_plugins}
        availableMarketplaces={Object.keys(settings.marketplaces || {})}
        onChange={(enabled_plugins) => update({ enabled_plugins })}
      />
    </>
  )
}
