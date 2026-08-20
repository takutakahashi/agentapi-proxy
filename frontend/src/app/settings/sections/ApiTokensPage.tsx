'use client'

import ApiTokensSection from '@/app/components/ApiTokensSection'
import { ImmediateSaveNotice, SettingsPageHeader } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export function ApiTokensPage() {
  const { scopeKind, scopeId } = useSettingsScope()

  return (
    <>
      <SettingsPageHeader
        title="API トークン"
        description={
          scopeKind === 'team'
            ? 'チーム用の API トークンを管理します。トークンは作成時に一度だけ表示されます。'
            : 'パーソナル API トークンを管理します。トークンは作成時に一度だけ表示されます。'
        }
      />
      <ImmediateSaveNotice />
      <ApiTokensSection
        scope={scopeKind === 'team' ? 'team' : 'personal'}
        teamId={scopeKind === 'team' ? scopeId : undefined}
      />
    </>
  )
}
