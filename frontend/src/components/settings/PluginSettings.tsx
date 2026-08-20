'use client'

import { useState } from 'react'

// 公式プラグインのマーケットプレイス名
const OFFICIAL_MARKETPLACE = 'claude-plugins-official'

interface PluginSettingsProps {
  enabledPlugins: string[] | undefined  // "plugin@marketplace" 形式
  availableMarketplaces: string[]       // 登録済みマーケットプレイス名
  onChange: (plugins: string[]) => void
}

// プラグイン文字列をパース
function parsePlugin(pluginStr: string): { name: string; marketplace: string } {
  const atIndex = pluginStr.lastIndexOf('@')
  if (atIndex === -1) {
    return { name: pluginStr, marketplace: '' }
  }
  return {
    name: pluginStr.substring(0, atIndex),
    marketplace: pluginStr.substring(atIndex + 1),
  }
}

// プラグイン文字列を生成
function formatPlugin(name: string, marketplace: string): string {
  return `${name}@${marketplace}`
}

export function PluginSettings({ enabledPlugins, availableMarketplaces, onChange }: PluginSettingsProps) {
  const [newPluginName, setNewPluginName] = useState('')
  const [selectedMarketplace, setSelectedMarketplace] = useState(OFFICIAL_MARKETPLACE)
  const [error, setError] = useState<string | null>(null)

  const pluginList = enabledPlugins || []

  // 公式プラグインと他のマーケットプレイスのプラグインを分離
  const officialPlugins = pluginList.filter(p => {
    const { marketplace } = parsePlugin(p)
    return marketplace === OFFICIAL_MARKETPLACE
  })
  const marketplacePlugins = pluginList.filter(p => {
    const { marketplace } = parsePlugin(p)
    return marketplace !== OFFICIAL_MARKETPLACE
  })

  // 利用可能なマーケットプレイス（公式 + 登録済み）
  const allMarketplaces = [OFFICIAL_MARKETPLACE, ...availableMarketplaces]

  const handleAdd = () => {
    setError(null)

    const trimmed = newPluginName.trim()
    if (!trimmed) {
      setError('Plugin name is required')
      return
    }

    const newPlugin = formatPlugin(trimmed, selectedMarketplace)

    if (pluginList.includes(newPlugin)) {
      setError('This plugin is already added')
      return
    }

    onChange([...pluginList, newPlugin])
    setNewPluginName('')
  }

  const handleDelete = (pluginStr: string) => {
    onChange(pluginList.filter(p => p !== pluginStr))
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      handleAdd()
    }
  }

  return (
    <div className="space-y-6">
      <div>
        {/* Official Plugins Section */}
        <div className="mb-6">
          <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3 flex items-center gap-2">
            <span>Official Plugins</span>
            <span className="text-xs font-normal text-gray-500 dark:text-gray-400">
              ({OFFICIAL_MARKETPLACE})
            </span>
          </h4>
          {officialPlugins.length === 0 ? (
            <div className="text-center py-4 text-gray-500 dark:text-gray-400 bg-gray-50 dark:bg-gray-800/50 rounded-lg">
              <p className="text-sm">No official plugins enabled</p>
            </div>
          ) : (
            <div className="space-y-2">
              {officialPlugins.map((pluginStr) => {
                const { name } = parsePlugin(pluginStr)
                return (
                  <div
                    key={pluginStr}
                    className="flex items-center justify-between border border-gray-200 dark:border-gray-700 rounded-lg px-4 py-3 bg-white dark:bg-gray-800"
                  >
                    <div className="flex items-center gap-2">
                      <span className="text-lg">🔌</span>
                      <span className="font-medium text-gray-900 dark:text-white">
                        {name}
                      </span>
                    </div>
                    <button
                      type="button"
                      onClick={() => handleDelete(pluginStr)}
                      className="text-sm font-medium text-red-600 hover:text-red-800 dark:text-red-400 dark:hover:text-red-300"
                    >
                      Delete
                    </button>
                  </div>
                )
              })}
            </div>
          )}
        </div>

        {/* Marketplace Plugins Section */}
        <div className="mb-6">
          <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3">
            Marketplace Plugins
          </h4>
          {marketplacePlugins.length === 0 ? (
            <div className="text-center py-4 text-gray-500 dark:text-gray-400 bg-gray-50 dark:bg-gray-800/50 rounded-lg">
              <p className="text-sm">No marketplace plugins enabled</p>
              {availableMarketplaces.length === 0 && (
                <p className="text-xs mt-1 text-gray-400 dark:text-gray-500">
                  Add marketplaces first to enable plugins from them
                </p>
              )}
            </div>
          ) : (
            <div className="space-y-2">
              {marketplacePlugins.map((pluginStr) => {
                const { name, marketplace } = parsePlugin(pluginStr)
                return (
                  <div
                    key={pluginStr}
                    className="flex items-center justify-between border border-gray-200 dark:border-gray-700 rounded-lg px-4 py-3 bg-white dark:bg-gray-800"
                  >
                    <div className="flex items-center gap-2 min-w-0 flex-1">
                      <span className="text-lg">🔌</span>
                      <div className="min-w-0">
                        <span className="font-medium text-gray-900 dark:text-white block truncate">
                          {name}
                        </span>
                        <span className="text-xs text-gray-500 dark:text-gray-400 block truncate">
                          @{marketplace}
                        </span>
                      </div>
                    </div>
                    <button
                      type="button"
                      onClick={() => handleDelete(pluginStr)}
                      className="text-sm font-medium ml-2 text-red-600 hover:text-red-800 dark:text-red-400 dark:hover:text-red-300"
                    >
                      Delete
                    </button>
                  </div>
                )
              })}
            </div>
          )}
        </div>

        {/* Add Plugin Input */}
        <div className="border-t border-gray-200 dark:border-gray-700 pt-4">
          <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3">
            Add Plugin
          </h4>
          {error && (
            <div className="mb-3 p-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
              <p className="text-red-600 dark:text-red-400 text-sm">{error}</p>
            </div>
          )}
          <div className="space-y-3">
            {/* Marketplace Selector */}
            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                Marketplace
              </label>
              <select
                value={selectedMarketplace}
                onChange={(e) => setSelectedMarketplace(e.target.value)}
                className="w-full border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
              >
                {allMarketplaces.map((mp) => (
                  <option key={mp} value={mp}>
                    {mp === OFFICIAL_MARKETPLACE ? `${mp} (Official)` : mp}
                  </option>
                ))}
              </select>
            </div>

            {/* Plugin Name Input */}
            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                Plugin Name
              </label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={newPluginName}
                  onChange={(e) => {
                    setNewPluginName(e.target.value)
                    setError(null)
                  }}
                  onKeyDown={handleKeyDown}
                  placeholder="Enter plugin name"
                  className="flex-1 border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
                <button
                  type="button"
                  onClick={handleAdd}
                  disabled={!newPluginName.trim()}
                  className={`px-4 py-2 rounded-md transition-colors ${
                    !newPluginName.trim()
                      ? 'bg-gray-300 dark:bg-gray-600 text-gray-500 dark:text-gray-400 cursor-not-allowed'
                      : 'bg-blue-600 text-white hover:bg-blue-700'
                  }`}
                >
                  + Add
                </button>
              </div>
              {newPluginName.trim() && (
                <p className="mt-1 text-xs text-blue-600 dark:text-blue-400">
                  Will add: <span className="font-medium">{formatPlugin(newPluginName.trim(), selectedMarketplace)}</span>
                </p>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
