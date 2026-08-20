'use client'

import { CodexCredentialsSettings } from '@/components/settings/CodexCredentialsSettings'
import { ImmediateSaveNotice, SettingsPageHeader } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export function CodexAuthSection() {
  const {
    scopeKind,
    scopeId,
    credentialsMetadata,
    reloadCredentials,
    clearCredentialsMetadata,
  } = useSettingsScope()

  return (
    <>
      <SettingsPageHeader
        title="Codex 認証"
        description={
          <>
            Codex が使う認証情報ファイル <code className="rounded bg-gray-100 px-1 dark:bg-gray-800">~/.codex/auth.json</code>{' '}
            を登録します。登録するとセッション開始時に自動で配置されます。
          </>
        }
      />

      <ImmediateSaveNotice />

      <CodexCredentialsSettings
        scope={scopeKind === 'personal' ? 'user' : 'team'}
        scopeName={scopeId}
        teamId={scopeKind === 'team' ? scopeId : undefined}
        credentialsMetadata={credentialsMetadata}
        onChanged={reloadCredentials}
        onDeleted={clearCredentialsMetadata}
      />
    </>
  )
}
