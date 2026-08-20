import { describe, expect, it } from 'vitest'
import { NextRequest } from 'next/server'

import { middleware } from '../middleware'

describe('frontend middleware', () => {
  it.each(['/s/share-token', '/s/share-token/messages', '/s'])('allows public share page %s without a cookie', async (path) => {
    const response = await middleware(new NextRequest(`https://ui.example.test${path}`))

    expect(response.headers.get('x-middleware-next')).toBe('1')
  })

  it('redirects a protected page without a cookie', async () => {
    const response = await middleware(new NextRequest('https://ui.example.test/chats'))

    expect(response.status).toBe(307)
    expect(response.headers.get('location')).toBe('https://ui.example.test/login')
  })

  it('allows a protected page when the authentication cookie exists', async () => {
    const request = new NextRequest('https://ui.example.test/chats')
    request.cookies.set('agentapi_token', 'encrypted')
    const response = await middleware(request)

    expect(response.headers.get('x-middleware-next')).toBe('1')
  })
})
