// 共有セッション関連の型定義

/**
 * 共有URL作成レスポンス
 * POST /sessions/{sessionId}/share
 */
export interface CreateShareResponse {
  token: string;
  session_id: string;
  share_url: string;
  created_at: string;
  expires_at: string | null;
}

/**
 * 共有状態取得レスポンス
 * GET /sessions/{sessionId}/share
 */
export interface ShareStatus {
  token: string;
  session_id: string;
  share_url: string;
  created_by: string;
  created_at: string;
  expires_at: string | null;
  is_expired: boolean;
}

/**
 * 共有取り消しレスポンス
 * DELETE /sessions/{sessionId}/share
 */
export interface RevokeShareResponse {
  message: string;
  session_id: string;
}

/**
 * 共有セッションエラー
 */
export interface ShareError {
  code: 'SHARE_NOT_FOUND' | 'SHARE_EXPIRED' | 'SHARE_FORBIDDEN' | 'SHARE_ERROR';
  message: string;
}
