'use client';

import React, { useState, useEffect } from 'react';
import { pushNotificationManager } from '../../utils/pushNotification';
import { pushNotificationSettings } from '../../lib/pushNotificationSettings';

interface NotificationState {
  isEnabled: boolean;
  isSupported: boolean;
  permission: NotificationPermission;
  isLoading: boolean;
  lastMessage: string;
  endpoint?: string;
}

export const OneClickPushNotifications: React.FC = () => {
  const [state, setState] = useState<NotificationState>({
    isEnabled: false,
    isSupported: false,
    permission: 'default',
    isLoading: false,
    lastMessage: ''
  });

  useEffect(() => {
    checkStatus();
    // 自動初期化を試行
    if (pushNotificationSettings.isEnabled()) {
      pushNotificationManager.autoInitialize();
    }
  }, []);

  const checkStatus = () => {
    const isSupported = 'serviceWorker' in navigator && 'PushManager' in window;
    const permission = isSupported ? Notification.permission : 'denied';
    const settings = pushNotificationSettings.getSettings();
    const subscriptionStatus = pushNotificationManager.getSubscriptionStatus();
    
    setState(prev => ({
      ...prev,
      isSupported,
      permission,
      isEnabled: settings.enabled,
      endpoint: settings.endpoint || subscriptionStatus.endpoint
    }));
  };

  const handleOneClickEnable = async () => {
    setState(prev => ({ ...prev, isLoading: true, lastMessage: '' }));

    try {
      const result = await pushNotificationManager.enableOneClick();
      setState(prev => ({
        ...prev,
        isLoading: false,
        lastMessage: result.message,
        isEnabled: result.success
      }));
      
      if (result.success) {
        checkStatus(); // ステータスを更新
      }
    } catch (error) {
      setState(prev => ({
        ...prev,
        isLoading: false,
        lastMessage: `エラー: ${error instanceof Error ? error.message : '不明なエラー'}`,
        isEnabled: false
      }));
    }
  };

  const handleOneClickDisable = async () => {
    setState(prev => ({ ...prev, isLoading: true, lastMessage: '' }));

    try {
      const result = await pushNotificationManager.disableOneClick();
      setState(prev => ({
        ...prev,
        isLoading: false,
        lastMessage: result.message,
        isEnabled: !result.success ? prev.isEnabled : false
      }));
      
      if (result.success) {
        checkStatus(); // ステータスを更新
      }
    } catch (error) {
      setState(prev => ({
        ...prev,
        isLoading: false,
        lastMessage: `エラー: ${error instanceof Error ? error.message : '不明なエラー'}`
      }));
    }
  };

  const handleTestNotification = async () => {
    setState(prev => ({ ...prev, isLoading: true, lastMessage: '' }));

    // ローカル通知機能は削除されました
    setState(prev => ({
      ...prev,
      isLoading: false,
      lastMessage: 'テスト通知機能は無効化されています（ローカル通知機能削除済み）'
    }));
  };


  const getStatusText = () => {
    if (!state.isSupported) return '非対応';
    if (state.permission === 'denied') return '許可が拒否されています';
    if (state.isEnabled) return '有効';
    return '無効';
  };

  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
      <h3 className="text-lg font-semibold mb-4 text-gray-900 dark:text-white">
        プッシュ通知
      </h3>

      {/* 現在のステータス */}
      <div className="mb-6 p-4 bg-gray-50 dark:bg-gray-700 rounded-lg border border-gray-200 dark:border-gray-600">
        <div className="flex items-center justify-between mb-3">
          <div className="flex items-center">
            <div className={`w-3 h-3 rounded-full mr-3 ${
              !state.isSupported || state.permission === 'denied' 
                ? 'bg-red-400' 
                : state.isEnabled 
                  ? 'bg-green-400' 
                  : 'bg-yellow-400'
            }`}></div>
            <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
              ステータス
            </span>
          </div>
          <span className={`text-sm font-semibold px-2 py-1 rounded-full ${
            !state.isSupported || state.permission === 'denied'
              ? 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300'
              : state.isEnabled
                ? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300'
                : 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300'
          }`}>
            {getStatusText()}
          </span>
        </div>
        
        {state.isSupported && (
          <div className="flex items-center text-xs text-gray-500 dark:text-gray-400">
            <svg className="w-3 h-3 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            許可状況: {state.permission === 'granted' ? '許可済み' : 
                      state.permission === 'denied' ? '拒否済み' : '未設定'}
          </div>
        )}
        
        {state.endpoint && (
          <div className="flex items-start text-xs text-gray-400 dark:text-gray-500 mt-2">
            <svg className="w-3 h-3 mr-2 mt-0.5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
            </svg>
            <span className="break-all">エンドポイント: {state.endpoint.substring(0, 60)}...</span>
          </div>
        )}
      </div>

      {/* メッセージ表示 */}
      {state.lastMessage && (
        <div className={`mb-4 p-3 rounded-md ${
          state.lastMessage.includes('エラー') || state.lastMessage.includes('失敗')
            ? 'bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-200'
            : 'bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 text-green-800 dark:text-green-200'
        }`}>
          <p className="text-sm">{state.lastMessage}</p>
        </div>
      )}

      {/* ワンクリックボタン */}
      <div className="space-y-3">
        {!state.isSupported ? (
          <div className="p-4 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
            <div className="flex items-center">
              <svg className="w-5 h-5 text-red-600 dark:text-red-400 mr-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4.5c-.77-.833-2.694-.833-3.464 0L3.34 16.5c-.77.833.192 2.5 1.732 2.5z" />
              </svg>
              <p className="text-sm text-red-800 dark:text-red-200 font-medium">
                お使いのブラウザはプッシュ通知をサポートしていません
              </p>
            </div>
          </div>
        ) : (
          <>
            {!state.isEnabled ? (
              <button
                onClick={handleOneClickEnable}
                disabled={state.isLoading}
                className="w-full inline-flex items-center justify-center px-6 py-3 bg-main-color hover:bg-main-color-dark text-white font-medium rounded-lg transition-all duration-200 focus:outline-none focus:ring-2 focus:ring-main-color focus:ring-offset-2 disabled:opacity-50 disabled:cursor-not-allowed shadow-sm"
              >
                {state.isLoading ? (
                  <>
                    <div className="animate-spin rounded-full h-4 w-4 border-b-2 border-white mr-3"></div>
                    有効化中...
                  </>
                ) : (
                  <>
                    <svg className="w-5 h-5 mr-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 17h5l-5 5-5-5h5v-5a7.5 7.5 0 0 0-15 0v5c0 1.38 1.12 2.5 2.5 2.5s2.5-1.12 2.5-2.5V12a3 3 0 0 1 6 0v5z" />
                    </svg>
                    プッシュ通知を有効にする
                  </>
                )}
              </button>
            ) : (
              <div className="space-y-3">
                <button
                  onClick={handleTestNotification}
                  disabled={state.isLoading}
                  className="w-full inline-flex items-center justify-center px-4 py-2.5 bg-blue-600 hover:bg-blue-700 text-white font-medium rounded-lg transition-all duration-200 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 disabled:opacity-50 disabled:cursor-not-allowed shadow-sm"
                >
                  {state.isLoading ? (
                    <>
                      <div className="animate-spin rounded-full h-4 w-4 border-b-2 border-white mr-2"></div>
                      送信中...
                    </>
                  ) : (
                    <>
                      <svg className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
                      </svg>
                      テスト通知を送信
                    </>
                  )}
                </button>
                
                <button
                  onClick={handleOneClickDisable}
                  disabled={state.isLoading}
                  className="w-full inline-flex items-center justify-center px-4 py-2.5 bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 font-medium rounded-lg transition-all duration-200 focus:outline-none focus:ring-2 focus:ring-gray-500 focus:ring-offset-2 disabled:opacity-50 disabled:cursor-not-allowed shadow-sm"
                >
                  {state.isLoading ? (
                    <>
                      <div className="animate-spin rounded-full h-4 w-4 border-b-2 border-gray-600 mr-2"></div>
                      無効化中...
                    </>
                  ) : (
                    <>
                      <svg className="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5.586 15H4a1 1 0 01-1-1v-4a1 1 0 011-1h1.586l4.707-4.707C10.923 3.663 12 4.109 12 5v14c0 .891-1.077 1.337-1.707.707L5.586 15z" />
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2" />
                      </svg>
                      通知を無効にする
                    </>
                  )}
                </button>
              </div>
            )}
          </>
        )}
      </div>

      {/* 設定情報 */}
      {state.isEnabled && (
        <div className="mt-4 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
          <div className="flex items-start">
            <svg className="w-5 h-5 text-blue-600 dark:text-blue-400 mr-3 mt-0.5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <div>
              <p className="text-sm font-medium text-blue-800 dark:text-blue-200 mb-1">
                プッシュ通知が有効です
              </p>
              <p className="text-xs text-blue-700 dark:text-blue-300">
                AgentAPIからの応答や重要な更新をリアルタイムで受け取れます
              </p>
            </div>
          </div>
        </div>
      )}

      {/* ヘルプ情報 */}
      <details className="mt-4 group">
        <summary className="flex items-center text-sm text-gray-600 dark:text-gray-400 cursor-pointer hover:text-gray-800 dark:hover:text-gray-200 transition-colors">
          <svg className="w-4 h-4 mr-2 transition-transform group-open:rotate-90" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
          </svg>
          設定について
        </summary>
        <div className="mt-3 ml-6 p-3 bg-gray-50 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700">
          <div className="text-xs text-gray-600 dark:text-gray-400 space-y-2">
            <div className="flex items-start">
              <div className="w-1.5 h-1.5 bg-gray-400 rounded-full mt-1.5 mr-2 flex-shrink-0"></div>
              <span>設定はブラウザに永続的に保存されます</span>
            </div>
            <div className="flex items-start">
              <div className="w-1.5 h-1.5 bg-gray-400 rounded-full mt-1.5 mr-2 flex-shrink-0"></div>
              <span>通知許可は一度拒否すると、ブラウザ設定から手動で有効にする必要があります</span>
            </div>
            <div className="flex items-start">
              <div className="w-1.5 h-1.5 bg-gray-400 rounded-full mt-1.5 mr-2 flex-shrink-0"></div>
              <span>プライベートブラウジングモードでは設定が保存されません</span>
            </div>
          </div>
        </div>
      </details>
    </div>
  );
};