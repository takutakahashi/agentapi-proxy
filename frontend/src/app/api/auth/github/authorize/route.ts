import { NextRequest, NextResponse } from 'next/server'
import { getBackendUrl } from '@/lib/server-backend-url'

export async function POST(request: NextRequest) {
  try {
    const body = await request.json()
    const { redirect_uri } = body

    if (!redirect_uri) {
      return NextResponse.json(
        { error: 'redirect_uri is required' },
        { status: 400 }
      )
    }

    const response = await fetch(getBackendUrl('/oauth/authorize'), {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        redirect_uri,
      }),
    })

    if (!response.ok) {
      console.error(`OAuth authorize request failed with status ${response.status}`)
      return NextResponse.json(
        { error: 'Failed to start OAuth flow' },
        { status: response.status }
      )
    }

    const data = await response.json()
    
    const result = NextResponse.json({
      auth_url: data.auth_url,
      state: data.state
    })
    result.cookies.set('oauth_state', data.state, {
      httpOnly: true,
      secure: process.env.NODE_ENV === 'production',
      sameSite: 'lax',
      maxAge: 900,
      path: '/',
    })
    return result
  } catch {
    console.error('OAuth authorize request failed')
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    )
  }
}
