'use client';

import { useState, useEffect, useCallback } from 'react';
import { AgentAPIProxyClient, AgentAPIProxyError } from '../../lib/agentapi-proxy-client';
import { ShareStatus } from '../../types/share';

interface ShareSessionButtonProps {
  sessionId: string;
  agentAPI: AgentAPIProxyClient;
}

export default function ShareSessionButton({ sessionId, agentAPI }: ShareSessionButtonProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [shareStatus, setShareStatus] = useState<ShareStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  // 共有状態を取得
  const fetchShareStatus = useCallback(async () => {
    try {
      const status = await agentAPI.getShareStatus(sessionId);
      setShareStatus(status);
    } catch (err) {
      console.error('Failed to fetch share status:', err);
    }
  }, [agentAPI, sessionId]);

  useEffect(() => {
    if (isOpen) {
      fetchShareStatus();
    }
  }, [isOpen, fetchShareStatus]);

  // 共有URLを作成
  const handleCreateShare = async () => {
    setIsLoading(true);
    setError(null);
    try {
      const result = await agentAPI.createShare(sessionId);
      setShareStatus({
        token: result.token,
        session_id: result.session_id,
        share_url: result.share_url,
        created_by: '',
        created_at: result.created_at,
        expires_at: result.expires_at,
        is_expired: false
      });
    } catch (err) {
      console.error('Failed to create share:', err);
      if (err instanceof AgentAPIProxyError) {
        setError(`共有リンクの作成に失敗しました: ${err.message}`);
      } else {
        setError('共有リンクの作成に失敗しました');
      }
    } finally {
      setIsLoading(false);
    }
  };

  // 共有を取り消し
  const handleRevokeShare = async () => {
    if (!confirm('共有リンクを取り消しますか？この操作を行うと、共有URLは無効になります。')) {
      return;
    }

    setIsLoading(true);
    setError(null);
    try {
      await agentAPI.revokeShare(sessionId);
      setShareStatus(null);
    } catch (err) {
      console.error('Failed to revoke share:', err);
      if (err instanceof AgentAPIProxyError) {
        setError(`共有の取り消しに失敗しました: ${err.message}`);
      } else {
        setError('共有の取り消しに失敗しました');
      }
    } finally {
      setIsLoading(false);
    }
  };

  // URLをクリップボードにコピー
  const handleCopyUrl = async () => {
    if (!shareStatus) return;

    const fullUrl = `${window.location.origin}${shareStatus.share_url}`;
    try {
      await navigator.clipboard.writeText(fullUrl);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      console.error('Failed to copy URL:', err);
      setError('URLのコピーに失敗しました');
    }
  };

  const formatDate = (dateString: string | null) => {
    if (!dateString) return '無期限';
    return new Date(dateString).toLocaleString('ja-JP');
  };

  return (
    <>
      {/* Share Button */}
      <button
        onClick={() => setIsOpen(true)}
        className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors"
        title="セッションを共有"
      >
        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8.684 13.342C8.886 12.938 9 12.482 9 12c0-.482-.114-.938-.316-1.342m0 2.684a3 3 0 110-2.684m0 2.684l6.632 3.316m-6.632-6l6.632-3.316m0 0a3 3 0 105.367-2.684 3 3 0 00-5.367 2.684zm0 9.316a3 3 0 105.368 2.684 3 3 0 00-5.368-2.684z" />
        </svg>
      </button>

      {/* Modal */}
      {isOpen && (
        <div
          className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
          onClick={(e) => {
            if (e.target === e.currentTarget) {
              setIsOpen(false);
            }
          }}
        >
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-md w-full">
            <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
                  セッションを共有
                </h2>
                <button
                  onClick={() => setIsOpen(false)}
                  className="text-gray-400 hover:text-gray-500 dark:hover:text-gray-300"
                >
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
            </div>

            <div className="px-6 py-4">
              {error && (
                <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-red-700 dark:text-red-400 text-sm">
                  {error}
                </div>
              )}

              {shareStatus ? (
                // 共有中の状態
                <div className="space-y-4">
                  <div className="flex items-center space-x-2">
                    <svg className="w-5 h-5 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                    </svg>
                    <span className="text-sm font-medium text-gray-900 dark:text-white">
                      このセッションは共有されています
                    </span>
                  </div>

                  {/* 共有URL */}
                  <div className="p-3 bg-gray-50 dark:bg-gray-700 rounded-md">
                    <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">
                      共有URL
                    </label>
                    <div className="flex items-center space-x-2">
                      <input
                        type="text"
                        readOnly
                        value={`${typeof window !== 'undefined' ? window.location.origin : ''}${shareStatus.share_url}`}
                        className="flex-1 px-2 py-1 text-sm bg-white dark:bg-gray-600 border border-gray-300 dark:border-gray-500 rounded text-gray-900 dark:text-white"
                      />
                      <button
                        onClick={handleCopyUrl}
                        className={`px-3 py-1 text-sm rounded transition-colors ${
                          copied
                            ? 'bg-green-600 text-white'
                            : 'bg-blue-600 hover:bg-blue-700 text-white'
                        }`}
                      >
                        {copied ? 'コピー済み' : 'コピー'}
                      </button>
                    </div>
                  </div>

                  {/* 有効期限 */}
                  <div className="text-sm text-gray-600 dark:text-gray-400">
                    <span className="font-medium">有効期限:</span> {formatDate(shareStatus.expires_at)}
                  </div>

                  {/* 共有作成日時 */}
                  <div className="text-sm text-gray-600 dark:text-gray-400">
                    <span className="font-medium">作成日時:</span> {formatDate(shareStatus.created_at)}
                  </div>

                  {/* 注意事項 */}
                  <div className="p-3 bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-800 rounded-md">
                    <div className="flex items-start space-x-2">
                      <svg className="w-5 h-5 text-purple-500 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                      </svg>
                      <p className="text-sm text-purple-700 dark:text-purple-300">
                        共有リンクを知っているログインユーザーは、このセッションを読み取り専用で閲覧できます。
                      </p>
                    </div>
                  </div>

                  {/* 取り消しボタン */}
                  <button
                    onClick={handleRevokeShare}
                    disabled={isLoading}
                    className="w-full px-4 py-2 text-red-600 hover:text-red-700 dark:text-red-400 dark:hover:text-red-300 border border-red-300 dark:border-red-600 hover:bg-red-50 dark:hover:bg-red-900/20 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                  >
                    {isLoading ? '処理中...' : '共有を取り消す'}
                  </button>
                </div>
              ) : (
                // 未共有の状態
                <div className="space-y-4">
                  <p className="text-sm text-gray-600 dark:text-gray-400">
                    このセッションを他のユーザーと共有できます。共有されたセッションは読み取り専用で閲覧できます。
                  </p>

                  <div className="p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md">
                    <div className="flex items-start space-x-2">
                      <svg className="w-5 h-5 text-blue-500 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                      </svg>
                      <p className="text-sm text-blue-700 dark:text-blue-300">
                        共有リンクを持つログイン済みユーザーのみが閲覧できます。メッセージの送信はできません。
                      </p>
                    </div>
                  </div>

                  <button
                    onClick={handleCreateShare}
                    disabled={isLoading}
                    className="w-full px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white rounded-md transition-colors disabled:cursor-not-allowed"
                  >
                    {isLoading ? '作成中...' : '共有リンクを作成'}
                  </button>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  );
}
