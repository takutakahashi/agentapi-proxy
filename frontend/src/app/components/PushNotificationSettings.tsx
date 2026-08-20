'use client';

import React, { useState, useEffect } from 'react';
import { pushNotificationManager } from '../../utils/pushNotification';

interface NotificationStatus {
  isSupported: boolean;
  permission: NotificationPermission;
  isSubscribed: boolean;
  endpoint?: string;
}

export const PushNotificationSettings: React.FC = () => {
  const [status, setStatus] = useState<NotificationStatus>({
    isSupported: false,
    permission: 'default',
    isSubscribed: false
  });
  const [isLoading, setIsLoading] = useState(false);
  const [message, setMessage] = useState('');

  useEffect(() => {
    checkNotificationStatus();
  }, []);

  const checkNotificationStatus = () => {
    const isSupported = 'serviceWorker' in navigator && 'PushManager' in window;
    const permission = isSupported ? Notification.permission : 'denied';
    const subscriptionStatus = pushNotificationManager.getSubscriptionStatus();
    
    setStatus({
      isSupported,
      permission,
      isSubscribed: subscriptionStatus.isSubscribed,
      endpoint: subscriptionStatus.endpoint
    });
  };

  const handleInitialize = async () => {
    setIsLoading(true);
    setMessage('');

    try {
      const initialized = await pushNotificationManager.initialize();
      if (initialized) {
        setMessage('Service Worker が正常に初期化されました');
        checkNotificationStatus();
      } else {
        setMessage('Service Worker の初期化に失敗しました');
      }
    } catch (error) {
      setMessage(`初期化エラー: ${error instanceof Error ? error.message : '不明なエラー'}`);
    } finally {
      setIsLoading(false);
    }
  };

  const handleRequestPermission = async () => {
    setIsLoading(true);
    setMessage('');

    try {
      const granted = await pushNotificationManager.requestPermission();
      if (granted) {
        setMessage('通知許可が取得されました');
        checkNotificationStatus();
      } else {
        setMessage('通知許可が拒否されました');
      }
    } catch (error) {
      setMessage(`許可エラー: ${error instanceof Error ? error.message : '不明なエラー'}`);
    } finally {
      setIsLoading(false);
    }
  };

  const handleSubscribe = async () => {
    setIsLoading(true);
    setMessage('');

    try {
      const subscribed = await pushNotificationManager.subscribe();
      if (subscribed) {
        setMessage('プッシュ通知の購読が完了しました');
        checkNotificationStatus();
      } else {
        setMessage('プッシュ通知の購読に失敗しました');
      }
    } catch (error) {
      setMessage(`購読エラー: ${error instanceof Error ? error.message : '不明なエラー'}`);
    } finally {
      setIsLoading(false);
    }
  };

  const handleUnsubscribe = async () => {
    setIsLoading(true);
    setMessage('');

    try {
      const unsubscribed = await pushNotificationManager.unsubscribe();
      if (unsubscribed) {
        setMessage('プッシュ通知の購読が解除されました');
        checkNotificationStatus();
      } else {
        setMessage('プッシュ通知の購読解除に失敗しました');
      }
    } catch (error) {
      setMessage(`購読解除エラー: ${error instanceof Error ? error.message : '不明なエラー'}`);
    } finally {
      setIsLoading(false);
    }
  };

  const handleTestNotification = async () => {
    setIsLoading(true);
    setMessage('');

    // ローカル通知機能は削除されました
    setMessage('テスト通知機能は無効化されています（ローカル通知機能削除済み）');
    setIsLoading(false);
  };

  const getPermissionText = (permission: NotificationPermission) => {
    switch (permission) {
      case 'granted': return '許可済み';
      case 'denied': return '拒否済み';
      default: return '未設定';
    }
  };

  const getPermissionColor = (permission: NotificationPermission) => {
    switch (permission) {
      case 'granted': return 'text-green-600';
      case 'denied': return 'text-red-600';
      default: return 'text-yellow-600';
    }
  };

  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
      <h3 className="text-lg font-semibold mb-4 text-gray-900 dark:text-white">
        プッシュ通知設定
      </h3>

      <div className="space-y-4">
        {/* ステータス表示 */}
        <div className="grid grid-cols-2 gap-4 text-sm">
          <div>
            <span className="font-medium text-gray-700 dark:text-gray-300">対応状況:</span>
            <span className={`ml-2 ${status.isSupported ? 'text-green-600' : 'text-red-600'}`}>
              {status.isSupported ? 'サポート済み' : '非対応'}
            </span>
          </div>
          <div>
            <span className="font-medium text-gray-700 dark:text-gray-300">許可状況:</span>
            <span className={`ml-2 ${getPermissionColor(status.permission)}`}>
              {getPermissionText(status.permission)}
            </span>
          </div>
          <div className="col-span-2">
            <span className="font-medium text-gray-700 dark:text-gray-300">購読状況:</span>
            <span className={`ml-2 ${status.isSubscribed ? 'text-green-600' : 'text-gray-600'}`}>
              {status.isSubscribed ? '購読中' : '未購読'}
            </span>
          </div>
        </div>

        {/* メッセージ表示 */}
        {message && (
          <div className="p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md">
            <p className="text-sm text-blue-800 dark:text-blue-200">{message}</p>
          </div>
        )}

        {/* ボタン群 */}
        <div className="flex flex-wrap gap-2">
          {!status.isSupported && (
            <div className="w-full p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md">
              <p className="text-sm text-red-800 dark:text-red-200">
                お使いのブラウザはプッシュ通知をサポートしていません
              </p>
            </div>
          )}

          {status.isSupported && (
            <>
              <button
                onClick={handleInitialize}
                disabled={isLoading}
                className="px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-md hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {isLoading ? '初期化中...' : 'Service Worker 初期化'}
              </button>

              {status.permission === 'default' && (
                <button
                  onClick={handleRequestPermission}
                  disabled={isLoading}
                  className="px-4 py-2 text-sm font-medium text-white bg-yellow-600 rounded-md hover:bg-yellow-700 disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {isLoading ? '許可要求中...' : '通知許可を取得'}
                </button>
              )}

              {status.permission === 'granted' && !status.isSubscribed && (
                <button
                  onClick={handleSubscribe}
                  disabled={isLoading}
                  className="px-4 py-2 text-sm font-medium text-white bg-green-600 rounded-md hover:bg-green-700 disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {isLoading ? '購読中...' : 'プッシュ通知を有効化'}
                </button>
              )}

              {status.isSubscribed && (
                <>
                  <button
                    onClick={handleTestNotification}
                    disabled={isLoading}
                    className="px-4 py-2 text-sm font-medium text-white bg-purple-600 rounded-md hover:bg-purple-700 disabled:opacity-50 disabled:cursor-not-allowed"
                  >
                    {isLoading ? '送信中...' : 'テスト通知送信'}
                  </button>
                  <button
                    onClick={handleUnsubscribe}
                    disabled={isLoading}
                    className="px-4 py-2 text-sm font-medium text-white bg-red-600 rounded-md hover:bg-red-700 disabled:opacity-50 disabled:cursor-not-allowed"
                  >
                    {isLoading ? '解除中...' : 'プッシュ通知を無効化'}
                  </button>
                </>
              )}
            </>
          )}
        </div>

        {/* エンドポイント情報（デバッグ用） */}
        {status.endpoint && (
          <div className="text-xs text-gray-500 dark:text-gray-400 break-all">
            <span className="font-medium">エンドポイント:</span>
            <br />
            {status.endpoint.substring(0, 100)}...
          </div>
        )}
      </div>
    </div>
  );
};