import { NextRequest, NextResponse } from 'next/server'
import {
  AUTH_COOKIE_NAME,
  AUTH_COOKIE_OPTIONS,
  encryptOAuthSession,
} from '@/lib/cookie-auth'
import { getPublicBaseUrl, getPublicUrl } from '@/lib/public-url'
import { getBackendUrl } from '@/lib/server-backend-url'

function redirectToError(request: NextRequest, error: string) {
  const url = getPublicUrl(request, '/login/github/error')
  url.searchParams.set('error', error)
  return NextResponse.redirect(url)
}

export async function GET(request: NextRequest) {
  try {
    const searchParams = request.nextUrl.searchParams
    const code = searchParams.get('code')
    const state = searchParams.get('state')

    if (!code || !state) {
      return redirectToError(request, 'missing_params')
    }

    // stateの検証（CSRF対策）
    const cookies = request.cookies
    const savedState = cookies.get('oauth_state')?.value

    if (!savedState || savedState !== state) {
      return redirectToError(request, 'invalid_state')
    }

    const callbackUrl = new URL(getBackendUrl('/oauth/callback'))
    callbackUrl.searchParams.set('code', code)
    callbackUrl.searchParams.set('state', state)
    const response = await fetch(callbackUrl, {
      method: 'GET',
      cache: 'no-store',
    })

    if (!response.ok) {
      console.error('OAuth callback error:', response.status, 'Response details omitted for security')
      return redirectToError(request, 'auth_failed')
    }

    const data = await response.json()
    if (
      typeof data.session_id !== 'string'
      || data.session_id.length === 0
      || typeof data.access_token !== 'string'
      || data.access_token.length === 0
    ) {
      console.error('OAuth callback returned an invalid session response')
      return redirectToError(request, 'auth_failed')
    }

    // ホームページにリダイレクト - 適切なホスト名を使用
    const redirectUrl = getPublicBaseUrl(request)
    const redirectResponse = NextResponse.redirect(new URL('/chats', redirectUrl))
    redirectResponse.cookies.set(
      AUTH_COOKIE_NAME,
      encryptOAuthSession(data.session_id, data.access_token),
      AUTH_COOKIE_OPTIONS,
    )
    redirectResponse.cookies.set('oauth_state', '', {
      httpOnly: true,
      secure: process.env.NODE_ENV === 'production',
      sameSite: 'lax',
      maxAge: 0,
      path: '/',
    })

    return redirectResponse
  } catch {
    console.error('OAuth callback request failed')
    return redirectToError(request, 'server_error')
  }
}
