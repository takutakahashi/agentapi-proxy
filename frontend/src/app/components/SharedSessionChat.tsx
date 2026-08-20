'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import Link from 'next/link';
import { createSharedSessionClientFromStorage, SharedSessionClientError } from '../../lib/shared-session-client';
import { SessionMessage, SessionMessageListResponse } from '../../types/agentapi';
import { useBackgroundAwareInterval } from '../hooks/usePageVisibility';

interface Message {
  id: number;
  role: 'user' | 'agent';
  content: string;
  timestamp: string;
}

interface AgentStatus {
  status: 'stable' | 'running' | 'error';
  last_activity?: string;
}

interface SharedSessionChatProps {
  shareToken: string;
}

function isValidSessionMessageResponse(response: unknown): response is SessionMessageListResponse {
  return (
    response !== null &&
    typeof response === 'object' &&
    'messages' in response &&
    Array.isArray((response as SessionMessageListResponse).messages)
  );
}

function convertSessionMessageId(id: string | number, fallbackId: number): number {
  const stringId = String(id);
  if (!stringId || stringId.trim() === '') {
    return fallbackId;
  }
  const parsed = parseInt(stringId, 10);
  if (!isNaN(parsed) && parsed >= 0) {
    return parsed;
  }
  let hash = 0;
  for (let i = 0; i < stringId.length; i++) {
    const char = stringId.charCodeAt(i);
    hash = ((hash << 5) - hash) + char;
    hash = hash & hash;
  }
  const hashId = Math.abs(hash);
  return hashId > 0 ? hashId : fallbackId;
}

function formatTextWithLinks(text: string): JSX.Element {
  if (!text || typeof text !== 'string') {
    return <>{text || ''}</>;
  }

  // URLにマッチする正規表現（スペースや一般的な区切り文字を除外）
  const urlRegex = /(https?:\/\/[^\s<>"'()[\]{}]+)/g;
  const parts = text.split(urlRegex);

  return (
    <>
      {parts.map((part, index) => {
        // URLかどうかをチェック（グローバルフラグの状態問題を回避）
        if (/^https?:\/\//.test(part)) {
          // 末尾の句読点を削除
          const cleanUrl = part.replace(/[.,;!?]+$/, '');
          return (
            <a
              key={index}
              href={cleanUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 underline whitespace-nowrap"
            >
              {cleanUrl}
            </a>
          );
        }
        return part;
      })}
    </>
  );
}

export default function SharedSessionChat({ shareToken }: SharedSessionChatProps) {
  const [sharedClient] = useState(() => createSharedSessionClientFromStorage());
  const sharedClientRef = useRef(sharedClient);

  useEffect(() => {
    sharedClientRef.current = sharedClient;
  }, [sharedClient]);

  const [messages, setMessages] = useState<Message[]>([]);
  const [agentStatus, setAgentStatus] = useState<AgentStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const [shouldAutoScroll, setShouldAutoScroll] = useState(true);
  const [hasNewMessages, setHasNewMessages] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const messagesContainerRef = useRef<HTMLDivElement>(null);
  const prevMessagesLengthRef = useRef(0);

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  };

  const checkIfAtBottom = () => {
    if (!messagesContainerRef.current) return false;
    const container = messagesContainerRef.current;
    const threshold = 100;
    return container.scrollHeight - container.scrollTop - container.clientHeight <= threshold;
  };

  const handleScroll = useCallback(() => {
    if (!messagesContainerRef.current) return;
    const isAtBottom = checkIfAtBottom();
    setShouldAutoScroll(isAtBottom);
    if (isAtBottom) {
      setHasNewMessages(false);
    }
  }, []);

  // Initialize chat
  useEffect(() => {
    if (sharedClient && shareToken) {
      const initializeChat = async () => {
        try {
          setError(null);
          setIsConnected(true);

          const sessionMessagesResponse = await sharedClientRef.current.getMessages(shareToken, { limit: 100 });

          if (!isValidSessionMessageResponse(sessionMessagesResponse)) {
            console.warn('Invalid session messages response structure:', sessionMessagesResponse);
          }

          const msgs = sessionMessagesResponse?.messages || [];
          const convertedMessages: Message[] = msgs.map((msg: SessionMessage, index: number) => ({
            id: convertSessionMessageId(msg.id, Date.now() + index),
            role: msg.role === 'assistant' ? 'agent' : (msg.role === 'system' ? 'agent' : msg.role as 'user' | 'agent'),
            content: msg.content,
            timestamp: msg.time || msg.timestamp || new Date().toISOString()
          }));

          setMessages(convertedMessages);
          setError(null);
        } catch (err) {
          console.error('Failed to load shared session messages:', err);
          setIsConnected(false);
          if (err instanceof SharedSessionClientError) {
            if (err.code === 'SHARE_NOT_FOUND') {
              setError('共有セッションが見つかりません。URLが正しいか確認してください。');
            } else if (err.code === 'SHARE_EXPIRED') {
              setError('この共有リンクは有効期限が切れています。');
            } else if (err.code === 'SHARE_FORBIDDEN') {
              setError('この共有セッションへのアクセス権限がありません。');
            } else {
              setError(`共有セッションの読み込みに失敗しました: ${err.message}`);
            }
          } else {
            setError('共有セッションへの接続に失敗しました');
          }
        }
      };

      const timeoutId = setTimeout(initializeChat, 0);
      return () => clearTimeout(timeoutId);
    }
  }, [shareToken, sharedClient]);

  // Polling for messages
  const pollMessages = useCallback(async () => {
    if (!isConnected || !shareToken || !sharedClientRef.current) return;

    try {
      const [sessionMessagesResponse, sessionStatus] = await Promise.all([
        sharedClientRef.current.getMessages(shareToken, { limit: 100 }),
        sharedClientRef.current.getStatus(shareToken)
      ]);

      if (!isValidSessionMessageResponse(sessionMessagesResponse)) {
        console.warn('Invalid session messages response structure during polling:', sessionMessagesResponse);
        return;
      }

      const msgs = sessionMessagesResponse?.messages || [];
      const convertedMessages: Message[] = msgs.map((msg: SessionMessage, index: number) => ({
        id: convertSessionMessageId(msg.id, Date.now() + index),
        role: msg.role === 'assistant' ? 'agent' : (msg.role === 'system' ? 'agent' : msg.role as 'user' | 'agent'),
        content: msg.content,
        timestamp: msg.time || msg.timestamp || new Date().toISOString()
      }));

      // Deep comparison: only update if there are actual differences in content
      setMessages(prevMessages => {
        // If length differs, update
        if (prevMessages.length !== convertedMessages.length) {
          return convertedMessages;
        }

        // Check if any message content has changed
        const hasChanges = convertedMessages.some((newMsg, index) => {
          const prevMsg = prevMessages[index];
          if (!prevMsg) return true;

          // Compare essential fields
          return (
            prevMsg.id !== newMsg.id ||
            prevMsg.role !== newMsg.role ||
            prevMsg.content !== newMsg.content ||
            prevMsg.timestamp !== newMsg.timestamp
          );
        });

        // Only update if there are actual changes
        return hasChanges ? convertedMessages : prevMessages;
      });
      setAgentStatus(sessionStatus as AgentStatus);
    } catch (err) {
      console.error('Failed to poll shared session data:', err);
      if (err instanceof SharedSessionClientError) {
        if (err.code === 'SHARE_EXPIRED') {
          setError('この共有リンクは有効期限が切れています。');
          setIsConnected(false);
        }
      }
    }
  }, [isConnected, shareToken]);

  const pollingControl = useBackgroundAwareInterval(pollMessages, 2000, true);
  const pollingControlRef = useRef(pollingControl);
  pollingControlRef.current = pollingControl;

  useEffect(() => {
    const control = pollingControlRef.current;
    if (isConnected && shareToken) {
      control.start();
    } else {
      control.stop();
    }
    return () => {
      control.stop();
    };
  }, [isConnected, shareToken]);

  // Handle new messages and auto-scroll
  useEffect(() => {
    const currentLength = messages.length;
    const previousLength = prevMessagesLengthRef.current;

    if (currentLength > previousLength) {
      if (shouldAutoScroll) {
        scrollToBottom();
      } else {
        setHasNewMessages(true);
      }
    }

    prevMessagesLengthRef.current = currentLength;
  }, [messages, shouldAutoScroll]);

  const formatTimestamp = useCallback((timestamp: string) => {
    return new Date(timestamp).toLocaleTimeString();
  }, []);

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'stable': return 'text-green-600 dark:text-green-400';
      case 'running': return 'text-yellow-600 dark:text-yellow-400';
      default: return 'text-gray-600 dark:text-gray-400';
    }
  };

  return (
    <div className="flex flex-col h-full bg-white dark:bg-gray-900" style={{ position: 'relative', minHeight: 0 }}>
      {/* Header */}
      <div className="bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-700 px-3 sm:px-6 py-2 sm:py-3 flex-shrink-0">
        <div className="flex items-center justify-between">
          <div className="flex items-center space-x-2 sm:space-x-4">
            <Link
              href="/chats"
              className="flex items-center space-x-1 sm:space-x-2 px-2 sm:px-3 py-1 sm:py-1.5 text-xs sm:text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors"
              title="Go to Conversations"
            >
              <svg className="w-3 h-3 sm:w-4 sm:h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
              </svg>
              <span className="hidden sm:inline">Conversations</span>
            </Link>
            <div className="flex items-center space-x-2">
              <h2 className="text-base sm:text-lg font-semibold text-gray-900 dark:text-white">
                Shared Session
              </h2>
              {/* 共有セッションバッジ */}
              <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200">
                <svg className="w-3 h-3 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                </svg>
                Read-only
              </span>
            </div>
          </div>
          <div className="flex items-center space-x-2 sm:space-x-4">
            {/* Agent Status */}
            {agentStatus && (
              <div className="flex items-center space-x-1 sm:space-x-2">
                <div className={`w-2 h-2 rounded-full ${agentStatus.status === 'stable' ? 'bg-green-500' : 'bg-yellow-500'}`}></div>
                <span className={`text-xs ${getStatusColor(agentStatus.status)} hidden sm:inline`}>
                  {agentStatus.status === 'stable' ? 'Agent Available' : agentStatus.status === 'running' ? 'Agent Running' : agentStatus.status}
                </span>
              </div>
            )}

            {/* Connection Status */}
            <div className="flex items-center space-x-1 sm:space-x-2">
              <div className={`w-2 h-2 rounded-full ${isConnected ? 'bg-green-500' : 'bg-red-500'}`}></div>
              <span className={`text-xs sm:text-sm ${isConnected ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'} hidden sm:inline`}>
                {isConnected ? 'Connected' : 'Disconnected'}
              </span>
            </div>
          </div>
        </div>
        {error && (
          <div className="mt-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-red-700 dark:text-red-400 text-sm">
            {error}
          </div>
        )}
      </div>

      {/* Messages */}
      <div
        ref={messagesContainerRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto bg-white dark:bg-gray-900 mobile-scroll min-h-0 relative"
        style={{
          overscrollBehavior: 'contain',
          WebkitOverflowScrolling: 'touch',
          transform: 'translateZ(0)'
        }}
      >
        {messages.length === 0 && isConnected && (
          <div className="text-center text-gray-500 dark:text-gray-400 py-12">
            <div className="mb-3">
              <svg className="w-12 h-12 mx-auto text-gray-300 dark:text-gray-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
              </svg>
            </div>
            <p className="text-lg font-medium">No messages yet</p>
            <p className="text-sm mt-1">This shared session has no messages</p>
          </div>
        )}

        <div className="divide-y divide-gray-200 dark:divide-gray-700">
          {messages.map((message) => (
            <div key={message.id} className="px-4 sm:px-6 py-4">
              <div className="flex items-start space-x-2 sm:space-x-3">
                <div className="flex-shrink-0">
                  <div className={`w-7 h-7 sm:w-8 sm:h-8 rounded-full flex items-center justify-center ${
                    message.role === 'user'
                      ? 'bg-blue-500 text-white'
                      : 'bg-purple-500 text-white'
                  }`}>
                    {message.role === 'user' ? (
                      <svg className="w-3 h-3 sm:w-4 sm:h-4" fill="currentColor" viewBox="0 0 24 24">
                        <path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/>
                      </svg>
                    ) : (
                      <svg className="w-3 h-3 sm:w-4 sm:h-4" fill="currentColor" viewBox="0 0 24 24">
                        <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z"/>
                      </svg>
                    )}
                  </div>
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center space-x-1 sm:space-x-2 mb-2">
                    <span className="text-sm font-medium text-gray-900 dark:text-white">
                      {message.role === 'user' ? 'User' : 'AgentAPI'}
                    </span>
                    <span className="text-xs text-gray-500 dark:text-gray-400">
                      {formatTimestamp(message.timestamp)}
                    </span>
                  </div>
                  <div className="prose prose-sm max-w-none text-gray-700 dark:text-gray-300">
                    <div className="whitespace-pre-wrap break-words overflow-wrap-anywhere max-w-full">
                      {formatTextWithLinks(message.content)}
                    </div>
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
        <div ref={messagesEndRef} />

        {/* 新着メッセージ通知 */}
        {hasNewMessages && (
          <div className="absolute bottom-4 left-1/2 transform -translate-x-1/2">
            <button
              onClick={() => {
                setShouldAutoScroll(true);
                setHasNewMessages(false);
                scrollToBottom();
              }}
              className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-full shadow-lg flex items-center space-x-2 transition-colors"
            >
              <span className="text-sm">新しいメッセージ</span>
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 14l-7 7m0 0l-7-7m7 7V3" />
              </svg>
            </button>
          </div>
        )}

        {/* スクロールボタン */}
        {!shouldAutoScroll && !hasNewMessages && (
          <div className="absolute bottom-4 right-4">
            <button
              onClick={() => {
                setShouldAutoScroll(true);
                scrollToBottom();
              }}
              className="bg-gray-600 hover:bg-gray-700 text-white p-2 rounded-full shadow-lg transition-colors"
              title="最新メッセージにスクロール"
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 14l-7 7m0 0l-7-7m7 7V3" />
              </svg>
            </button>
          </div>
        )}
      </div>

      {/* Read-only Notice Footer */}
      <div className="bg-gray-100 dark:bg-gray-800 border-t border-gray-200 dark:border-gray-700 px-4 sm:px-6 py-3 flex-shrink-0">
        <div className="flex items-center justify-center space-x-2 text-gray-500 dark:text-gray-400">
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
          </svg>
          <span className="text-sm">This is a read-only shared session view</span>
        </div>
      </div>
    </div>
  );
}
