/**
 * 共有セッション用APIクライアント
 * /s/{shareToken}/ 経由で共有セッションにアクセスするためのクライアント
 */

import { SessionMessageListResponse } from '../types/agentapi';
import { loadFullGlobalSettings, getDefaultProxySettings } from '../types/settings';

function defaultDebugEnabled(): boolean {
  return process.env.NODE_ENV !== 'production'
    && (process.env.NEXT_PUBLIC_DEBUG_LOGS === 'true' || process.env.DEBUG_LOGS === 'true');
}

export class SharedSessionClientError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string
  ) {
    super(message);
    this.name = 'SharedSessionClientError';
  }
}

export interface SharedSessionClientConfig {
  baseURL: string;
  timeout?: number;
  debug?: boolean;
}

/**
 * 共有セッション用の読み取り専用クライアント
 */
export class SharedSessionClient {
  private baseURL: string;
  private timeout: number;
  private debug: boolean;

  constructor(config: SharedSessionClientConfig) {
    this.baseURL = config.baseURL.replace(/\/$/, '');
    this.timeout = config.timeout || 10000;
    this.debug = process.env.NODE_ENV !== 'production' && config.debug === true;
  }

  private async makeRequest<T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<T> {
    const url = `${this.baseURL}${endpoint}`;

    const headers: HeadersInit = {
      'Content-Type': 'application/json',
      'Accept': 'application/json',
    };

    const requestOptions: RequestInit = {
      ...options,
      headers: {
        ...headers,
        ...options.headers,
      },
    };

    if (this.debug) {
      console.log(`[SharedSessionClient] Starting public share request (${options.method || 'GET'})`);
    }

    try {
      const response = await fetch(url, requestOptions);

      if (!response.ok) {
        const errorData = await response.json().catch(() => ({
          error: {
            code: 'UNKNOWN_ERROR',
            message: `HTTP ${response.status}: ${response.statusText}`,
          }
        }));

        // 特定のエラーコードをマッピング
        let code = errorData.error?.code || 'UNKNOWN_ERROR';
        if (response.status === 404) code = 'SHARE_NOT_FOUND';
        if (response.status === 410) code = 'SHARE_EXPIRED';
        if (response.status === 403) code = 'SHARE_FORBIDDEN';

        throw new SharedSessionClientError(
          response.status,
          code,
          errorData.error?.message || `HTTP ${response.status}`
        );
      }

      const data = await response.json();

      if (this.debug) {
        console.log(`[SharedSessionClient] Public share request completed (${options.method || 'GET'})`);
      }

      return data;
    } catch (error) {
      if (error instanceof SharedSessionClientError) {
        throw error;
      }

      throw new SharedSessionClientError(
        0,
        'NETWORK_ERROR',
        error instanceof Error ? error.message : 'Unknown network error'
      );
    }
  }

  /**
   * 共有セッションのメッセージ一覧を取得
   * GET /s/{shareToken}/messages
   */
  async getMessages(shareToken: string, params?: { limit?: number; page?: number }): Promise<SessionMessageListResponse> {
    const searchParams = new URLSearchParams();
    if (params?.limit) searchParams.set('limit', params.limit.toString());
    if (params?.page) searchParams.set('page', params.page.toString());

    const endpoint = `/s/${shareToken}/messages${searchParams.toString() ? `?${searchParams.toString()}` : ''}`;
    const result = await this.makeRequest<SessionMessageListResponse>(endpoint);

    return {
      ...result,
      messages: result.messages || []
    };
  }

  /**
   * 共有セッションのステータスを取得
   * GET /s/{shareToken}/status
   */
  async getStatus(shareToken: string): Promise<{ status: string; last_activity?: string }> {
    return this.makeRequest(`/s/${shareToken}/status`);
  }

  /**
   * デバッグモードを設定
   */
  setDebug(debug: boolean): void {
    this.debug = process.env.NODE_ENV !== 'production' && debug;
  }
}

/**
 * ストレージから設定を読み込んでクライアントを作成
 */
export function createSharedSessionClientFromStorage(): SharedSessionClient {
  if (typeof window === 'undefined') {
    return new SharedSessionClient({
      baseURL: process.env.AGENTAPI_PROXY_URL || 'http://localhost:8080',
      timeout: parseInt(process.env.AGENTAPI_TIMEOUT || '10000'),
      debug: defaultDebugEnabled(),
    });
  }

  try {
    const globalSettings = loadFullGlobalSettings();
    const proxySettings = globalSettings.agentApiProxy || getDefaultProxySettings();

    const baseURL = proxySettings.enabled
      ? proxySettings.endpoint
      : `${window.location.protocol}//${window.location.host}/api/proxy`;

    return new SharedSessionClient({
      baseURL,
      timeout: proxySettings.timeout,
      debug: defaultDebugEnabled(),
    });
  } catch {
    if (defaultDebugEnabled()) {
      console.warn('Failed to load settings for SharedSessionClient');
    }
    return new SharedSessionClient({
      baseURL: `${window.location.protocol}//${window.location.host}/api/proxy`,
      timeout: 10000,
      debug: defaultDebugEnabled(),
    });
  }
}
