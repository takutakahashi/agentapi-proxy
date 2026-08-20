'use client'

import { useState } from 'react'
import { APIMCPServerConfig } from '@/types/settings'

interface MCPServerSettingsProps {
  servers: Record<string, APIMCPServerConfig> | undefined
  onChange: (servers: Record<string, APIMCPServerConfig>) => void
}

interface EditingServer {
  name: string
  config: APIMCPServerConfig
  isNew: boolean
}

const getDefaultServerConfig = (): APIMCPServerConfig => ({
  type: 'stdio',
  command: '',
  args: [],
  env: {},
})

export function MCPServerSettings({ servers, onChange }: MCPServerSettingsProps) {
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [editingServer, setEditingServer] = useState<EditingServer | null>(null)

  const serverEntries = Object.entries(servers || {})

  const handleAdd = () => {
    setEditingServer({
      name: '',
      config: getDefaultServerConfig(),
      isNew: true,
    })
    setIsModalOpen(true)
  }

  const handleEdit = (name: string) => {
    const config = servers?.[name]
    if (config) {
      setEditingServer({
        name,
        config: { ...config },
        isNew: false,
      })
      setIsModalOpen(true)
    }
  }

  const handleDelete = (name: string) => {
    if (confirm(`Are you sure you want to delete "${name}"?`)) {
      const newServers = { ...servers }
      delete newServers[name]
      onChange(newServers)
    }
  }

  const handleModalSave = (name: string, config: APIMCPServerConfig) => {
    const newServers = { ...servers }

    // 名前が変更された場合は古いエントリを削除
    if (editingServer && !editingServer.isNew && editingServer.name !== name) {
      delete newServers[editingServer.name]
    }

    newServers[name] = config
    onChange(newServers)
    setIsModalOpen(false)
    setEditingServer(null)
  }

  const handleModalClose = () => {
    setIsModalOpen(false)
    setEditingServer(null)
  }

  const getServerIcon = (type: APIMCPServerConfig['type']) => {
    switch (type) {
      case 'stdio':
        return '📦'
      case 'http':
        return '🌐'
      case 'sse':
        return '📡'
      default:
        return '⚙️'
    }
  }

  const getServerDescription = (config: APIMCPServerConfig) => {
    if (config.type === 'stdio') {
      if (config.command) {
        const args = config.args?.join(' ') || ''
        return `${config.command} ${args}`.trim()
      }
      return 'No command configured'
    }
    return config.url || 'No URL configured'
  }

  return (
    <div className="space-y-4">
      {serverEntries.length === 0 ? (
        <div className="text-center py-8 text-gray-500 dark:text-gray-400">
          <p>No MCP servers configured</p>
          <p className="text-sm mt-1">Click the button below to add your first MCP server</p>
        </div>
      ) : (
        <div className="space-y-3">
          {serverEntries.map(([name, config]) => (
            <div
              key={name}
              className="border border-gray-200 dark:border-gray-700 rounded-lg p-4 bg-white dark:bg-gray-800"
            >
              <div className="flex items-start justify-between">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-lg">{getServerIcon(config.type)}</span>
                    <h4 className="font-medium text-gray-900 dark:text-white truncate">
                      {name}
                    </h4>
                    <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300">
                      {config.type}
                    </span>
                  </div>
                  <p className="mt-1 text-sm text-gray-500 dark:text-gray-400 truncate">
                    {getServerDescription(config)}
                  </p>
                  {(() => {
                    const envKeys = config.env_keys?.length
                      ? config.env_keys
                      : config.env
                        ? Object.keys(config.env)
                        : []
                    return envKeys.length > 0 ? (
                      <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
                        Env: {envKeys.join(', ')}
                      </p>
                    ) : null
                  })()}
                  {(() => {
                    const headerKeys = config.header_keys?.length
                      ? config.header_keys
                      : config.headers
                        ? Object.keys(config.headers)
                        : []
                    return headerKeys.length > 0 ? (
                      <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
                        Headers: {headerKeys.join(', ')}
                      </p>
                    ) : null
                  })()}
                </div>
                <div className="flex gap-2 ml-4">
                  <button
                    type="button"
                    onClick={() => handleEdit(name)}
                    className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 text-sm font-medium"
                  >
                    Edit
                  </button>
                  <button
                    type="button"
                    onClick={() => handleDelete(name)}
                    className="text-red-600 hover:text-red-800 dark:text-red-400 dark:hover:text-red-300 text-sm font-medium"
                  >
                    Delete
                  </button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      <button
        type="button"
        onClick={handleAdd}
        className="w-full py-2 px-4 border-2 border-dashed border-gray-300 dark:border-gray-600 rounded-lg text-gray-600 dark:text-gray-400 hover:border-blue-500 hover:text-blue-500 dark:hover:border-blue-400 dark:hover:text-blue-400 transition-colors"
      >
        + Add MCP Server
      </button>

      {isModalOpen && editingServer && (
        <MCPServerModal
          server={editingServer}
          existingNames={Object.keys(servers || {})}
          onSave={handleModalSave}
          onClose={handleModalClose}
        />
      )}
    </div>
  )
}

interface MCPServerModalProps {
  server: EditingServer
  existingNames: string[]
  onSave: (name: string, config: APIMCPServerConfig) => void
  onClose: () => void
}

function MCPServerModal({ server, existingNames, onSave, onClose }: MCPServerModalProps) {
  const [name, setName] = useState(server.name)
  const [config, setConfig] = useState<APIMCPServerConfig>(server.config)
  const [argsText, setArgsText] = useState(server.config.args?.join('\n') || '')
  const [envPairs, setEnvPairs] = useState<Array<{ key: string; value: string }>>(() => {
    if (server.config.env && Object.keys(server.config.env).length > 0) {
      return Object.entries(server.config.env).map(([key, value]) => ({ key, value }))
    }
    if (server.config.env_keys && server.config.env_keys.length > 0) {
      // API から取得したキーのみ（値は空でユーザーに再入力を促す）
      return server.config.env_keys.map((key) => ({ key, value: '' }))
    }
    return []
  })
  const [headerPairs, setHeaderPairs] = useState<Array<{ key: string; value: string }>>(() => {
    if (server.config.headers && Object.keys(server.config.headers).length > 0) {
      return Object.entries(server.config.headers).map(([key, value]) => ({ key, value }))
    }
    if (server.config.header_keys && server.config.header_keys.length > 0) {
      // API から取得したキーのみ（値は空でユーザーに再入力を促す）
      return server.config.header_keys.map((key) => ({ key, value: '' }))
    }
    return []
  })
  const [error, setError] = useState<string | null>(null)

  const handleTypeChange = (type: APIMCPServerConfig['type']) => {
    setConfig(prev => ({
      ...prev,
      type,
      // タイプ変更時に関連しないフィールドをクリア
      command: type === 'stdio' ? prev.command : undefined,
      args: type === 'stdio' ? prev.args : undefined,
      url: type !== 'stdio' ? prev.url : undefined,
      headers: type !== 'stdio' ? prev.headers : undefined,
    }))
    if (type !== 'stdio') {
      setArgsText('')
    }
  }

  const handleAddEnv = () => {
    setEnvPairs(prev => [...prev, { key: '', value: '' }])
  }

  const handleRemoveEnv = (index: number) => {
    setEnvPairs(prev => prev.filter((_, i) => i !== index))
  }

  const handleEnvChange = (index: number, field: 'key' | 'value', value: string) => {
    setEnvPairs(prev => prev.map((pair, i) =>
      i === index ? { ...pair, [field]: value } : pair
    ))
  }

  const handleAddHeader = () => {
    setHeaderPairs(prev => [...prev, { key: '', value: '' }])
  }

  const handleRemoveHeader = (index: number) => {
    setHeaderPairs(prev => prev.filter((_, i) => i !== index))
  }

  const handleHeaderChange = (index: number, field: 'key' | 'value', value: string) => {
    setHeaderPairs(prev => prev.map((pair, i) =>
      i === index ? { ...pair, [field]: value } : pair
    ))
  }

  const handleSave = () => {
    setError(null)

    // バリデーション
    if (!name.trim()) {
      setError('Server name is required')
      return
    }

    // 名前の重複チェック（新規または名前変更時）
    if ((server.isNew || server.name !== name.trim()) && existingNames.includes(name.trim())) {
      setError('A server with this name already exists')
      return
    }

    if (config.type === 'stdio' && !config.command?.trim()) {
      setError('Command is required for stdio type')
      return
    }

    if (config.type !== 'stdio' && !config.url?.trim()) {
      setError('URL is required for http/sse type')
      return
    }

    // 設定を構築
    const finalConfig: APIMCPServerConfig = {
      type: config.type,
    }

    if (config.type === 'stdio') {
      finalConfig.command = config.command?.trim()
      const args = argsText.split('\n').map(a => a.trim()).filter(a => a)
      if (args.length > 0) {
        finalConfig.args = args
      }
    } else {
      finalConfig.url = config.url?.trim()
      const headers: Record<string, string> = {}
      headerPairs.forEach(({ key, value }) => {
        if (key.trim() && value.trim()) {
          headers[key.trim()] = value.trim()
        }
      })
      if (Object.keys(headers).length > 0) {
        finalConfig.headers = headers
      }
    }

    // 環境変数
    const env: Record<string, string> = {}
    envPairs.forEach(({ key, value }) => {
      if (key.trim() && value.trim()) {
        env[key.trim()] = value.trim()
      }
    })
    if (Object.keys(env).length > 0) {
      finalConfig.env = env
    }

    onSave(name.trim(), finalConfig)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black bg-opacity-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-lg mx-4 max-h-[90vh] overflow-y-auto">
        <div className="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white">
            {server.isNew ? 'Add MCP Server' : 'Edit MCP Server'}
          </h3>
          <button
            type="button"
            onClick={onClose}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
          >
            <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="p-4 space-y-4">
          {error && (
            <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
              <p className="text-red-600 dark:text-red-400 text-sm">{error}</p>
            </div>
          )}

          {/* Server Name */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              Server Name <span className="text-red-500">*</span>
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g., github, slack"
              className="w-full border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          </div>

          {/* Type */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Type <span className="text-red-500">*</span>
            </label>
            <div className="flex gap-4">
              {(['stdio', 'http', 'sse'] as const).map((type) => (
                <label key={type} className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="radio"
                    name="type"
                    value={type}
                    checked={config.type === type}
                    onChange={() => handleTypeChange(type)}
                    className="text-blue-600 focus:ring-blue-500"
                  />
                  <span className="text-gray-700 dark:text-gray-300">{type}</span>
                </label>
              ))}
            </div>
          </div>

          {/* Type-specific fields */}
          {config.type === 'stdio' ? (
            <>
              {/* Command */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Command <span className="text-red-500">*</span>
                </label>
                <input
                  type="text"
                  value={config.command || ''}
                  onChange={(e) => setConfig(prev => ({ ...prev, command: e.target.value }))}
                  placeholder="e.g., npx, node, python"
                  className="w-full border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>

              {/* Args */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Arguments (one per line)
                </label>
                <textarea
                  value={argsText}
                  onChange={(e) => setArgsText(e.target.value)}
                  placeholder={"-y\n@modelcontextprotocol/server-github"}
                  rows={3}
                  className="w-full border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 font-mono text-sm"
                />
              </div>
            </>
          ) : (
            <>
              {/* URL */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  URL <span className="text-red-500">*</span>
                </label>
                <input
                  type="url"
                  value={config.url || ''}
                  onChange={(e) => setConfig(prev => ({ ...prev, url: e.target.value }))}
                  placeholder="https://mcp.example.com/server"
                  className="w-full border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>

              {/* Headers */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  Headers
                </label>
                <div className="space-y-2">
                  {headerPairs.map((pair, index) => (
                    <div key={index} className="flex gap-2">
                      <input
                        type="text"
                        value={pair.key}
                        onChange={(e) => handleHeaderChange(index, 'key', e.target.value)}
                        placeholder="Key"
                        className="flex-1 border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm"
                      />
                      <input
                        type="password"
                        value={pair.value}
                        onChange={(e) => handleHeaderChange(index, 'value', e.target.value)}
                        placeholder="Value"
                        className="flex-1 border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm"
                      />
                      <button
                        type="button"
                        onClick={() => handleRemoveHeader(index)}
                        className="px-2 text-red-500 hover:text-red-700"
                      >
                        <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                        </svg>
                      </button>
                    </div>
                  ))}
                  <button
                    type="button"
                    onClick={handleAddHeader}
                    className="text-sm text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                  >
                    + Add Header
                  </button>
                </div>
              </div>
            </>
          )}

          {/* Environment Variables */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Environment Variables
            </label>
            <div className="space-y-2">
              {envPairs.map((pair, index) => (
                <div key={index} className="flex gap-2">
                  <input
                    type="text"
                    value={pair.key}
                    onChange={(e) => handleEnvChange(index, 'key', e.target.value)}
                    placeholder="Key"
                    className="flex-1 border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm"
                  />
                  <input
                    type="password"
                    value={pair.value}
                    onChange={(e) => handleEnvChange(index, 'value', e.target.value)}
                    placeholder="Value"
                    className="flex-1 border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm"
                  />
                  <button
                    type="button"
                    onClick={() => handleRemoveEnv(index)}
                    className="px-2 text-red-500 hover:text-red-700"
                  >
                    <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                    </svg>
                  </button>
                </div>
              ))}
              <button
                type="button"
                onClick={handleAddEnv}
                className="text-sm text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
              >
                + Add Variable
              </button>
            </div>
          </div>
        </div>

        <div className="flex justify-end gap-3 p-4 border-t border-gray-200 dark:border-gray-700">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={handleSave}
            className="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700 transition-colors"
          >
            Save
          </button>
        </div>
      </div>
    </div>
  )
}
