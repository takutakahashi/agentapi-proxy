'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { Github } from 'lucide-react'
import { getRedirectUri } from '@/lib/oauth-utils'
import { useConfig } from '@/hooks/useConfig'

export default function LoginPage() {
  const router = useRouter()
  const { config, isLoading: isConfigLoading } = useConfig()
  const [apiKey, setApiKey] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [isGitHubLoading, setIsGitHubLoading] = useState(false)
  const [checkingAuth, setCheckingAuth] = useState(true)

  // 設定から値を取得
  const loginTitle = config?.loginTitle ?? 'AgentAPI UI'
  const loginDescription = config?.loginDescription ?? 'Enter an access token or sign in with GitHub to continue.'
  const loginSubDescription = config?.loginSubDescription ?? ''

  // 認証状態をチェック
  useEffect(() => {
    const checkAuthStatus = async () => {
      try {
        const response = await fetch('/api/auth/status')
        const data = await response.json()

        if (data.authenticated) {
          // 認証済みの場合はダッシュボードにリダイレクト
          router.push('/')
        } else {
          // 認証されていない場合はログインフォームを表示
          setCheckingAuth(false)
        }
      } catch (error) {
        console.error('Failed to check auth status:', error)
        setCheckingAuth(false)
      }
    }

    checkAuthStatus()
  }, [router])

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    setError('')

    try {
      const response = await fetch('/api/auth/login', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ apiKey }),
      })

      const data = await response.json()

      if (response.ok) {
        router.push('/')
      } else {
        setError(data.error || 'Invalid API key')
      }
    } catch {
      setError('An error occurred. Please try again.')
    } finally {
      setLoading(false)
    }
  }

  const handleGitHubLogin = async () => {
    setIsGitHubLoading(true)
    setError('')

    try {
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
      setIsGitHubLoading(false)
    }
  }

  // 認証状態チェック中または設定読み込み中はローディングを表示
  if (checkingAuth || isConfigLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-900">
        <div className="text-center">
          <div className="inline-block animate-spin rounded-full h-8 w-8 border-b-2 border-gray-900 dark:border-white"></div>
          <p className="mt-4 text-gray-600 dark:text-gray-400">Checking authentication...</p>
        </div>
      </div>
    )
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-900 py-12 px-4 sm:px-6 lg:px-8">
      <div className="max-w-md w-full space-y-8">
        <div>
          <h1 className="mt-6 text-center text-3xl font-extrabold text-gray-900 dark:text-white">
            {loginTitle}
          </h1>
          <p className="mt-2 text-center text-sm text-gray-600 dark:text-gray-400">
            {loginDescription}
          </p>
          {loginSubDescription && (
            <p className="mt-1 text-center text-xs text-gray-500 dark:text-gray-500">
              {loginSubDescription}
            </p>
          )}
        </div>

        <div className="mt-8">
          <button
            onClick={handleGitHubLogin}
            disabled={isGitHubLoading}
            className="w-full flex justify-center items-center px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-md shadow-sm text-sm font-medium text-gray-700 dark:text-gray-200 bg-white dark:bg-gray-800 hover:bg-gray-50 dark:hover:bg-gray-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <Github className="w-5 h-5 mr-2" />
            {isGitHubLoading ? 'Redirecting...' : 'Continue with GitHub'}
          </button>
        </div>

        <div className="mt-6">
          <div className="relative">
            <div className="absolute inset-0 flex items-center">
              <div className="w-full border-t border-gray-300 dark:border-gray-600" />
            </div>
            <div className="relative flex justify-center text-sm">
              <span className="px-2 bg-gray-50 dark:bg-gray-900 text-gray-500">Or continue with token</span>
            </div>
          </div>
        </div>

        <form className="mt-6 space-y-6" onSubmit={handleLogin}>
          <div className="rounded-md shadow-sm -space-y-px">
            <div>
              <label htmlFor="api-key" className="sr-only">
                Access token
              </label>
              <input
                id="api-key"
                name="api-key"
                type="password"
                required
                className="appearance-none rounded-md relative block w-full px-3 py-2 border border-gray-300 dark:border-gray-600 placeholder-gray-500 dark:placeholder-gray-400 text-gray-900 dark:text-white dark:bg-gray-800 focus:outline-none focus:ring-blue-500 focus:border-blue-500 focus:z-10 sm:text-sm"
                placeholder="Enter your access token"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
              />
            </div>
          </div>

          <div>
            <button
              type="submit"
              disabled={loading || !apiKey}
              className="group relative w-full flex justify-center py-2 px-4 border border-transparent text-sm font-medium rounded-md text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {loading ? 'Logging in...' : 'Continue with token'}
            </button>
          </div>
        </form>

        {error && (
          <div className="rounded-md bg-red-50 dark:bg-red-900/20 p-4">
            <p className="text-sm text-red-800 dark:text-red-200">{error}</p>
          </div>
        )}
      </div>
    </div>
  )
}
