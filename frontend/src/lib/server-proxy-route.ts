import { NextRequest, NextResponse } from 'next/server'
import { getApiKeyFromCookie, renewApiKeyCookie } from '@/lib/cookie-auth'
import { getBackendBaseUrl } from '@/lib/server-backend-url'
import {
  buildDownstreamResponseHeaders,
  buildUpstreamRequestHeaders,
  isServerSentEventsRequest,
  isUnauthenticatedProxyPath,
  requestCanHaveBody,
} from '@/lib/proxy-transport'

export type ProxyRouteContext = { params: Promise<{ path?: string[] }> }

export interface ProxyRouteOptions {
  publicPrefix: string
  passThroughAuthorization: boolean
}

export function createProxyRouteHandler(method: string, options: ProxyRouteOptions) {
  return async (request: NextRequest, context: ProxyRouteContext) => {
    const { path = [] } = await context.params
    return handleProxyRequest(request, path, method, options)
  }
}

const DEBUG_ENABLED = process.env.NODE_ENV !== 'production' && process.env.DEBUG_LOGS === 'true'

function debugLog(message: string, metadata?: Record<string, unknown>) {
  if (DEBUG_ENABLED) {
    console.log(message, metadata)
  }
}

interface LinkedAbort {
  controller: AbortController
  cleanup: () => void
}

function createLinkedAbort(requestSignal: AbortSignal, timeoutMs?: number): LinkedAbort {
  const controller = new AbortController()
  let timeout: ReturnType<typeof setTimeout> | undefined

  const abortFromRequest = () => {
    if (!controller.signal.aborted) {
      controller.abort(requestSignal.reason)
    }
  }

  if (requestSignal.aborted) {
    abortFromRequest()
  } else {
    requestSignal.addEventListener('abort', abortFromRequest, { once: true })
  }

  if (timeoutMs !== undefined) {
    timeout = setTimeout(() => {
      if (!controller.signal.aborted) {
        controller.abort(new DOMException('Upstream request timed out', 'TimeoutError'))
      }
    }, timeoutMs)
  }

  return {
    controller,
    cleanup: () => {
      requestSignal.removeEventListener('abort', abortFromRequest)
      if (timeout !== undefined) clearTimeout(timeout)
    },
  }
}

function proxyUpstreamResponse(response: Response, linkedAbort: LinkedAbort, sse = false): NextResponse {
  const headers = buildDownstreamResponseHeaders(response.headers)
  const responseInit = {
    status: response.status,
    statusText: response.statusText,
    headers,
  }

  if (!response.body || response.status === 204 || response.status === 205 || response.status === 304) {
    linkedAbort.cleanup()
    return new NextResponse(null, responseInit)
  }

  const reader = response.body.getReader()
  let finished = false
  const finish = () => {
    if (finished) return
    finished = true
    linkedAbort.cleanup()
  }

  if (sse) {
    headers.set('Cache-Control', 'no-cache, no-transform')
    headers.set('X-Accel-Buffering', 'no')
    const encoder = new TextEncoder()
    let keepalive: ReturnType<typeof setInterval> | undefined
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        // Commit the downstream response immediately. Without an initial byte,
        // EventSource can remain CONNECTING while an otherwise healthy agent is idle.
        controller.enqueue(encoder.encode(': connected\n\n'))
        keepalive = setInterval(() => {
          try {
            controller.enqueue(encoder.encode(': keepalive\n\n'))
          } catch {
            // Cancellation races with the interval callback.
          }
        }, 15000)

        // Pump independently from downstream pull requests. OpenNext consumes
        // the returned stream through its own response pump; awaiting the
        // upstream reader from pull() can otherwise leave both sides waiting
        // and prevent even the initial SSE comment from being committed.
        void (async () => {
          try {
            while (true) {
              const { done, value } = await reader.read()
              if (done) {
                if (keepalive) clearInterval(keepalive)
                finish()
                controller.close()
                return
              }
              controller.enqueue(value)
            }
          } catch (error) {
            if (keepalive) clearInterval(keepalive)
            finish()
            controller.error(error)
          }
        })()
      },
      async cancel(reason) {
        if (keepalive) clearInterval(keepalive)
        if (!linkedAbort.controller.signal.aborted) linkedAbort.controller.abort(reason)
        try {
          await reader.cancel(reason)
        } catch {
          // The abort may already have closed the upstream reader.
        } finally {
          finish()
        }
      },
    })
    return new NextResponse(stream, responseInit)
  }

  const stream = new ReadableStream<Uint8Array>({
    async pull(controller) {
      try {
        const { done, value } = await reader.read()
        if (done) {
          finish()
          controller.close()
          return
        }
        controller.enqueue(value)
      } catch (error) {
        finish()
        controller.error(error)
      }
    },
    async cancel(reason) {
      if (!linkedAbort.controller.signal.aborted) {
        linkedAbort.controller.abort(reason)
      }
      try {
        await reader.cancel(reason)
      } catch {
        // The abort may already have closed the upstream reader.
      } finally {
        finish()
      }
    },
  })

  return new NextResponse(stream, responseInit)
}

function proxyServerSentEvents(
  targetUrl: string,
  requestInit: RequestInit,
  linkedAbort: LinkedAbort,
): NextResponse {
  const encoder = new TextEncoder()
  let keepalive: ReturnType<typeof setInterval> | undefined
  let upstreamReader: ReadableStreamDefaultReader<Uint8Array> | undefined
  let finished = false
  const finish = () => {
    if (finished) return
    finished = true
    if (keepalive) clearInterval(keepalive)
    linkedAbort.cleanup()
  }

  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      // Do not wait for the upstream server to emit its first event before
      // opening the browser-facing stream. Some SSE servers do not commit
      // their response headers while the session is idle.
      controller.enqueue(encoder.encode(': connected\n\n'))
      keepalive = setInterval(() => {
        try {
          controller.enqueue(encoder.encode(': keepalive\n\n'))
        } catch {
          // Cancellation races with the interval callback.
        }
      }, 15000)

      void (async () => {
        try {
          const response = await fetch(targetUrl, requestInit)
          if (!response.ok || !response.body) {
            throw new Error(`Upstream SSE request failed with status ${response.status}`)
          }

          upstreamReader = response.body.getReader()
          while (true) {
            const { done, value } = await upstreamReader.read()
            if (done) {
              finish()
              controller.close()
              return
            }
            controller.enqueue(value)
          }
        } catch (error) {
          finish()
          controller.error(error)
        }
      })()
    },
    async cancel(reason) {
      if (!linkedAbort.controller.signal.aborted) linkedAbort.controller.abort(reason)
      try {
        await upstreamReader?.cancel(reason)
      } catch {
        // The abort may already have closed the upstream reader.
      } finally {
        finish()
      }
    },
  })

  return new NextResponse(stream, {
    status: 200,
    headers: {
      'Content-Type': 'text/event-stream; charset=utf-8',
      'Cache-Control': 'no-cache, no-transform',
      'X-Accel-Buffering': 'no',
    },
  })
}

async function handleProxyRequest(
  request: NextRequest,
  pathParts: string[],
  method: string,
  options: ProxyRouteOptions,
): Promise<NextResponse> {
  let linkedAbort: LinkedAbort | undefined

  try {
    // Preserve percent-encoded path separators decoded by Next.js catch-all params.
    const rawPathname = request.nextUrl.pathname
    const path = rawPathname === options.publicPrefix
      ? ''
      : rawPathname.startsWith(`${options.publicPrefix}/`)
        ? rawPathname.slice(options.publicPrefix.length + 1)
      : pathParts.join('/')

    const authenticationRequired = !isUnauthenticatedProxyPath(path)
    const incomingAuthorization = options.passThroughAuthorization
      ? request.headers.get('authorization')
      : null
    let apiKey: string | null = null
    if (authenticationRequired && !incomingAuthorization) {
      apiKey = await getApiKeyFromCookie()
      if (!apiKey) {
        return NextResponse.json(
          {
            error: 'Authentication required',
            message: 'Please log in again.',
            code: 'NO_API_KEY',
          },
          { status: 401 },
        )
      }

      if (path === 'start' && method === 'POST') {
        await renewApiKeyCookie()
      }
    }

    const body = requestCanHaveBody(method) ? await request.arrayBuffer() : undefined
    const headers = buildUpstreamRequestHeaders(
      request.headers,
      apiKey,
      options.passThroughAuthorization,
    )
    const targetUrl = `${getBackendBaseUrl()}/${path}${request.nextUrl.search}`
    const isSSE = isServerSentEventsRequest(headers)
    const isMessageEndpoint = pathParts.includes('message')
      || (pathParts.includes('messages') && pathParts.includes('wait'))
    const timeoutMs = isSSE ? undefined : (isMessageEndpoint ? 120000 : 35000)

    linkedAbort = createLinkedAbort(request.signal, timeoutMs)
    debugLog('[API Proxy] Forwarding request', {
      method,
      path: `/${path}`,
      authenticated: authenticationRequired,
      streaming: isSSE,
    })

    const requestInit: RequestInit = {
      method,
      headers,
      body,
      signal: linkedAbort.controller.signal,
      redirect: 'manual',
    }

    if (isSSE) {
      const result = proxyServerSentEvents(targetUrl, requestInit, linkedAbort)
      linkedAbort = undefined
      return result
    }

    const response = await fetch(targetUrl, requestInit)

    debugLog('[API Proxy] Received upstream response', {
      method,
      path: `/${path}`,
      status: response.status,
      contentType: response.headers.get('content-type'),
    })

    const result = proxyUpstreamResponse(response, linkedAbort, isSSE)
    linkedAbort = undefined
    return result
  } catch (error) {
    linkedAbort?.cleanup()

    if (error instanceof Error && error.name === 'TimeoutError') {
      return NextResponse.json(
        {
          error: 'Request timeout',
          message: 'リクエストがタイムアウトしました。処理に時間がかかっている可能性があります。',
          code: 'TIMEOUT_ERROR',
        },
        { status: 408 },
      )
    }

    if (request.signal.aborted) {
      return NextResponse.json({ error: 'Client closed request' }, { status: 499 })
    }

    console.error('Proxy request failed:', error instanceof Error ? error.name : 'Unknown error')
    return NextResponse.json(
      {
        error: 'Internal proxy error',
        code: 'PROXY_ERROR',
      },
      { status: 502 },
    )
  }
}
