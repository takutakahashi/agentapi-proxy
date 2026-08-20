import { NextResponse } from 'next/server';
import { deleteApiKeyCookie, getAuthSessionFromCookie } from '@/lib/cookie-auth';
import { getBackendUrl } from '@/lib/server-backend-url';

export async function POST() {
  try {
    const authSession = await getAuthSessionFromCookie();
    if (authSession?.type === 'github_oauth') {
      try {
        const response = await fetch(getBackendUrl('/oauth/logout'), {
          method: 'POST',
          headers: {
            'X-Session-ID': authSession.session_id,
          },
          cache: 'no-store',
          signal: AbortSignal.timeout(10000),
        });
        if (!response.ok) {
          console.warn(`OAuth session revocation failed with status ${response.status}`);
        }
      } catch {
        // Local logout must still complete when the backend is unavailable.
        console.warn('OAuth session revocation could not reach the backend');
      }
    }

    // Delete the API key cookie
    await deleteApiKeyCookie();

    return NextResponse.json(
      { message: 'Successfully logged out' },
      { status: 200 }
    );
  } catch {
    console.error('Logout request failed');
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    );
  }
}
