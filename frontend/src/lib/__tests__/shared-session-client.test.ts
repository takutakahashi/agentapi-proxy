import { afterEach, describe, expect, it, vi } from 'vitest'

import { createSharedSessionClientFromStorage } from '../shared-session-client'

describe('SharedSessionClient public authentication boundary', () => {
  afterEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('never sends a stored API key to a public share endpoint', async () => {
    localStorage.setItem('agentapi-full-global-settings', JSON.stringify({
      agentApiProxy: {
        endpoint: 'https://api.example.test',
        enabled: true,
        timeout: 10000,
        apiKey: 'stored-private-api-key',
      },
      mcpServers: [],
      repositoryHistory: [],
      messageTemplates: [],
      created_at: '2026-08-13T00:00:00.000Z',
      updated_at: '2026-08-13T00:00:00.000Z',
    }))
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ messages: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    const client = createSharedSessionClientFromStorage()
    await client.getMessages('public-share-token')

    const [url, options] = fetchMock.mock.calls[0]
    expect(url).toBe('https://api.example.test/s/public-share-token/messages')
    expect(new Headers(options?.headers).get('authorization')).toBeNull()
    expect(JSON.stringify(options?.headers)).not.toContain('stored-private-api-key')
  })
})
