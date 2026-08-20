import { NextResponse } from 'next/server'
import { getAuthSessionFromCookie } from '@/lib/cookie-auth'
import { getBackendUrl } from '@/lib/server-backend-url'
import { UserInfo, ProxyUserInfo } from '@/types/user'

export async function GET() {
  try {
    const authSession = await getAuthSessionFromCookie()
    if (!authSession) {
      return NextResponse.json(
        { error: 'Not authenticated' },
        { status: 401 }
      )
    }

    const response = await fetch(getBackendUrl('/user/info'), {
      headers: {
        Authorization: `Bearer ${authSession.access_token}`,
        Accept: 'application/json',
      },
      cache: 'no-store',
      signal: AbortSignal.timeout(10000),
    })
    if (!response.ok) {
      return NextResponse.json(
        { error: response.status === 401 ? 'Not authenticated' : 'Failed to get user info' },
        { status: response.status === 401 || response.status === 403 ? 401 : 502 },
      )
    }

    const proxyUserInfo = await response.json() as ProxyUserInfo
    const userInfo: UserInfo = {
      type: authSession.type === 'github_oauth' ? 'github' : 'proxy',
      user: {
        authenticated: true,
        ...(authSession.type === 'github_oauth' ? { login: proxyUserInfo.username } : {}),
      },
      proxy: proxyUserInfo
    }
    return NextResponse.json(userInfo)
  } catch {
    console.error('User info request failed')
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    )
  }
}
