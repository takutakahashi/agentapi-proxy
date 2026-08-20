/**
 * アプリケーション設定の型定義
 * /api/config エンドポイントから取得される設定
 */

export interface AppConfig {
  /** ログインページのタイトル */
  loginTitle: string;
  /** ログインページの説明文 */
  loginDescription: string;
  /** ログインページのサブ説明文 */
  loginSubDescription: string;
  /** 有効なOAuthプロバイダー */
  oauthProviders: string[];
  /** カスタム favicon URL */
  faviconUrl: string | null;
}

/** デフォルト設定 */
export const DEFAULT_CONFIG: AppConfig = {
  loginTitle: 'AgentAPI UI',
  loginDescription: 'Enter an access token or sign in with GitHub to continue.',
  loginSubDescription: 'Use any valid authentication token for your AgentAPI service.',
  oauthProviders: ['github'],
  faviconUrl: null,
};
