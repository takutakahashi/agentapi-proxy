import { NextResponse } from 'next/server'
import type { NextRequest } from 'next/server'

export async function middleware(request: NextRequest) {
  const pathname = request.nextUrl.pathname

  // Allow access to login page, API routes and static assets
  if (
    pathname.startsWith('/login') ||
    pathname === '/s' ||
    pathname.startsWith('/s/') ||
    pathname.startsWith('/api/') ||
    pathname.startsWith('/_next/') ||
    pathname.startsWith('/static/') ||
    pathname === '/manifest.json' ||
    pathname === '/manifest.webmanifest' ||
    pathname === '/favicon.ico' ||
    pathname.startsWith('/icons/')
  ) {
    return NextResponse.next()
  }

  // Check if user has auth token
  const authToken = request.cookies.get('agentapi_token')

  if (!authToken) {
    // If no auth token, redirect to login page
    const loginUrl = new URL('/login', request.url)
    return NextResponse.redirect(loginUrl)
  }

  return NextResponse.next()
}

export const config = {
  matcher: [
    /*
     * Match all request paths except for the ones starting with:
     * - api (API routes)
     * - _next/static (static files)
     * - _next/image (image optimization files)
     * - favicon.ico (favicon file)
     * - manifest.json (PWA manifest - legacy)
     * - manifest.webmanifest (PWA manifest - dynamic)
     * - icons/ (icon files)
     *
     * Shared session pages (/s/*) are public and are allowed above.
     */
    '/((?!api|_next/static|_next/image|favicon.ico|manifest.json|manifest.webmanifest|icons/).*)',
  ],
}
