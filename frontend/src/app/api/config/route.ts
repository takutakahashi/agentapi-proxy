import { NextResponse } from 'next/server'
import { DEFAULT_CONFIG } from '@/types/config'

export async function GET() {
  // VAPIDパブリックキーを取得（プライベートキーは絶対に公開しない）
  const vapidPublicKey = process.env.VAPID_PUBLIC_KEY || process.env.NEXT_PUBLIC_VAPID_PUBLIC_KEY;

  // Base64URL形式の検証
  if (vapidPublicKey && !/^[A-Za-z0-9_-]+$/.test(vapidPublicKey)) {
    console.error('Invalid VAPID_PUBLIC_KEY format detected');
  }

  const loginTitle = process.env.LOGIN_TITLE
    || process.env.NEXT_PUBLIC_LOGIN_TITLE
    || DEFAULT_CONFIG.loginTitle;
  const loginDescription = process.env.LOGIN_DESCRIPTION
    || process.env.NEXT_PUBLIC_LOGIN_DESCRIPTION
    || DEFAULT_CONFIG.loginDescription;
  const loginSubDescription = process.env.LOGIN_SUB_DESCRIPTION
    || process.env.NEXT_PUBLIC_LOGIN_SUB_DESCRIPTION
    || DEFAULT_CONFIG.loginSubDescription;

  // カスタム favicon URL を取得
  const faviconUrl = process.env.FAVICON_URL || null;

  return NextResponse.json({
    loginTitle,
    loginDescription,
    loginSubDescription,
    oauthProviders: DEFAULT_CONFIG.oauthProviders,
    // Push通知設定
    vapidPublicKey: (vapidPublicKey && /^[A-Za-z0-9_-]+$/.test(vapidPublicKey)) ? vapidPublicKey : null,
    // カスタマイズ設定
    faviconUrl,
  }, {
    headers: {
      'Cache-Control': 'no-store, no-cache, must-revalidate, proxy-revalidate',
      'Pragma': 'no-cache',
      'Expires': '0',
    }
  })
}
