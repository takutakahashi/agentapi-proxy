'use client'

import { useState } from 'react'
import { createAgentAPIProxyClientFromStorage } from '@/lib/agentapi-proxy-client'
import type { ExternalSessionManagerRegistrationToken } from '@/types/settings'

interface Props {
  scope: 'user' | 'team'
  teamId?: string
}

export function ESMRegistrationToken({ scope, teamId }: Props) {
  const [issued, setIssued] = useState<ExternalSessionManagerRegistrationToken | null>(null)
  const [issuing, setIssuing] = useState(false)
  const [copied, setCopied] = useState<'token' | 'command' | null>(null)
  const [error, setError] = useState('')

  const issue = async () => {
    setIssuing(true)
    setError('')
    try {
      const client = createAgentAPIProxyClientFromStorage()
      setIssued(await client.issueExternalSessionManagerRegistrationToken(scope, teamId))
    } catch (cause) {
      console.error('Failed to issue ESM registration token:', cause)
      setError('登録トークンの発行に失敗しました')
    } finally {
      setIssuing(false)
    }
  }

  const copy = async (kind: 'token' | 'command') => {
    if (!issued) return
    const teamFlags = scope === 'team' && teamId ? ` --scope team --team-id ${JSON.stringify(teamId)}` : ''
    const value = kind === 'token'
      ? issued.registration_token
      : `agentapi-proxy native install --upstream <parent-proxy-url> --registration-token ${JSON.stringify(issued.registration_token)}${teamFlags}`
    await navigator.clipboard.writeText(value)
    setCopied(kind)
    setTimeout(() => setCopied(null), 2000)
  }

  return (
    <div className="rounded-lg border border-violet-200 bg-violet-50 p-3 dark:border-violet-800 dark:bg-violet-900/10">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="text-sm font-medium text-gray-900 dark:text-white">新しい manager を接続</p>
          <p className="mt-1 text-xs text-gray-600 dark:text-gray-400">15分間有効・1回限りの登録トークンを発行します。接続完了時にPool、Supplier、Bindingが自動作成されます。</p>
        </div>
        <button type="button" onClick={() => void issue()} disabled={issuing || (scope === 'team' && !teamId)} className="rounded-md bg-violet-600 px-3 py-2 text-xs text-white hover:bg-violet-700 disabled:cursor-not-allowed disabled:opacity-50">
          {issuing ? '発行中…' : issued ? '再発行' : '登録トークンを発行'}
        </button>
      </div>
      {error && <p className="mt-2 text-xs text-red-600 dark:text-red-400">{error}</p>}
      {issued && (
        <div className="mt-3 rounded-md border border-violet-200 bg-white p-2 dark:border-violet-700 dark:bg-gray-800">
          <code className="block break-all text-xs text-gray-700 dark:text-gray-200">{issued.registration_token}</code>
          <p className="mt-1 text-xs text-gray-500">有効期限: {new Date(issued.expires_at).toLocaleString()}</p>
          <div className="mt-2 flex flex-wrap gap-3">
            <button type="button" onClick={() => void copy('token')} className="text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400">{copied === 'token' ? 'コピーしました' : 'トークンをコピー'}</button>
            <button type="button" onClick={() => void copy('command')} className="text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400">{copied === 'command' ? 'コピーしました' : 'native install コマンドをコピー'}</button>
          </div>
          <p className="mt-2 text-xs font-medium text-amber-700 dark:text-amber-300">このトークンは再表示できません。必要な場所へ今すぐコピーしてください。</p>
        </div>
      )}
    </div>
  )
}
