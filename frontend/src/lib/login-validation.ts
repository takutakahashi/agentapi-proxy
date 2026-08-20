import { getBackendUrl } from './server-backend-url'

export interface LoginValidationResult {
  ok: boolean
  status: number
}

/** Validate a login token against an authenticated backend endpoint. */
export async function validateLoginToken(
  apiKey: string,
  fetchImpl: typeof fetch = fetch,
): Promise<LoginValidationResult> {
  try {
    const response = await fetchImpl(getBackendUrl('/user/info'), {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${apiKey}`,
        Accept: 'application/json',
      },
      cache: 'no-store',
      signal: AbortSignal.timeout(10000),
    })

    if (response.ok) {
      return { ok: true, status: response.status }
    }

    return {
      ok: false,
      status: response.status === 401 || response.status === 403 ? 401 : 502,
    }
  } catch {
    // Missing configuration, DNS/connect failures, and timeouts all fail closed.
    return { ok: false, status: 503 }
  }
}
