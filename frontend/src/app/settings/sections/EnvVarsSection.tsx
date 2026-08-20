'use client'

import { EnvVarsSettings, SettingsPageHeader } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export function EnvVarsSection() {
  const { settings, update } = useSettingsScope()

  const handleChange = (updates: Record<string, string>) =>
    update((prev) => ({ env_vars: { ...(prev.env_vars || {}), ...updates } }))

  return (
    <>
      <SettingsPageHeader
        title="環境変数"
        description="セッションのプロセスに渡す環境変数を設定します。値は保存後は表示されません。"
      />
      <EnvVarsSettings envVarKeys={settings.env_var_keys} onChange={handleChange} />
    </>
  )
}
