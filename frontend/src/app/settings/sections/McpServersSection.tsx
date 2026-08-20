'use client'

import { APIMCPServerConfig } from '@/types/settings'
import { MCPServerSettings, SettingsPageHeader } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export function McpServersSection() {
  const { settings, update } = useSettingsScope()

  const handleChange = (mcp_servers: Record<string, APIMCPServerConfig>) => update({ mcp_servers })

  return (
    <>
      <SettingsPageHeader
        title="MCP サーバー"
        description="セッションから利用できる Model Context Protocol サーバーを登録します。"
      />
      <MCPServerSettings servers={settings.mcp_servers} onChange={handleChange} />
    </>
  )
}
