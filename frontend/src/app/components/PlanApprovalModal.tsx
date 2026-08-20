'use client';

import { useState, useEffect } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

interface PlanApprovalModalProps {
  isOpen: boolean;
  planContent: string;
  onApprove: () => void;
  onReject: () => void;
  onClose: () => void;
  isLoading?: boolean;
}

export default function PlanApprovalModal({
  isOpen,
  planContent,
  onApprove,
  onReject,
  onClose,
  isLoading = false
}: PlanApprovalModalProps) {
  const [isApproving, setIsApproving] = useState(false);
  const [isRejecting, setIsRejecting] = useState(false);

  // ESCキーでモーダルを閉じる
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isOpen) {
        onClose();
      }
    };

    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown);
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  const handleApprove = async () => {
    setIsApproving(true);
    try {
      await onApprove();
    } finally {
      setIsApproving(false);
    }
  };

  const handleReject = async () => {
    setIsRejecting(true);
    try {
      await onReject();
    } finally {
      setIsRejecting(false);
    }
  };

  // Extract plan from JSON structure if needed
  let planMarkdown = planContent;

  try {
    // planContentがすでにオブジェクトの場合
    if (typeof planContent === 'object' && planContent !== null) {
      const obj = planContent as { plan?: string };
      if (obj.plan && typeof obj.plan === 'string') {
        planMarkdown = obj.plan;
      }
    }
    // planContentが文字列の場合
    else if (typeof planContent === 'string') {
      // JSONとしてパースを試みる
      try {
        const parsed = JSON.parse(planContent);
        if (parsed && typeof parsed === 'object' && parsed.plan && typeof parsed.plan === 'string') {
          planMarkdown = parsed.plan;
        } else {
          // JSONとしてパースできたが、planフィールドがない場合は元の文字列を使用
          planMarkdown = planContent;
        }
      } catch {
        // JSONパースに失敗した場合は、元の文字列をそのまま使用（マークダウンの可能性）
        planMarkdown = planContent;
      }
    }
  } catch (e) {
    console.error('[PlanApprovalModal] Error during plan extraction:', e);
    planMarkdown = String(planContent);
  }

  return (
    <div className="fixed inset-0 z-50 overflow-y-auto" aria-labelledby="modal-title" role="dialog" aria-modal="true">
      <div className="flex items-center justify-center min-h-screen pt-4 px-4 pb-20 text-center sm:block sm:p-0">
        {/* Background overlay */}
        <div
          className="fixed inset-0 bg-gray-500 dark:bg-gray-900 bg-opacity-75 dark:bg-opacity-75 transition-opacity"
          aria-hidden="true"
          onClick={onClose}
        ></div>

        {/* Modal panel */}
        <div className="inline-block align-bottom bg-white dark:bg-gray-800 rounded-lg text-left overflow-hidden shadow-xl transform transition-all sm:my-8 sm:align-middle sm:max-w-5xl sm:w-full">
          {/* Header */}
          <div className="bg-amber-50 dark:bg-amber-900/20 px-4 sm:px-6 py-4 border-b border-amber-200 dark:border-amber-800">
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <svg className="w-6 h-6 text-amber-600 dark:text-amber-400" fill="currentColor" viewBox="0 0 24 24">
                  <path d="M9 2v2H7v2H5v2H3v12h18V8h-2V6h-2V4h-2V2H9zm0 2h6v2h2v2h2v10H5V8h2V6h2V4zm2 4v2h2V8h-2zm-4 4v2h10v-2H7z"/>
                </svg>
                <h3 className="text-lg font-medium text-amber-900 dark:text-amber-100" id="modal-title">
                  📋 Plan Ready for Approval
                </h3>
              </div>
              <button
                onClick={onClose}
                className="text-gray-400 hover:text-gray-500 dark:hover:text-gray-300 focus:outline-none"
              >
                <svg className="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
          </div>

          {/* Content */}
          <div className="bg-white dark:bg-gray-800 px-4 sm:px-6 py-4 max-h-[60vh] overflow-y-auto">
            <div className="prose prose-sm max-w-none dark:prose-invert prose-headings:text-gray-900 dark:prose-headings:text-gray-100 prose-p:text-gray-700 dark:prose-p:text-gray-300 prose-a:text-blue-600 dark:prose-a:text-blue-400 prose-code:text-pink-600 dark:prose-code:text-pink-400 prose-pre:bg-gray-100 dark:prose-pre:bg-gray-900 prose-li:text-gray-700 dark:prose-li:text-gray-300">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
                {planMarkdown}
              </ReactMarkdown>
            </div>
          </div>

          {/* Footer */}
          <div className="bg-gray-50 dark:bg-gray-900 px-4 sm:px-6 py-4 sm:flex sm:flex-row-reverse sm:gap-2 border-t border-gray-200 dark:border-gray-700">
            <button
              type="button"
              disabled={isLoading || isApproving || isRejecting}
              onClick={handleApprove}
              className="w-full inline-flex justify-center rounded-md border border-transparent shadow-sm px-4 py-2 bg-green-600 text-base font-medium text-white hover:bg-green-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-green-500 disabled:bg-gray-400 disabled:cursor-not-allowed sm:ml-3 sm:w-auto sm:text-sm transition-colors"
            >
              {isApproving ? (
                <>
                  <svg className="animate-spin -ml-1 mr-2 h-4 w-4 text-white" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                  </svg>
                  Approving...
                </>
              ) : (
                <>✓ Approve Plan</>
              )}
            </button>
            <button
              type="button"
              disabled={isLoading || isApproving || isRejecting}
              onClick={handleReject}
              className="mt-3 w-full inline-flex justify-center rounded-md border border-transparent shadow-sm px-4 py-2 bg-red-600 text-base font-medium text-white hover:bg-red-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-red-500 disabled:bg-gray-400 disabled:cursor-not-allowed sm:mt-0 sm:w-auto sm:text-sm transition-colors"
            >
              {isRejecting ? (
                <>
                  <svg className="animate-spin -ml-1 mr-2 h-4 w-4 text-white" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                  </svg>
                  Rejecting...
                </>
              ) : (
                <>✗ Reject Plan</>
              )}
            </button>
            <button
              type="button"
              disabled={isLoading || isApproving || isRejecting}
              onClick={onClose}
              className="mt-3 w-full inline-flex justify-center rounded-md border border-gray-300 dark:border-gray-600 shadow-sm px-4 py-2 bg-white dark:bg-gray-700 text-base font-medium text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-600 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:bg-gray-100 dark:disabled:bg-gray-800 disabled:cursor-not-allowed sm:mt-0 sm:w-auto sm:text-sm transition-colors"
            >
              Close
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
