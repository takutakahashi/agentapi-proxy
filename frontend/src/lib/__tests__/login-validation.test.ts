import { afterEach, describe, expect, it, vi } from 'vitest'

import { validateLoginToken } from '../login-validation'

describe('login token validation', () => {
  afterEach(() => {
    vi.unstubAllEnvs()
    vi.restoreAllMocks()
  })

  it('validates against the authenticated /user/info endpoint', async () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://backend:8080')
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ username: 'alice', teams: [] }), { status: 200 }),
    )

    await expect(validateLoginToken('valid-token', fetchMock)).resolves.toEqual({
      ok: true,
      status: 200,
    })

    expect(fetchMock).toHaveBeenCalledOnce()
    const [url, options] = fetchMock.mock.calls[0]
    expect(url).toBe('http://backend:8080/user/info')
    expect(new Headers(options?.headers).get('authorization')).toBe('Bearer valid-token')
  })

  it.each([401, 403])('fails closed for authentication status %s', async (status) => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://backend:8080')
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(new Response(null, { status }))

    await expect(validateLoginToken('invalid-token', fetchMock)).resolves.toEqual({
      ok: false,
      status: 401,
    })
  })

  it('fails closed for backend errors and network failures', async () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://backend:8080')
    const unavailable = vi.fn<typeof fetch>().mockResolvedValue(new Response(null, { status: 500 }))
    const disconnected = vi.fn<typeof fetch>().mockRejectedValue(new Error('connection refused'))

    await expect(validateLoginToken('token', unavailable)).resolves.toEqual({ ok: false, status: 502 })
    await expect(validateLoginToken('token', disconnected)).resolves.toEqual({ ok: false, status: 503 })
  })

  it('fails closed when the backend URL is not configured', async () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', '')
    vi.stubEnv('AGENTAPI_PROXY_ENDPOINT', '')
    const fetchMock = vi.fn<typeof fetch>()

    await expect(validateLoginToken('token', fetchMock)).resolves.toEqual({ ok: false, status: 503 })
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
