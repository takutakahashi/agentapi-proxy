'use client'

import { useState } from 'react'
import { MarketplaceConfig } from '@/types/settings'

interface MarketplaceSettingsProps {
  marketplaces: Record<string, MarketplaceConfig> | undefined
  onChange: (marketplaces: Record<string, MarketplaceConfig>) => void
}

interface EditingMarketplace {
  originalName: string  // 編集時の元の名前（新規は空）
  config: MarketplaceConfig
  isNew: boolean
}

// URLからリポジトリ名を抽出
export const extractRepoNameFromUrl = (url: string): string => {
  try {
    // .git suffix を除去
    const cleanUrl = url.replace(/\.git$/, '')
    // URL からパスの最後の部分を取得
    const urlObj = new URL(cleanUrl)
    const pathParts = urlObj.pathname.split('/').filter(p => p)
    // 最後の部分がリポジトリ名
    return pathParts[pathParts.length - 1] || ''
  } catch {
    // URL パース失敗時はパスとして解釈を試みる
    const cleanUrl = url.replace(/\.git$/, '')
    const parts = cleanUrl.split('/').filter(p => p)
    return parts[parts.length - 1] || ''
  }
}

const getDefaultMarketplaceConfig = (): MarketplaceConfig => ({
  url: '',
})

export function MarketplaceSettings({ marketplaces, onChange }: MarketplaceSettingsProps) {
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [editingMarketplace, setEditingMarketplace] = useState<EditingMarketplace | null>(null)

  const marketplaceEntries = Object.entries(marketplaces || {})

  const handleAdd = () => {
    setEditingMarketplace({
      originalName: '',
      config: getDefaultMarketplaceConfig(),
      isNew: true,
    })
    setIsModalOpen(true)
  }

  const handleEdit = (name: string) => {
    const config = marketplaces?.[name]
    if (config) {
      setEditingMarketplace({
        originalName: name,
        config: { ...config },
        isNew: false,
      })
      setIsModalOpen(true)
    }
  }

  const handleDelete = (name: string) => {
    if (confirm(`Are you sure you want to delete "${name}"?`)) {
      const newMarketplaces = { ...marketplaces }
      delete newMarketplaces[name]
      onChange(newMarketplaces)
    }
  }

  const handleModalSave = (config: MarketplaceConfig) => {
    const newMarketplaces = { ...marketplaces }
    const name = extractRepoNameFromUrl(config.url)

    // 名前が変更された場合（URL変更時）は古いエントリを削除
    if (editingMarketplace && !editingMarketplace.isNew && editingMarketplace.originalName !== name) {
      delete newMarketplaces[editingMarketplace.originalName]
    }

    newMarketplaces[name] = config
    onChange(newMarketplaces)
    setIsModalOpen(false)
    setEditingMarketplace(null)
  }

  const handleModalClose = () => {
    setIsModalOpen(false)
    setEditingMarketplace(null)
  }

  return (
    <div className="space-y-4">
      <div>
        {marketplaceEntries.length === 0 ? (
          <div className="text-center py-8 text-gray-500 dark:text-gray-400">
            <p>No marketplaces configured</p>
            <p className="text-sm mt-1">Click the button below to add your first marketplace</p>
          </div>
        ) : (
          <div className="space-y-3">
            {marketplaceEntries.map(([name, config]) => (
              <div
                key={name}
                className="border border-gray-200 dark:border-gray-700 rounded-lg p-4 bg-white dark:bg-gray-800"
              >
                <div className="flex items-start justify-between">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-lg">🛒</span>
                      <h4 className="font-medium text-gray-900 dark:text-white truncate">
                        {name}
                      </h4>
                    </div>
                    <p className="mt-1 text-sm text-gray-500 dark:text-gray-400 truncate">
                      {config.url || 'No URL configured'}
                    </p>
                  </div>
                  <div className="flex gap-2 ml-4">
                    <button
                      type="button"
                      onClick={() => handleEdit(name)}
                      className="text-sm font-medium text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                    >
                      Edit
                    </button>
                    <button
                      type="button"
                      onClick={() => handleDelete(name)}
                      className="text-sm font-medium text-red-600 hover:text-red-800 dark:text-red-400 dark:hover:text-red-300"
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
          className="w-full py-2 px-4 border-2 border-dashed rounded-lg transition-colors border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:border-blue-500 hover:text-blue-500 dark:hover:border-blue-400 dark:hover:text-blue-400"
        >
          + Add Marketplace
        </button>
      </div>

      {isModalOpen && editingMarketplace && (
        <MarketplaceModal
          marketplace={editingMarketplace}
          existingNames={Object.keys(marketplaces || {}).filter(n => n !== editingMarketplace.originalName)}
          onSave={handleModalSave}
          onClose={handleModalClose}
        />
      )}
    </div>
  )
}

interface MarketplaceModalProps {
  marketplace: EditingMarketplace
  existingNames: string[]
  onSave: (config: MarketplaceConfig) => void
  onClose: () => void
}

function MarketplaceModal({ marketplace, existingNames, onSave, onClose }: MarketplaceModalProps) {
  const [url, setUrl] = useState(marketplace.config.url || '')
  const [error, setError] = useState<string | null>(null)

  // URLから抽出されるリポジトリ名をリアルタイムで表示
  const extractedName = extractRepoNameFromUrl(url)

  const handleSave = () => {
    setError(null)

    if (!url.trim()) {
      setError('URL is required')
      return
    }

    const repoName = extractRepoNameFromUrl(url.trim())
    if (!repoName) {
      setError('Could not extract repository name from URL')
      return
    }

    // 名前の重複チェック
    if (existingNames.includes(repoName)) {
      setError(`A marketplace with the name "${repoName}" already exists`)
      return
    }

    // 設定を構築
    const config: MarketplaceConfig = {
      url: url.trim(),
    }

    onSave(config)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black bg-opacity-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-lg mx-4 max-h-[90vh] overflow-y-auto">
        <div className="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white">
            {marketplace.isNew ? 'Add Marketplace' : 'Edit Marketplace'}
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

          {/* URL */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              Git Repository URL <span className="text-red-500">*</span>
            </label>
            <input
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://github.com/org/marketplace-plugins"
              className="w-full border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
            {extractedName && (
              <p className="mt-1 text-xs text-blue-600 dark:text-blue-400">
                Marketplace name: <span className="font-medium">{extractedName}</span>
              </p>
            )}
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              Git repository URL containing the marketplace plugins
            </p>
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
