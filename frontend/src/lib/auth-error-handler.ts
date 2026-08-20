/**
 * Handles "Authentication required" (401) errors by logging the user out
 * and redirecting to the login page.
 *
 * This is triggered when the Next.js proxy returns 401 because the
 * session cookie (agentapi_token) is missing or expired.
 */

let isHandlingAuthError = false;

export async function handleAuthenticationRequired(): Promise<void> {
  if (typeof window === 'undefined') return;

  // Prevent multiple concurrent logout redirects
  if (isHandlingAuthError) return;
  isHandlingAuthError = true;

  try {
    await fetch('/api/auth/logout', { method: 'POST' });
  } catch (e) {
    console.error('[auth-error-handler] Auto-logout request failed:', e);
  }

  window.location.href = '/login';
}

/**
 * Returns true when the response data indicates that the proxy rejected the
 * request because no valid session cookie was present.
 */
export function isAuthenticationRequiredError(
  status: number,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  errorData: Record<string, any>
): boolean {
  if (status !== 401) return false;
  return (
    errorData?.error === 'Authentication required' ||
    errorData?.code === 'NO_API_KEY' ||
    String(errorData?.message ?? '').toLowerCase().includes('no api key')
  );
}
