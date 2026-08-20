import type { NextRequest } from 'next/server'

function firstHeaderValue(value: string | null): string | undefined {
  return value?.split(',')[0]?.trim() || undefined
}

export function getPublicBaseUrl(request: NextRequest): string {
  const configuredUrl = process.env.NEXT_PUBLIC_BASE_URL?.trim()
  if (configuredUrl) {
    return configuredUrl.replace(/\/$/, '')
  }

  const forwardedHost = firstHeaderValue(request.headers.get('x-forwarded-host'))
  const host = forwardedHost || firstHeaderValue(request.headers.get('host'))
  const forwardedProto = firstHeaderValue(request.headers.get('x-forwarded-proto'))

  if (host) {
    return `${forwardedProto || request.nextUrl.protocol.replace(':', '')}://${host}`
  }

  return request.nextUrl.origin
}

export function getPublicUrl(request: NextRequest, path: string): URL {
  return new URL(path, `${getPublicBaseUrl(request)}/`)
}
