const BLOCKED_REQUEST_HEADERS = new Set([
  'accept-encoding',
  'authorization',
  'connection',
  'content-length',
  'cookie',
  'host',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
  'x-api-key',
])

const BLOCKED_RESPONSE_HEADERS = new Set([
  'connection',
  'content-encoding',
  'content-length',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
])

export function isUnauthenticatedProxyPath(path: string): boolean {
  return path === 's'
    || path.startsWith('s/')
    || path.startsWith('oauth/')
    || path.startsWith('auth/')
}

export function requestCanHaveBody(method: string): boolean {
  const normalizedMethod = method.toUpperCase()
  return normalizedMethod !== 'GET' && normalizedMethod !== 'HEAD'
}

/** Copy browser request metadata while keeping credentials owned by the BFF. */
export function buildUpstreamRequestHeaders(
  source: Headers,
  apiKey: string | null,
  passThroughAuthorization = false,
): Headers {
  const result = new Headers()

  source.forEach((value, name) => {
    const normalizedName = name.toLowerCase()
    if (!BLOCKED_REQUEST_HEADERS.has(normalizedName)) {
      result.append(name, value)
    }
  })

  if (passThroughAuthorization && source.has('authorization')) {
    result.set('Authorization', source.get('authorization')!)
  } else if (apiKey) {
    result.set('Authorization', `Bearer ${apiKey}`)
  }

  return result
}

export function buildDownstreamResponseHeaders(source: Headers): Headers {
  const result = new Headers()

  source.forEach((value, name) => {
    if (!BLOCKED_RESPONSE_HEADERS.has(name.toLowerCase())) {
      result.append(name, value)
    }
  })

  return result
}

export function isServerSentEventsRequest(headers: Headers): boolean {
  return headers.get('accept')?.toLowerCase().includes('text/event-stream') === true
}
