'use client'

import { FileSettings, ImmediateSaveNotice, SettingsPageHeader } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export function FilesSection() {
  const { userName } = useSettingsScope()

  return (
    <>
      <SettingsPageHeader
        title="セッションファイル"
        description="SSH 鍵などのファイルを、セッション起動時に指定したパスへ自動配置します。"
      />
      <ImmediateSaveNotice />
      <FileSettings userName={userName} />
    </>
  )
}
