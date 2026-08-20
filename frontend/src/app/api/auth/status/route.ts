import { NextResponse } from 'next/server';
import { getAuthSessionFromCookie } from '@/lib/cookie-auth';

export async function GET() {
  try {
    const authSession = await getAuthSessionFromCookie();
    const authenticated = !!authSession;

    return NextResponse.json(
      {
        authenticated,
        message: authenticated ? 'Authenticated' : 'Not authenticated',
        auth_type: authSession?.type,
      },
      { status: 200 }
    );
  } catch {
    console.error('Authentication status check failed');
    return NextResponse.json(
      {
        authenticated: false,
        error: 'Internal server error'
      },
      { status: 500 }
    );
  }
}
