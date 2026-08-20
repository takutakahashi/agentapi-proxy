import { NextRequest, NextResponse } from 'next/server'

const DEFAULT_SCIA_INTERNAL_URL = 'http://scia-oauth:8081'

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ provider: string }> }
) {
  const { provider: rawProvider } = await params
  const provider = encodeURIComponent(rawProvider)
  const sciaBaseUrl = (process.env.SCIA_OAUTH_INTERNAL_URL || DEFAULT_SCIA_INTERNAL_URL).replace(/\/$/, '')
  const publicBaseUrl = getPublicBaseUrl(request)
  const completePath = process.env.NEXT_PUBLIC_OAUTH_COMPLETE_PATH || `/integrations?integration_connected=${provider}`
  const callbackUrl = `${sciaBaseUrl}/oauth/${provider}/callback${request.nextUrl.search}`

  const response = await fetch(callbackUrl, {
    method: 'GET',
    cache: 'no-store',
  })

  if (!response.ok) {
    const text = await response.text()
    return new NextResponse(text || `${rawProvider} OAuth callback failed`, {
      status: response.status,
      headers: {
        'content-type': response.headers.get('content-type') || 'text/plain; charset=utf-8',
      },
    })
  }

  return NextResponse.redirect(new URL(completePath, publicBaseUrl))
}

function getPublicBaseUrl(request: NextRequest): string {
  if (process.env.NEXT_PUBLIC_BASE_URL) {
    return process.env.NEXT_PUBLIC_BASE_URL
  }

  const forwardedHost = request.headers.get('x-forwarded-host')
  const host = forwardedHost || request.headers.get('host')
  if (host && !host.startsWith('0.0.0.0')) {
    const proto = request.headers.get('x-forwarded-proto') || 'https'
    return `${proto}://${host}`
  }

  return 'https://cc-dev.takutakahashi.dev'
}
