'use client'

import { FieldGroup, FieldRow, SettingsPageHeader, TextField } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export function GitHubAppSection() {
  const { settings, update } = useSettingsScope()

  return (
    <>
      <SettingsPageHeader
        title="GitHub App 認証"
        description="チームのセッションで使う GitHub App のインストールを指定します。"
      />

      <FieldGroup>
        <FieldRow
          label="Installation ID"
          htmlFor="github-app-installation-id"
          description="セッション開始時に、この installation 用の短期トークンを生成して GITHUB_TOKEN に設定します"
        >
          <TextField
            id="github-app-installation-id"
            value={settings.github_app_installation_id || ''}
            onChange={(github_app_installation_id) => update({ github_app_installation_id })}
            inputMode="numeric"
            pattern="[0-9]*"
            placeholder="12345678"
          />
        </FieldRow>
      </FieldGroup>
    </>
  )
}
