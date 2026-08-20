'use client';

import { useEffect } from 'react';
import { pushNotificationManager } from '../../utils/pushNotification';

export const PushNotificationAutoInit: React.FC = () => {
  useEffect(() => {
    // ページロード時に自動初期化を試行
    const initPushNotifications = async () => {
      try {
        await pushNotificationManager.autoInitialize();
      } catch (error) {
        console.debug('プッシュ通知の自動初期化に失敗:', error);
      }
    };

    // 少し遅延させて実行（ページロード完了後）
    const timer = setTimeout(initPushNotifications, 1000);
    
    return () => clearTimeout(timer);
  }, []);

  // このコンポーネントは何も描画しない
  return null;
};