import { NextResponse } from 'next/server'

/**
 * PWA マニフェストを動的に生成する API ルート
 * 環境変数で以下の設定が可能:
 * - PWA_APP_NAME: アプリ名
 * - PWA_SHORT_NAME: 短縮名
 * - PWA_DESCRIPTION: 説明
 * - PWA_ICON_URL: カスタムアイコン URL (設定時はすべてのサイズでこの URL を使用)
 */
export async function GET() {
  const appName = process.env.PWA_APP_NAME
    || process.env.NEXT_PUBLIC_PWA_APP_NAME
    || 'AgentAPI UI'

  const shortName = process.env.PWA_SHORT_NAME
    || process.env.NEXT_PUBLIC_PWA_SHORT_NAME
    || 'AgentAPI'

  const description = process.env.PWA_DESCRIPTION
    || process.env.NEXT_PUBLIC_PWA_DESCRIPTION
    || 'User interface for AgentAPI - AI agent conversation management'

  // カスタムアイコン URL が設定されている場合はそれを使用
  const customIconUrl = process.env.PWA_ICON_URL

  // アイコン設定を生成
  const icons = customIconUrl
    ? [
        {
          src: customIconUrl,
          sizes: '192x192',
          type: 'image/png',
          purpose: 'maskable',
        },
        {
          src: customIconUrl,
          sizes: '192x192',
          type: 'image/png',
          purpose: 'any',
        },
        {
          src: customIconUrl,
          sizes: '512x512',
          type: 'image/png',
          purpose: 'maskable',
        },
        {
          src: customIconUrl,
          sizes: '512x512',
          type: 'image/png',
          purpose: 'any',
        },
      ]
    : [
        {
          src: '/icon-192x192.png',
          sizes: '192x192',
          type: 'image/png',
          purpose: 'maskable',
        },
        {
          src: '/icon-192x192.png',
          sizes: '192x192',
          type: 'image/png',
          purpose: 'any',
        },
        {
          src: '/icon-256x256.png',
          sizes: '256x256',
          type: 'image/png',
        },
        {
          src: '/icon-384x384.png',
          sizes: '384x384',
          type: 'image/png',
        },
        {
          src: '/icon-512x512.png',
          sizes: '512x512',
          type: 'image/png',
          purpose: 'maskable',
        },
        {
          src: '/icon-512x512.png',
          sizes: '512x512',
          type: 'image/png',
          purpose: 'any',
        },
      ]

  const manifest = {
    name: appName,
    short_name: shortName,
    description: description,
    theme_color: '#000000',
    background_color: '#ffffff',
    display: 'standalone',
    orientation: 'portrait',
    scope: '/',
    start_url: '/',
    icons,
  }

  return NextResponse.json(manifest, {
    headers: {
      'Content-Type': 'application/manifest+json',
      'Cache-Control': 'no-store, no-cache, must-revalidate, proxy-revalidate',
    },
  })
}
