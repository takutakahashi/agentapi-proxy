'use client'

import { BedrockConfig } from '@/types/settings'

interface BedrockSettingsProps {
  config: BedrockConfig | undefined
  onChange: (config: BedrockConfig) => void
  showCredentials?: boolean // チーム設定の場合のみ true
}

const DEFAULT_MODEL = 'global.anthropic.claude-sonnet-4-5-20250929-v1:0'
const RECOMMENDED_MODEL = 'global.anthropic.claude-sonnet-4-20250514-v1:0'

export function BedrockSettings({ config, onChange, showCredentials = false }: BedrockSettingsProps) {
  const getDefaultConfig = (): BedrockConfig => ({
    enabled: false,
    model: DEFAULT_MODEL,
  })

  const handleChange = <K extends keyof BedrockConfig>(field: K, value: BedrockConfig[K]) => {
    const currentConfig = config || getDefaultConfig()
    onChange({
      ...currentConfig,
      [field]: value,
    })
  }

  const handleToggleEnabled = () => {
    const currentConfig = config || getDefaultConfig()
    onChange({
      ...currentConfig,
      enabled: !currentConfig.enabled,
    })
  }

  const isEnabled = config?.enabled ?? false

  return (
    <div className="space-y-4">
      {/* Enable/Disable Toggle */}
      <div className="flex items-center justify-between">
        <div>
          <label
            htmlFor="bedrockEnabled"
            className="block text-sm font-medium text-gray-700 dark:text-gray-300"
          >
            Enable Bedrock
          </label>
          <p className="text-xs text-gray-500 dark:text-gray-400">
            Use Amazon Bedrock for AI operations
          </p>
        </div>
        <button
          type="button"
          id="bedrockEnabled"
          onClick={handleToggleEnabled}
          className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 ${
            isEnabled ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-600'
          }`}
          role="switch"
          aria-checked={isEnabled}
        >
          <span
            className={`pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
              isEnabled ? 'translate-x-5' : 'translate-x-0'
            }`}
          />
        </button>
      </div>

      {/* Model ID (Optional) */}
      <div>
        <label
          htmlFor="model"
          className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
        >
          Model ID
        </label>
        <input
          type="text"
          id="model"
          value={config?.model || ''}
          onChange={(e) => handleChange('model', e.target.value)}
          placeholder={DEFAULT_MODEL}
          disabled={!isEnabled}
          className="mt-1 block w-full border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent disabled:opacity-50 disabled:cursor-not-allowed"
        />
        <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
          Bedrock model ID (default: {DEFAULT_MODEL})
        </p>
        <div className="mt-2">
          <button
            type="button"
            onClick={() => handleChange('model', RECOMMENDED_MODEL)}
            disabled={!isEnabled}
            className="inline-flex items-center px-3 py-1.5 text-xs font-medium rounded-md text-white bg-orange-600 hover:bg-orange-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-orange-500 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            Claude Sonnet 4
          </button>
          <span className="ml-2 text-xs text-gray-500 dark:text-gray-400">
            Recommended
          </span>
        </div>
      </div>

      {/* AWS Credentials - Team settings only */}
      {showCredentials && (
        <>
          <div className="border-t border-gray-200 dark:border-gray-700 pt-4 mt-4">
            <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
              AWS Credentials (Team Only)
            </h4>
          </div>

          <div>
            <label
              htmlFor="accessKeyId"
              className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
            >
              Access Key ID
            </label>
            <input
              type="text"
              id="accessKeyId"
              value={config?.access_key_id || ''}
              onChange={(e) => handleChange('access_key_id', e.target.value)}
              placeholder="AKIAIOSFODNN7EXAMPLE"
              disabled={!isEnabled}
              className="mt-1 block w-full border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent disabled:opacity-50 disabled:cursor-not-allowed"
            />
          </div>

          <div>
            <label
              htmlFor="secretAccessKey"
              className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
            >
              Secret Access Key
            </label>
            <input
              type="password"
              id="secretAccessKey"
              value={config?.secret_access_key || ''}
              onChange={(e) => handleChange('secret_access_key', e.target.value)}
              placeholder="••••••••••••••••"
              disabled={!isEnabled}
              className="mt-1 block w-full border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent disabled:opacity-50 disabled:cursor-not-allowed"
            />
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              AWS credentials for Bedrock API access. These are stored securely and masked in responses.
            </p>
          </div>
        </>
      )}
    </div>
  )
}
