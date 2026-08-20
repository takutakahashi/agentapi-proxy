'use client';

import { useEffect, useCallback } from 'react';
import ProgressBar from './ProgressBar';
import ProgressStep from './ProgressStep';
import {
  SessionCreationProgress,
  SESSION_CREATION_STEPS,
  getProgressPercentage,
  getStepIndex,
} from '../../types/sessionProgress';

interface SessionCreationProgressModalProps {
  isOpen: boolean;
  progress: SessionCreationProgress;
  onClose?: () => void;
  onRetry?: () => void;
}

export default function SessionCreationProgressModal({
  isOpen,
  progress,
  onClose,
  onRetry,
}: SessionCreationProgressModalProps) {
  const { status, message, repository, errorMessage } = progress;
  const isFailed = status === 'failed';
  const isCompleted = status === 'completed';
  const currentStepIndex = getStepIndex(status);
  const percentage = getProgressPercentage(status);

  // ESCキーでモーダルを閉じる（失敗時のみ）
  const handleKeyDown = useCallback(
    (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isFailed && onClose) {
        onClose();
      }
    },
    [isFailed, onClose]
  );

  useEffect(() => {
    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown);
    }
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [isOpen, handleKeyDown]);

  if (!isOpen) return null;

  const getStepStatus = (stepIndex: number): 'pending' | 'active' | 'completed' | 'failed' => {
    if (isFailed && stepIndex === currentStepIndex) return 'failed';
    if (stepIndex < currentStepIndex || isCompleted) return 'completed';
    if (stepIndex === currentStepIndex) return 'active';
    return 'pending';
  };

  const getStepDescription = (stepIndex: number): string | undefined => {
    const stepStatus = getStepStatus(stepIndex);
    const step = SESSION_CREATION_STEPS[stepIndex];

    if (stepStatus === 'active') {
      return step.activeLabel;
    }
    if (stepStatus === 'completed') {
      return step.completedLabel;
    }
    if (stepStatus === 'failed') {
      return errorMessage || 'エラーが発生しました';
    }
    return undefined;
  };

  const getModalTitle = () => {
    if (isCompleted) return 'セッション作成完了';
    if (isFailed) return 'セッション作成失敗';
    return 'セッション作成中';
  };

  const getBarStatus = (): 'active' | 'completed' | 'failed' => {
    if (isCompleted) return 'completed';
    if (isFailed) return 'failed';
    return 'active';
  };

  // メッセージを省略表示
  const truncateMessage = (msg: string, maxLength: number = 100): string => {
    if (msg.length <= maxLength) return msg;
    return msg.slice(0, maxLength) + '...';
  };

  return (
    <div
      className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
      onClick={(e) => {
        if (e.target === e.currentTarget && isFailed && onClose) {
          onClose();
        }
      }}
    >
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md">
        {/* ヘッダー */}
        <div className="p-6 pb-4">
          <div className="flex justify-between items-center">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
              {getModalTitle()}
            </h2>
            {isFailed && onClose && (
              <button
                onClick={onClose}
                className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
                aria-label="閉じる"
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            )}
          </div>
        </div>

        {/* プログレスバー */}
        <div className="px-6 pb-4">
          <ProgressBar
            percentage={percentage}
            status={getBarStatus()}
            showPercentage={!isCompleted && !isFailed}
            size="md"
          />
        </div>

        {/* ステップリスト */}
        <div className="px-6 pb-4 space-y-3">
          {SESSION_CREATION_STEPS.map((step, index) => (
            <ProgressStep
              key={step.status}
              number={index + 1}
              label={step.label}
              status={getStepStatus(index)}
              description={getStepDescription(index)}
              showConnector={index < SESSION_CREATION_STEPS.length - 1}
            />
          ))}
        </div>

        {/* セッション情報 */}
        <div className="px-6 pb-4">
          <div className="bg-gray-50 dark:bg-gray-700/50 rounded-lg p-3">
            {repository && (
              <div className="flex items-start mb-2">
                <span className="text-xs text-gray-500 dark:text-gray-400 w-20 flex-shrink-0">
                  リポジトリ
                </span>
                <span className="text-sm text-gray-700 dark:text-gray-300 break-all">
                  {repository}
                </span>
              </div>
            )}
            <div className="flex items-start">
              <span className="text-xs text-gray-500 dark:text-gray-400 w-20 flex-shrink-0">
                メッセージ
              </span>
              <span className="text-sm text-gray-700 dark:text-gray-300 break-all">
                {truncateMessage(message)}
              </span>
            </div>
          </div>
        </div>

        {/* エラーメッセージ */}
        {isFailed && errorMessage && (
          <div className="px-6 pb-4">
            <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-3">
              <p className="text-sm text-red-600 dark:text-red-400">{errorMessage}</p>
            </div>
          </div>
        )}

        {/* アクションボタン（失敗時のみ） */}
        {isFailed && (
          <div className="px-6 pb-6 flex justify-end gap-3">
            {onClose && (
              <button
                onClick={onClose}
                className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors"
              >
                閉じる
              </button>
            )}
            {onRetry && (
              <button
                onClick={onRetry}
                className="px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 rounded-md transition-colors"
              >
                再試行
              </button>
            )}
          </div>
        )}

        {/* 完了時のメッセージ */}
        {isCompleted && (
          <div className="px-6 pb-6">
            <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg p-3">
              <p className="text-sm text-green-600 dark:text-green-400">
                セッションが正常に作成されました。リダイレクト中...
              </p>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
