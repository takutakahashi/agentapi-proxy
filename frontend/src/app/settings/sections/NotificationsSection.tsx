'use client'

import { SettingsPageHeader, SlackSettings } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export function NotificationsSection() {
  const { settings, update } = useSettingsScope()

  return (
    <>
      <SettingsPageHeader
        title="通知"
        description="セッションの完了や確認待ちをどこで受け取るかを設定します。"
      />
      <SlackSettings
        slackUserId={settings.slack_user_id || ''}
        notificationChannels={settings.notification_channels}
        onSlackUserIdChange={(slack_user_id) => update({ slack_user_id })}
        onChannelsChange={(notification_channels) => update({ notification_channels })}
      />
    </>
  )
}
