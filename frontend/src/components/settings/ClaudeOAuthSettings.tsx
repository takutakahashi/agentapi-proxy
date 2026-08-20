'use client'

import { useState } from 'react'
import { AuthMode } from '@/types/settings'

interface ClaudeOAuthSettingsProps {
  hasToken: boolean  // トークンが設定されているかどうか（API レスポンスから取得）
  authMode?: AuthMode  // 現在の認証モード
  onChange: (token: string, authMode: AuthMode) => void  // トークンと認証モードの変更を通知
}

export function ClaudeOAuthSettings({ hasToken, authMode, onChange }: ClaudeOAuthSettingsProps) {
  const [token, setToken] = useState('')
  const [isEditing, setIsEditing] = useState(false)
  const [showToken, setShowToken] = useState(false)

  const handleTokenChange = (value: string) => {
    setToken(value)
  }

  const handleSaveToken = () => {
    onChange(token, 'oauth')
    setIsEditing(false)
    setToken('')  // 入力をクリア（セキュリティのため）
  }

  const handleDeleteToken = () => {
    onChange('', authMode || 'oauth')  // 空文字列で削除
    setIsEditing(false)
    setToken('')
  }

  const handleAuthModeChange = (mode: AuthMode) => {
    // 認証モードのみ変更（トークンは変更しない）
    onChange(token || '', mode)
  }

  const handleStartEditing = () => {
    setIsEditing(true)
    setToken('')
  }

  const handleCancelEditing = () => {
    setIsEditing(false)
    setToken('')
    setShowToken(false)
  }

  return (
    <div className="space-y-4">
      {/* Token Status */}
      <div className="flex items-center justify-between">
        <div>
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
            Claude Code OAuth Token
          </label>
          <p className="text-xs text-gray-500 dark:text-gray-400">
            Claude Code の OAuth トークンを設定します
          </p>
        </div>
        <div className="flex items-center gap-2">
          {hasToken ? (
            <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400">
              <svg className="w-3 h-3 mr-1" fill="currentColor" viewBox="0 0 20 20">
                <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clipRule="evenodd" />
              </svg>
              Configured
            </span>
          ) : (
            <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300">
              Not configured
            </span>
          )}
        </div>
      </div>

      {/* Auth Mode Selection */}
      {hasToken && (
        <div className="border-t border-gray-200 dark:border-gray-700 pt-4">
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
            Authentication Mode
          </label>
          <div className="flex gap-4">
            <label className="flex items-center">
              <input
                type="radio"
                name="authMode"
                value="oauth"
                checked={authMode === 'oauth'}
                onChange={() => handleAuthModeChange('oauth')}
                className="h-4 w-4 text-blue-600 border-gray-300 focus:ring-blue-500"
              />
              <span className="ml-2 text-sm text-gray-700 dark:text-gray-300">
                OAuth (Claude Code)
              </span>
            </label>
            <label className="flex items-center">
              <input
                type="radio"
                name="authMode"
                value="bedrock"
                checked={authMode === 'bedrock'}
                onChange={() => handleAuthModeChange('bedrock')}
                className="h-4 w-4 text-blue-600 border-gray-300 focus:ring-blue-500"
              />
              <span className="ml-2 text-sm text-gray-700 dark:text-gray-300">
                Bedrock
              </span>
            </label>
          </div>
          <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
            OAuth が設定されている場合、OAuth が優先されます
          </p>
        </div>
      )}

      {/* Token Input / Edit Section */}
      {isEditing ? (
        <div className="space-y-3 border-t border-gray-200 dark:border-gray-700 pt-4">
          <div>
            <label
              htmlFor="oauthToken"
              className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
            >
              {hasToken ? 'New OAuth Token' : 'OAuth Token'}
            </label>
            <div className="relative">
              <input
                type={showToken ? 'text' : 'password'}
                id="oauthToken"
                value={token}
                onChange={(e) => handleTokenChange(e.target.value)}
                placeholder="Enter your Claude Code OAuth token"
                className="block w-full border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 pr-10 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
              />
              <button
                type="button"
                onClick={() => setShowToken(!showToken)}
                className="absolute inset-y-0 right-0 px-3 flex items-center text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
              >
                {showToken ? (
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l3.59 3.59m0 0A9.953 9.953 0 0112 5c4.478 0 8.268 2.943 9.543 7a10.025 10.025 0 01-4.132 5.411m0 0L21 21" />
                  </svg>
                ) : (
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                  </svg>
                )}
              </button>
            </div>
            <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
              Claude Code から取得した OAuth トークンを入力してください
            </p>
          </div>

          <div className="flex gap-2">
            <button
              type="button"
              onClick={handleSaveToken}
              disabled={!token.trim()}
              className="px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 rounded-md focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {hasToken ? 'Update Token' : 'Save Token'}
            </button>
            <button
              type="button"
              onClick={handleCancelEditing}
              className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-gray-500"
            >
              Cancel
            </button>
          </div>
        </div>
      ) : (
        <div className="flex gap-2 border-t border-gray-200 dark:border-gray-700 pt-4">
          <button
            type="button"
            onClick={handleStartEditing}
            className="px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 rounded-md focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500"
          >
            {hasToken ? 'Update Token' : 'Set Token'}
          </button>
          {hasToken && (
            <button
              type="button"
              onClick={handleDeleteToken}
              className="px-4 py-2 text-sm font-medium text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 hover:bg-red-100 dark:hover:bg-red-900/30 rounded-md focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-red-500"
            >
              Delete Token
            </button>
          )}
        </div>
      )}
    </div>
  )
}
