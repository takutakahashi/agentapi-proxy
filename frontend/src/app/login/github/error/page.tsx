import Link from 'next/link'
import { AlertCircle } from 'lucide-react'

const errorMessages: Record<string, { title: string; description: string }> = {
  missing_params: {
    title: '認証情報を確認できませんでした',
    description: 'GitHub から必要な情報が返されませんでした。もう一度ログインをお試しください。',
  },
  invalid_state: {
    title: '認証セッションの有効期限が切れました',
    description: '安全のため、このログイン処理は続行できません。ログインを最初からやり直してください。',
  },
  auth_failed: {
    title: 'GitHub ログインに失敗しました',
    description: '認証処理を完了できませんでした。時間をおいてもう一度お試しください。',
  },
  server_error: {
    title: '予期しないエラーが発生しました',
    description: '一時的な問題が発生している可能性があります。時間をおいてもう一度お試しください。',
  },
}

type ErrorPageProps = {
  searchParams: Promise<{ error?: string }>
}

export default async function GitHubLoginErrorPage({ searchParams }: ErrorPageProps) {
  const { error } = await searchParams
  const message = errorMessages[error || ''] || errorMessages.server_error

  return (
    <main className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-900 p-4">
      <div className="w-full max-w-md rounded-lg bg-white p-6 text-center shadow-md dark:bg-gray-800">
        <AlertCircle className="mx-auto mb-4 h-12 w-12 text-red-500" aria-hidden="true" />
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">
          {message.title}
        </h1>
        <p className="mt-3 text-gray-600 dark:text-gray-300">
          {message.description}
        </p>
        <div className="mt-6 space-y-3">
          <Link
            href="/login/github"
            className="block w-full rounded-md bg-gray-800 px-4 py-3 text-sm font-medium text-white hover:bg-gray-900 focus:outline-none focus:ring-2 focus:ring-gray-500 focus:ring-offset-2 dark:bg-gray-700 dark:hover:bg-gray-600"
          >
            GitHub ログインをやり直す
          </Link>
          <Link
            href="/login"
            className="block text-sm font-medium text-gray-600 hover:text-gray-900 dark:text-gray-300 dark:hover:text-white"
          >
            ログイン方法の選択に戻る
          </Link>
        </div>
      </div>
    </main>
  )
}
