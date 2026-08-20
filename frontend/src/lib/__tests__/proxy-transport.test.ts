import { describe, expect, it } from 'vitest'

import {
  buildDownstreamResponseHeaders,
  buildUpstreamRequestHeaders,
  isServerSentEventsRequest,
  isUnauthenticatedProxyPath,
  requestCanHaveBody,
} from '../proxy-transport'

describe('proxy transport helpers', () => {
  it('allows only explicitly public backend paths without a login cookie', () => {
    expect(isUnauthenticatedProxyPath('s/share-token/messages')).toBe(true)
    expect(isUnauthenticatedProxyPath('oauth/callback')).toBe(true)
    expect(isUnauthenticatedProxyPath('auth/status')).toBe(true)
    expect(isUnauthenticatedProxyPath('sessions')).toBe(false)
    expect(isUnauthenticatedProxyPath('sessions/something')).toBe(false)
  })

  it('preserves content negotiation, conditional, range, replay, and ACP headers', () => {
    const source = new Headers({
      Accept: 'text/event-stream',
      'Content-Type': 'application/json-patch+json',
      Range: 'bytes=10-20',
      'If-None-Match': '"etag"',
      'Last-Event-ID': '42',
      'Acp-Session-Id': 'acp-1',
      Cookie: 'agentapi_token=encrypted',
      Authorization: 'Bearer browser-supplied',
      'X-API-Key': 'browser-supplied',
    })

    const result = buildUpstreamRequestHeaders(source, 'cookie-token')

    expect(result.get('accept')).toBe('text/event-stream')
    expect(result.get('content-type')).toBe('application/json-patch+json')
    expect(result.get('range')).toBe('bytes=10-20')
    expect(result.get('if-none-match')).toBe('"etag"')
    expect(result.get('last-event-id')).toBe('42')
    expect(result.get('acp-session-id')).toBe('acp-1')
    expect(result.get('cookie')).toBeNull()
    expect(result.get('x-api-key')).toBeNull()
    expect(result.get('authorization')).toBe('Bearer cookie-token')
  })

  it('does not forward browser credentials for a public request', () => {
    const result = buildUpstreamRequestHeaders(new Headers({
      Authorization: 'Bearer attacker-controlled',
      Cookie: 'agentapi_token=value',
    }), null)

    expect(result.get('authorization')).toBeNull()
    expect(result.get('cookie')).toBeNull()
  })

  it('passes through Authorization only when explicitly enabled', () => {
    const result = buildUpstreamRequestHeaders(new Headers({
      Authorization: 'Bearer api-client-token',
      Cookie: 'agentapi_token=encrypted-ui-cookie',
    }), null, true)

    expect(result.get('authorization')).toBe('Bearer api-client-token')
    expect(result.get('cookie')).toBeNull()
  })

  it('preserves response metadata without stale transport encodings', () => {
    const result = buildDownstreamResponseHeaders(new Headers({
      'Content-Type': 'application/octet-stream',
      ETag: '"asset"',
      Location: '/next',
      'Content-Encoding': 'gzip',
      'Content-Length': '123',
    }))

    expect(result.get('content-type')).toBe('application/octet-stream')
    expect(result.get('etag')).toBe('"asset"')
    expect(result.get('location')).toBe('/next')
    expect(result.get('content-encoding')).toBeNull()
    expect(result.get('content-length')).toBeNull()
  })

  it('detects SSE and preserves bodies for every method that permits one', () => {
    expect(isServerSentEventsRequest(new Headers({ Accept: 'application/json, text/event-stream' }))).toBe(true)
    expect(requestCanHaveBody('POST')).toBe(true)
    expect(requestCanHaveBody('PATCH')).toBe(true)
    expect(requestCanHaveBody('DELETE')).toBe(true)
    expect(requestCanHaveBody('GET')).toBe(false)
    expect(requestCanHaveBody('HEAD')).toBe(false)
  })
})
