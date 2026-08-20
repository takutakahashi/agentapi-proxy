'use client'

import { ESMRegistrationToken, SettingsPageHeader, SettingsSubsection } from '@/components/settings'
import { ExternalSessionManagerList } from '@/components/settings/ExternalSessionManagerList'
import { useSettingsScope } from '../SettingsScopeContext'

export function SessionManagersSection() {
  const {
    scopeKind,
    scopeId,
    settings,
    update,
    revealedTokens,
    regenerateEsmToken,
    regeneratingEsmId,
  } = useSettingsScope()

  return (
    <>
      <SettingsPageHeader
        title="セッションマネージャー"
        description="外部のセッションマネージャーを登録し、接続に使うトークンを発行します。"
      />

      <SettingsSubsection
        title="新しいマネージャーを登録"
        description="発行した登録トークンを External Session Manager に設定すると接続されます"
      >
        <ESMRegistrationToken
          scope={scopeKind === 'personal' ? 'user' : 'team'}
          teamId={scopeKind === 'team' ? scopeId : undefined}
        />
      </SettingsSubsection>

      <SettingsSubsection title="登録済みのマネージャー">
        <ExternalSessionManagerList
          managers={settings.external_session_managers ?? []}
          onChange={(external_session_managers) => update({ external_session_managers })}
          revealedTokens={revealedTokens}
          onRegenerate={regenerateEsmToken}
          regeneratingEsmId={regeneratingEsmId}
        />
      </SettingsSubsection>
    </>
  )
}
