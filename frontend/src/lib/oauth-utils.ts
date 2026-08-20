export function getRedirectUri(): string {
  // クライアントサイドでのみ実行されることを確認
  if (typeof window === 'undefined') {
    // サーバーサイドの場合は環境変数から取得
    return process.env.NEXT_PUBLIC_BASE_URL ? 
      `${process.env.NEXT_PUBLIC_BASE_URL}/api/auth/github/callback` : 
      'http://localhost:3000/api/auth/github/callback';
  }

  // window.location.originのフォールバック処理
  const origin = window.location.origin || 
    `${window.location.protocol}//${window.location.hostname}${window.location.port ? ':' + window.location.port : ''}`;

  return `${origin}/api/auth/github/callback`;
}