/**
 * Resolve the internal backend URL used by Next.js route handlers.
 *
 * Older deployments sometimes configured the frontend with a URL ending in
 * `/api/proxy`. Route handlers now talk to the backend directly, so strip that
 * legacy suffix instead of accidentally nesting requests below it.
 */
export function normalizeBackendBaseUrl(configuredUrl: string): string {
  const value = configuredUrl.trim()
  if (!value) {
    throw new Error('AGENTAPI_PROXY_URL is required')
  }

  const url = new URL(value)
  url.hash = ''
  url.search = ''
  url.pathname = url.pathname
    .replace(/\/api\/proxy\/?$/, '')
    .replace(/\/+$/, '')

  return url.toString().replace(/\/$/, '')
}

export function getBackendBaseUrl(): string {
  const configuredUrl = process.env.AGENTAPI_PROXY_URL || process.env.AGENTAPI_PROXY_ENDPOINT
  if (!configuredUrl) {
    throw new Error('AGENTAPI_PROXY_URL is required')
  }
  return normalizeBackendBaseUrl(configuredUrl)
}

export function getBackendUrl(path: string): string {
  const normalizedPath = path.replace(/^\/+/, '')
  return new URL(normalizedPath, `${getBackendBaseUrl()}/`).toString()
}
