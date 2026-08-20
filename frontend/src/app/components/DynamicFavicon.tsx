'use client';

import { useEffect } from 'react';
import { useConfig } from '../../hooks/useConfig';

/**
 * 環境変数で指定されたカスタム favicon を動的に設定するコンポーネント
 * FAVICON_URL 環境変数が設定されている場合、その URL を favicon として使用
 */
export const DynamicFavicon: React.FC = () => {
  const { config } = useConfig();

  useEffect(() => {
    if (!config?.faviconUrl) {
      return;
    }

    // 既存の favicon link タグを取得または作成
    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (!link) {
      link = document.createElement('link');
      link.rel = 'icon';
      document.head.appendChild(link);
    }

    // favicon URL を更新
    link.href = config.faviconUrl;
  }, [config?.faviconUrl]);

  // このコンポーネントは何も描画しない
  return null;
};
