'use client'

import { useState, useEffect } from 'react'
import { type AppConfig, DEFAULT_CONFIG } from '@/types/config'

interface UseConfigReturn {
  config: AppConfig | null;
  isLoading: boolean;
  error: Error | null;
}

/**
 * アプリケーション設定を取得するフック
 * /api/config エンドポイントから設定を取得する
 */
export function useConfig(): UseConfigReturn {
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    const fetchConfig = async () => {
      try {
        const response = await fetch('/api/config');
        if (!response.ok) {
          throw new Error('Failed to fetch config');
        }
        const data = await response.json();
        setConfig({
          loginTitle: data.loginTitle ?? DEFAULT_CONFIG.loginTitle,
          loginDescription: data.loginDescription ?? DEFAULT_CONFIG.loginDescription,
          loginSubDescription: data.loginSubDescription ?? DEFAULT_CONFIG.loginSubDescription,
          oauthProviders: data.oauthProviders ?? DEFAULT_CONFIG.oauthProviders,
          faviconUrl: data.faviconUrl ?? DEFAULT_CONFIG.faviconUrl,
        });
      } catch (err) {
        setError(err instanceof Error ? err : new Error('Unknown error'));
        // エラー時はデフォルト設定を使用
        setConfig(DEFAULT_CONFIG);
      } finally {
        setIsLoading(false);
      }
    };

    fetchConfig();
  }, []);

  return { config, isLoading, error };
}
