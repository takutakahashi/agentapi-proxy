import { NextRequest, NextResponse } from 'next/server';
import { setApiKeyCookie } from '@/lib/cookie-auth';
import { validateLoginToken } from '@/lib/login-validation';

export async function POST(request: NextRequest) {
  try {
    const { apiKey } = await request.json();
    
    if (!apiKey || typeof apiKey !== 'string') {
      return NextResponse.json(
        { error: 'API key is required' },
        { status: 400 }
      );
    }

    // Validate API key format (basic validation)
    // Most API keys are at least 20 characters, but we'll be lenient
    if (apiKey.trim().length === 0) {
      return NextResponse.json(
        { error: 'API key cannot be empty' },
        { status: 400 }
      );
    }

    // Always validate against an authenticated backend endpoint and fail closed.
    const validation = await validateLoginToken(apiKey);
    if (!validation.ok) {
      const error = validation.status === 401
        ? 'Invalid or unauthorized API key'
        : 'Authentication service is unavailable. Please try again.';
      return NextResponse.json({ error }, { status: validation.status });
    }

    // Set the encrypted API key in a secure cookie
    await setApiKeyCookie(apiKey);

    return NextResponse.json(
      { message: 'Successfully logged in' },
      { status: 200 }
    );
  } catch {
    console.error('Login request failed');
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    );
  }
}
