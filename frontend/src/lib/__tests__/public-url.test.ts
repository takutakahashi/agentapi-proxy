import { afterEach, describe, expect, it } from 'vitest'
import { NextRequest } from 'next/server'
import { getPublicBaseUrl, getPublicUrl } from '../public-url'

const originalBaseUrl = process.env.NEXT_PUBLIC_BASE_URL

afterEach(() => {
  if (originalBaseUrl === undefined) {
    delete process.env.NEXT_PUBLIC_BASE_URL
  } else {
    process.env.NEXT_PUBLIC_BASE_URL = originalBaseUrl
  }
})

describe('public URL resolution', () => {
  it('prefers the configured public URL over the internal request address', () => {
    process.env.NEXT_PUBLIC_BASE_URL = 'https://agentapi.example.com/'
    const request = new NextRequest('http://0.0.0.0:3000/api/auth/github/callback')

    expect(getPublicBaseUrl(request)).toBe('https://agentapi.example.com')
    expect(getPublicUrl(request, '/login/github/error').toString()).toBe(
      'https://agentapi.example.com/login/github/error',
    )
  })

  it('uses forwarded headers when no public URL is configured', () => {
    delete process.env.NEXT_PUBLIC_BASE_URL
    const request = new NextRequest('http://0.0.0.0:3000/api/auth/github/callback', {
      headers: {
        host: '0.0.0.0:3000',
        'x-forwarded-host': 'agentapi.example.com',
        'x-forwarded-proto': 'https',
      },
    })

    expect(getPublicBaseUrl(request)).toBe('https://agentapi.example.com')
  })
})
