import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  getBackendUrl,
  normalizeBackendBaseUrl,
} from '../server-backend-url'

describe('server backend URL resolution', () => {
  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('removes a legacy /api/proxy suffix', () => {
    expect(normalizeBackendBaseUrl('http://backend:8080/api/proxy/')).toBe('http://backend:8080')
  })

  it('builds a direct backend endpoint without nesting below /api/proxy', () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://backend:8080/api/proxy')

    expect(getBackendUrl('/user/info')).toBe('http://backend:8080/user/info')
  })

  it('rejects an empty backend URL', () => {
    expect(() => normalizeBackendBaseUrl('  ')).toThrow('AGENTAPI_PROXY_URL is required')
  })
})
