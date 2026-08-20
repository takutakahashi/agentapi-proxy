import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  AUTH_COOKIE_VERSION,
  decryptApiKey,
  decryptAuthCookie,
  encryptApiKey,
  encryptOAuthSession,
} from '../cookie-auth'

describe('versioned authentication cookie', () => {
  beforeEach(() => {
    vi.stubEnv('COOKIE_ENCRYPTION_SECRET', '11'.repeat(32))
  })

  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('stores an API key in the versioned schema', () => {
    const encrypted = encryptApiKey('api-key-value')

    expect(encrypted).not.toContain('api-key-value')
    expect(decryptAuthCookie(encrypted)).toEqual({
      version: AUTH_COOKIE_VERSION,
      type: 'api_key',
      access_token: 'api-key-value',
    })
    expect(decryptApiKey(encrypted)).toBe('api-key-value')
  })

  it('keeps the OAuth session id and access token in one encrypted payload', () => {
    const encrypted = encryptOAuthSession('session-123', 'github-access-token')

    expect(encrypted).not.toContain('github-access-token')
    expect(decryptAuthCookie(encrypted)).toEqual({
      version: AUTH_COOKIE_VERSION,
      type: 'github_oauth',
      session_id: 'session-123',
      access_token: 'github-access-token',
    })
  })

  it('rejects tampered ciphertext', () => {
    const encrypted = encryptOAuthSession('session-123', 'github-access-token')
    const tampered = `${encrypted.slice(0, -2)}AA`

    expect(() => decryptAuthCookie(tampered)).toThrow()
  })

  it('rejects a non-hex encryption key', () => {
    vi.stubEnv('COOKIE_ENCRYPTION_SECRET', 'z'.repeat(64))

    expect(() => encryptApiKey('token')).toThrow('64 hex characters')
  })
})
