'use client'

import { useState } from 'react'
import { Github } from 'lucide-react'
import { getRedirectUri } from '@/lib/oauth-utils'

export default function GitHubLoginPage() {
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleGitHubLogin = async () => {
    setIsLoading(true)
    setError(null)

    try {
      // GitHub OAuth認証フローを開始
      const response = await fetch('/api/auth/github/authorize', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          redirect_uri: getRedirectUri()
        }),
      })

      if (!response.ok) {
        const errorData = await response.json()
        throw new Error(errorData.error || 'GitHub認証の開始に失敗しました')
      }

      const data = await response.json()
      
      // GitHubの認証ページにリダイレクト
      window.location.href = data.auth_url
    } catch (err) {
      setError(err instanceof Error ? err.message : '予期しないエラーが発生しました')
      setIsLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-900 p-4">
      <div className="w-full max-w-md bg-white dark:bg-gray-800 rounded-lg shadow-md">
        <div className="space-y-1 p-6 pb-4">
          <h1 className="text-2xl font-bold text-center text-gray-900 dark:text-white">
            Agent API UI
          </h1>
          <p className="text-center text-gray-600 dark:text-gray-400">
            GitHubアカウントでログインしてください
          </p>
        </div>
        <div className="space-y-4 p-6 pt-0">
          {error && (
            <div className="rounded-md bg-red-50 dark:bg-red-900/20 p-4">
              <p className="text-sm text-red-800 dark:text-red-200">{error}</p>
            </div>
          )}
          
          <button
            className="w-full flex justify-center items-center py-3 px-4 border border-transparent text-sm font-medium rounded-md text-white bg-gray-800 hover:bg-gray-900 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-gray-500 disabled:opacity-50 disabled:cursor-not-allowed"
            onClick={handleGitHubLogin}
            disabled={isLoading}
          >
            <Github className="mr-2 h-5 w-5" />
            {isLoading ? 'リダイレクト中...' : 'GitHubでログイン'}
          </button>
          
          <p className="text-center text-sm text-gray-500 dark:text-gray-400">
            ログインすることで、Agent API UIの利用規約に同意したものとみなされます
          </p>
        </div>
      </div>
    </div>
  )
}