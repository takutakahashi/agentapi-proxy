'use client'

import { useCallback, useEffect, useState } from 'react'
import { createCurrentDeploymentAgentAPIProxyClient } from '@/lib/agentapi-proxy-client'
import { AdminSettingsDocument, AdminSettingsVersion } from '@/types/admin-settings'
import { useToast } from '@/contexts/ToastContext'
import {
  ItemList,
  ItemListEmpty,
  ItemListRow,
  RowAction,
  SettingsPageHeader,
  StatusBadge,
} from '@/components/settings'

export default function AdminHistoryPage() {
  const [current, setCurrent] = useState<AdminSettingsDocument | null>(null)
  const [versions, setVersions] = useState<AdminSettingsVersion[]>([])
  const [busy, setBusy] = useState<number | null>(null)
  const { showToast } = useToast()

  const load = useCallback(async () => {
    const client = createCurrentDeploymentAgentAPIProxyClient()
    const [document, history] = await Promise.all([
      client.getAdminSettings(),
      client.listAdminSettingsVersions(),
    ])
    setCurrent(document)
    setVersions(history.versions)
  }, [])

  useEffect(() => {
    load().catch((error) => console.error('Failed to load admin settings history', error))
  }, [load])

  const restore = async (version: number) => {
    if (!current || !window.confirm(`version ${version} の内容を新しい version として復元しますか？`)) return
    setBusy(version)
    try {
      const client = createCurrentDeploymentAgentAPIProxyClient()
      const old = await client.getAdminSettings(version)
      await client.updateAdminSettings({ base_version: current.version, sections: old.sections })
      await load()
      showToast(`version ${version} の内容を復元しました`, 'success')
    } catch {
      showToast('復元に失敗しました', 'error')
    } finally {
      setBusy(null)
    }
  }

  return (
    <>
      <SettingsPageHeader
        title="変更履歴"
        description="更新のたびに、その時点の settings.json 全体が KV Store に保持されます。過去の内容は新しい version として復元できます。"
      />

      <ItemList>
        {versions.length === 0 && <ItemListEmpty>変更履歴がありません</ItemListEmpty>}
        {versions.map((item) => (
          <ItemListRow
            key={item.version}
            name={`version ${item.version}`}
            meta={new Date(item.updated_at).toLocaleString()}
            badges={current?.version === item.version && <StatusBadge tone="green">current</StatusBadge>}
            actions={
              <RowAction
                onClick={() => restore(item.version)}
                disabled={current?.version === item.version || busy !== null}
              >
                {busy === item.version ? '復元中...' : 'この内容を復元'}
              </RowAction>
            }
          />
        ))}
      </ItemList>
    </>
  )
}
