'use client';

import { useState, useEffect, useRef, useCallback, useMemo, type ClipboardEvent, type TouchEvent } from 'react';
import { useSearchParams, useRouter } from 'next/navigation';
import Link from 'next/link';
import { createAgentAPIProxyClientFromStorage, ACPSessionInfo, ACPConfigOption, ACPUserPromptInfo } from '../../lib/agentapi-proxy-client';
import { AgentAPIProxyError } from '../../lib/agentapi-proxy-client';
import { createACPServerClientFromStorage, ACPServerClient } from '../../lib/acp-server-client';
import { getACPServerEnabled } from '../../types/settings';
import { Session, SessionAnnotations, SessionMessage, SessionMessageListResponse, PendingAction } from '../../types/agentapi';
import { useBackgroundAwareInterval, usePageVisibility } from '../hooks/usePageVisibility';
import { messageTemplateManager } from '../../utils/messageTemplateManager';
import { MessageTemplate } from '../../types/messageTemplate';
import { recentMessagesManager } from '../../utils/recentMessagesManager';
import { pushNotificationManager } from '../../utils/pushNotification';
import { getEnterKeyBehavior, getFontSettings, FontSettings, setFontSettings as saveFontSettings, FontFamily } from '../../types/settings';
import ShareSessionButton from './ShareSessionButton';
import MessageItem from './MessageItem';
import ToolExecutionPane from './ToolExecutionPane';
import PlanApprovalModal from './PlanApprovalModal';
import AskUserQuestionModal from './AskUserQuestionModal';
import SessionListSidebar from './SessionListSidebar';
import { mergeRefreshedMessageHistory } from './messageHistory';

const SIDEBAR_VISIBLE_KEY = 'session_list_sidebar_visible';

// Define local types for agent status
interface AgentStatus {
  status: 'stable' | 'running' | 'error';
  last_activity?: string;
  current_task?: string;
  /** Provisioner error message when status is 'error' */
  message?: string;
}

type MessageSSEConnectionStatus = 'connecting' | 'connected';

// Normalize raw proxy statuses to the three values the UI knows.
// The proxy can return 'cancel', 'stopped', etc. after a cancel — treat them as stable.
function normalizeAgentStatus(raw: string): 'stable' | 'running' | 'error' {
  if (raw === 'running') return 'running';
  if (raw === 'error') return 'error';
  return 'stable';
}

function isACPAgentType(agentType?: string | null): boolean {
  return !!agentType && (agentType.includes('acp') || agentType === 'pi-ollama' || agentType === 'cursor');
}

// Type guard function to validate session message response
function isValidSessionMessageResponse(response: unknown): response is SessionMessageListResponse {
  return (
    response !== null &&
    typeof response === 'object' &&
    'messages' in response &&
    Array.isArray((response as SessionMessageListResponse).messages)
  );
}

type RelatedLink = {
  type: 'pr' | 'issue';
  url: string;
};

function getSessionFromList(sessions: Session[], sessionId: string): Session | undefined {
  return sessions.find(session => session.session_id === sessionId);
}

function getRelatedLinks(annotations?: SessionAnnotations): RelatedLink[] {
  return [
    annotations?.pr_url ? { type: 'pr' as const, url: annotations.pr_url } : null,
    annotations?.issue_url ? { type: 'issue' as const, url: annotations.issue_url } : null,
  ].filter((link): link is RelatedLink => Boolean(link));
}

function getGitHubLinkParts(url: string, type: RelatedLink['type']) {
  const urlParts = url.split('/');
  const marker = type === 'pr' ? 'pull' : 'issues';
  const markerIndex = urlParts.findIndex(part => part === marker);
  const domain = urlParts[2] || '';
  const number = markerIndex >= 0 ? urlParts[markerIndex + 1] || '' : '';
  const owner = markerIndex >= 2 ? urlParts[markerIndex - 2] || '' : '';
  const repo = markerIndex >= 1 ? urlParts[markerIndex - 1] || '' : '';
  const repoName = owner && repo ? `${owner}/${repo}` : '';
  return { domain, number, repoName, isGitHubCom: domain === 'github.com' };
}

function formatACPModelValue(value: unknown): string | null {
  if (typeof value === 'string') {
    const trimmed = value.trim();
    return trimmed.length > 0 ? trimmed : null;
  }

  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value);
  }

  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null;
  }

  const record = value as Record<string, unknown>;
  const provider = formatACPModelValue(record.provider);
  const direct =
    formatACPModelValue(record.id) ||
    formatACPModelValue(record.model) ||
    formatACPModelValue(record.modelId) ||
    formatACPModelValue(record.name) ||
    formatACPModelValue(record.displayName) ||
    formatACPModelValue(record.value) ||
    formatACPModelValue(record.currentValue) ||
    formatACPModelValue(record.default);

  if (provider && direct && !direct.includes(provider)) {
    return `${provider}/${direct}`;
  }
  return direct;
}

interface ACPModelOption {
  value: string;
  label: string;
  description?: string;
  group?: string;
}

function getACPConfigOptionId(option: ACPConfigOption | undefined): string | null {
  if (!option) return null;
  return formatACPModelValue(option.id) || formatACPModelValue(option.key);
}

function getACPConfigOptionCurrentValue(option: ACPConfigOption | undefined): string | null {
  if (!option) return null;
  return (
    formatACPModelValue(option.currentValue) ||
    formatACPModelValue(option.value) ||
    formatACPModelValue(option.default)
  );
}

function getACPModelConfigOption(info: ACPSessionInfo | null): ACPConfigOption | null {
  if (!info?.configOptions?.length) return null;

  const byCategory = info.configOptions.find(option => option.category === 'model');
  if (byCategory) return byCategory;

  return info.configOptions.find(option => {
    const haystack = [option.id, option.key, option.name, option.description]
      .filter((value): value is string => typeof value === 'string')
      .join(' ')
      .toLowerCase();
    return /\bmodel\b/.test(haystack) || haystack.includes('modelid');
  }) ?? null;
}

function getACPEffortConfigOption(info: ACPSessionInfo | null): ACPConfigOption | null {
  if (!info?.configOptions?.length) return null;

  const byCategory = info.configOptions.find(option => option.category?.toLowerCase() === 'effort');
  if (byCategory) return byCategory;

  return info.configOptions.find(option => {
    const haystack = [option.id, option.key, option.name, option.description]
      .filter((value): value is string => typeof value === 'string')
      .join(' ')
      .toLowerCase();
    return /\b(effort|reasoning effort)\b/.test(haystack);
  }) ?? null;
}

function flattenACPModelOptions(options: unknown): ACPModelOption[] {
  if (!Array.isArray(options)) return [];

  const flattened: ACPModelOption[] = [];
  for (const item of options) {
    if (typeof item === 'string') {
      flattened.push({ value: item, label: item });
      continue;
    }

    if (!item || typeof item !== 'object') continue;
    const record = item as Record<string, unknown>;

    if (Array.isArray(record.options)) {
      const groupName = formatACPModelValue(record.name) || formatACPModelValue(record.group) || undefined;
      flattened.push(...flattenACPModelOptions(record.options).map(option => ({
        ...option,
        group: option.group ?? groupName,
      })));
      continue;
    }

    const value = formatACPModelValue(record.value);
    if (!value) continue;
    flattened.push({
      value,
      label: formatACPModelValue(record.name) || value,
      description: formatACPModelValue(record.description) || undefined,
    });
  }

  return flattened;
}

export function getACPModelDisplay(info: ACPSessionInfo | null): string | null {
  if (!info) return null;

  const direct =
    formatACPModelValue(info.model) ||
    formatACPModelValue(info.modelId) ||
    formatACPModelValue(info.currentModel) ||
    formatACPModelValue(info.selectedModel) ||
    formatACPModelValue(info.modelInfo);

  if (direct) return direct;

  const meta = info._meta;
  const metaModel =
    meta &&
    (
      formatACPModelValue(meta.model) ||
      formatACPModelValue(meta.modelId) ||
      formatACPModelValue(meta.currentModel) ||
      formatACPModelValue(meta.modelInfo)
    );
  if (metaModel) return metaModel;

  const modelOption = getACPModelConfigOption(info);

  if (!modelOption) return null;
  const currentValue = getACPConfigOptionCurrentValue(modelOption);
  const selected = flattenACPModelOptions(modelOption.options).find(option => option.value === currentValue);
  return selected ? `${selected.label} (${selected.value})` : currentValue;
}

interface SessionTokenUsage {
  total: number;
  input?: number;
  output?: number;
  source: 'reported' | 'estimated';
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value);
}

function getNumberField(record: Record<string, unknown>, keys: string[]): number | undefined {
  for (const key of keys) {
    const value = record[key];
    if (typeof value === 'number' && Number.isFinite(value)) return value;
    if (typeof value === 'string' && value.trim() !== '') {
      const parsed = Number(value);
      if (Number.isFinite(parsed)) return parsed;
    }
  }
  return undefined;
}

function extractReportedTokenUsage(value: unknown): SessionTokenUsage | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;

  const record = value as Record<string, unknown>;
  const candidates = [
    record.usage,
    record.tokenUsage,
    record.tokens,
    record.modelUsage,
    record._meta,
    value,
  ];

  for (const candidate of candidates) {
    if (!candidate || typeof candidate !== 'object' || Array.isArray(candidate)) continue;
    const usage = candidate as Record<string, unknown>;
    const input = getNumberField(usage, ['inputTokens', 'input_tokens', 'promptTokens', 'prompt_tokens']);
    const output = getNumberField(usage, ['outputTokens', 'output_tokens', 'completionTokens', 'completion_tokens']);
    const total = getNumberField(usage, ['totalTokens', 'total_tokens', 'tokens', 'total']);
    const computedTotal = total ?? ((input ?? 0) + (output ?? 0));

    if (computedTotal > 0) {
      return {
        total: computedTotal,
        input,
        output,
        source: 'reported',
      };
    }
  }

  return null;
}

function estimateMessageTokens(messages: SessionMessage[]): SessionTokenUsage {
  const totalChars = messages.reduce((sum, message) => {
    return sum + (message.content?.length ?? 0) + (message.thought?.length ?? 0);
  }, 0);

  return {
    total: Math.ceil(totalChars / 4),
    source: 'estimated',
  };
}

function aggregateReportedMessageUsage(messages: SessionMessage[]): SessionTokenUsage | null {
  const total = messages.reduce(
    (sum, message) => {
      const usage = extractReportedTokenUsage(message.metadata);
      if (!usage) return sum;
      return {
        total: sum.total + usage.total,
        input: sum.input + (usage.input ?? 0),
        output: sum.output + (usage.output ?? 0),
        count: sum.count + 1,
      };
    },
    { total: 0, input: 0, output: 0, count: 0 }
  );

  if (total.count === 0 || total.total <= 0) return null;
  return {
    total: total.total,
    input: total.input > 0 ? total.input : undefined,
    output: total.output > 0 ? total.output : undefined,
    source: 'reported',
  };
}

function getSessionTokenUsage(info: ACPSessionInfo | null, messages: SessionMessage[]): SessionTokenUsage {
  return extractReportedTokenUsage(info) ?? aggregateReportedMessageUsage(messages) ?? estimateMessageTokens(messages);
}

function getLatestACPUserPromptIndex(prompts: ACPUserPromptInfo[]): number | null {
  return prompts.length > 0 ? prompts[prompts.length - 1].index : null;
}

function withACPHistoryMessageIds(messages: SessionMessage[], userPromptIndex: number): SessionMessage[] {
  return messages.map((message, index) => ({
    ...message,
    id: -((userPromptIndex + 1) * 100000 + index + 1),
  }));
}

interface AgentAPIChatProps {
  sessionId?: string;
}

export default function AgentAPIChat({ sessionId: propSessionId }: AgentAPIChatProps = {}) {
  const searchParams = useSearchParams();
  const router = useRouter();
  // Use sessionId from props if provided, otherwise fall back to query param
  const sessionId = propSessionId || searchParams.get('session');

  // Create global API client
  const [agentAPI] = useState<ReturnType<typeof createAgentAPIProxyClientFromStorage>>(() => {
    return createAgentAPIProxyClientFromStorage();
  });
  const agentAPIRef = useRef<ReturnType<typeof createAgentAPIProxyClientFromStorage>>(agentAPI);

  // Keep ref in sync
  useEffect(() => {
    agentAPIRef.current = agentAPI;
  }, [agentAPI]);

  // Global ACP server mode (POST /acp + GET /acp/sse)
  const [acpServerEnabled] = useState(() => typeof window !== 'undefined' ? getACPServerEnabled() : false);
  const acpServerClientRef = useRef<ACPServerClient | null>(null);
  useEffect(() => {
    if (acpServerEnabled) {
      const client = createACPServerClientFromStorage();
      acpServerClientRef.current = client;
      client.initialize().catch(err => console.warn('[ACP] initialize handshake failed:', err));
    }
  }, [acpServerEnabled]);

  // Initialize push notifications
  useEffect(() => {
    pushNotificationManager.initialize().catch(console.error);
  }, []);
  
  // Initialize chat when agentAPI is ready - optimized for faster loading
  useEffect(() => {
    if (agentAPI && sessionId) {
      // Reset initial load flag when session changes
      setIsInitialLoadComplete(false);
      setIsStarting(true);
      setIsConnected(false);
      setAgentStatus(null);
      setMessages([]);
      setACPInfo(null);
      setACPUserPrompts([]);
      setLoadedACPStartPromptIndex(null);
      setMessageSSEConnectionStatus('connecting');
      // Clear any pending retry timer
      if (retryTimerRef.current) {
        clearTimeout(retryTimerRef.current);
        retryTimerRef.current = null;
      }

      const initializeChat = async () => {
        console.log(`[ACP] initializeChat called (sessionId=${sessionId}, existingACPInfo=${JSON.stringify(acpInfo)}, hasExistingEventSource=${!!acpEventSourceRef.current})`);
        try {
          setError(null);
          setIsConnected(true); // Set connected immediately for better UX

          if (sessionId) {
            void agentAPIRef.current.search({ limit: 100 })
              .then(response => {
                setSessionAnnotations(getSessionFromList(response.sessions, sessionId)?.annotations);
                lastSessionAnnotationLoadTimeRef.current = Date.now();
              })
              .catch(err => console.warn('Failed to load session annotations:', err));

            // ── ACP session detection ───────────────────────────────────────
            // Try GET /{sessionId}/session first. If it succeeds, this is an
            // ACP-transport session – use SSE + JSON-RPC instead of polling.
            if (agentAPIRef.current) {
              // Shared callback object used for both normal ACP and early-ACP-fallback paths.
              const acpCallbacks = {
                  onMessage: (msg: SessionMessage) => {
                    setMessages(prev => [...prev, msg]);
                  },
                  onChunk: (msgId: number, text: string) => {
                    setMessages(prev => prev.map(m =>
                      m.id === msgId ? { ...m, content: m.content + text } : m
                    ));
                  },
                  onImageChunk: (msgId: number, image: { mimeType: string; data: string }) => {
                    setMessages(prev => prev.map(m =>
                      m.id === msgId ? { ...m, images: [...(m.images ?? []), image] } : m
                    ));
                  },
                  onThoughtChunk: (msgId: number, thought: string) => {
                    setMessages(prev => prev.map(m =>
                      m.id === msgId ? { ...m, thought: (m.thought ?? '') + thought } : m
                    ));
                  },
                  onToolUpdate: (toolCallId: string, status: string) => {
                    setMessages(prev => prev.map(m =>
                      m.toolUseId === toolCallId
                        ? { ...m, content: JSON.stringify({ ...JSON.parse(m.content || '{}'), _status: status }) }
                        : m
                    ));
                  },
                  onToolInputUpdate: (toolCallId: string, input: unknown, title?: string, locations?: Array<{ path: string; line?: number }>) => {
                    setMessages(prev => prev.map(m => {
                      if (m.toolUseId !== toolCallId) return m;
                      try {
                        const toolObj = JSON.parse(m.content || '{}');
                        return { ...m, content: JSON.stringify({
                          ...toolObj,
                          input,
                          ...(title != null ? { title } : {}),
                          ...(locations != null ? { locations } : {}),
                        })};
                      } catch { return m; }
                    }));
                  },
                  onModeUpdate: (modeId: string) => {
                    console.log('[ACP] current_mode_update:', modeId);
                  },
                  onConfigOptionsUpdate: (configOptions: ACPConfigOption[]) => {
                    applyACPConfigOptions(configOptions);
                  },
                  onStatus: (status: { status: 'stable' | 'running' | 'error'; agent_type?: string }) => {
                    setAgentStatus(status);
                    if (status.status === 'stable') {
                      void agentAPIRef.current?.getACPMessageHistory(sessionId, '').then(result => {
                        setACPUserPrompts(result.userPrompts);
                        setLoadedACPStartPromptIndex(prev => prev ?? result.userPromptIndex ?? getLatestACPUserPromptIndex(result.userPrompts));
                      });
                    }
                  },
                  onPermission: (action: PendingAction, rpcId: number) => {
                    setACPPendingPermission({ action, rpcId });
                    setPendingAction(action);
                    setShowQuestionModal(true);
                  },
                  onConnectionOpen: () => {
                    setMessageSSEConnectionStatus('connected');
                  },
                  onConnectionConnecting: () => {
                    setMessageSSEConnectionStatus('connecting');
                  },
                  onConnectionClosed: () => {
                    setMessageSSEConnectionStatus('connecting');
                  },
                  onError: (err: Event | Error) => {
                    console.error('[ACP] SSE error callback (from AgentAPIChat):', err);
                  },
              };

              const info = await agentAPIRef.current.getACPSessionInfo(sessionId);
              if (info) {
                console.log(`[ACP] initializeChat: ACP session detected (acpSessionId=${info.sessionId}), previous acpInfo=${JSON.stringify(acpInfo)}`);
                setACPInfo(info);
                setAgentType('acp');

                // Always fetch history from the per-session bridge.
                // We always use the per-session SSE (which does NOT replay history),
                // so there is no risk of duplicates from SSE replay.
                // History and status are independent remote calls. External session
                // managers may add noticeable tunnel latency to each request, so do
                // not make chat bootstrap pay for them serially.
                const [historyResult, currentStatus] = await Promise.all([
                  agentAPIRef.current!.getACPMessageHistory(sessionId, info.sessionId),
                  agentAPIRef.current!.getSessionStatus(sessionId).catch(() => null),
                ]);
                setMessages(historyResult.messages);
                setACPUserPrompts(historyResult.userPrompts);
                setLoadedACPStartPromptIndex(historyResult.userPromptIndex ?? getLatestACPUserPromptIndex(historyResult.userPrompts));
                console.log(`[ACP] initializeChat: restored ${historyResult.messages.length} messages from bridge history`);

                setHasMoreMessages(false);
                setIsInitialLoadComplete(true);
                setIsStarting(false);

                // Fetch current status immediately so the UI reflects running/stable
                // on reconnect (e.g. user navigated away then came back while the
                // agent was still processing the provisioner's initial prompt).
                if (currentStatus) {
                  setAgentStatus({ ...currentStatus, status: normalizeAgentStatus(currentStatus.status) });
                  // Update agentType so markdown renders for ACP sessions.
                  // getACPSessionInfo hardcodes 'acp', but agent_type from status is authoritative.
                  if (currentStatus.agent_type) {
                    setAgentType(currentStatus.agent_type);
                  }
                }

                // Subscribe to ACP SSE stream.
                if (acpEventSourceRef.current) {
                  console.log(`[ACP] initializeChat: closing existing SSE connection`);
                  acpEventSourceRef.current.close();
                }
                setMessageSSEConnectionStatus('connecting');

                // Subscribe to SSE. In acpServerEnabled mode, route through the proxy's
                // GET /acp endpoint with Acp-Session-Id header. Otherwise connect directly
                // to the per-session bridge SSE.
                console.log(`[ACP] initializeChat: subscribing to SSE for sessionId=${sessionId} (acpServerEnabled=${acpServerEnabled})`);
                if (acpServerEnabled && acpServerClientRef.current) {
                  acpEventSourceRef.current = acpServerClientRef.current.subscribeToEvents(
                    sessionId,
                    acpCallbacks
                  );
                } else {
                  acpEventSourceRef.current = agentAPIRef.current.subscribeToACPSessionEvents(
                    sessionId,
                    info.sessionId,
                    {
                      ...acpCallbacks,
                      onCommandsUpdate: (commands) => {
                        console.log('[ACP] available_commands_update:', commands);
                      },
                    }
                  );
                }
                return;
              } else if (acpServerEnabled && acpServerClientRef.current) {
                // Bridge pod not ready yet, but ACP server mode is enabled.
                // Enter ACP mode immediately to suppress polling; the SSE client
                // will reconnect automatically once the bridge comes up.
                console.log(`[ACP] initializeChat: bridge not ready, entering early ACP mode (sessionId=${sessionId})`);
                setACPInfo({ sessionId: '', status: 'running' });
                setAgentType('acp');
                setIsInitialLoadComplete(true);
                setIsStarting(false);

                if (!acpEventSourceRef.current) {
                  setMessageSSEConnectionStatus('connecting');
                  acpEventSourceRef.current = acpServerClientRef.current.subscribeToEvents(sessionId, acpCallbacks);
                }
                return;
              } else {
                // For claude-acp / codex-acp per-session bridge (not ACP server mode):
                // getACPSessionInfo returned null, but the session might be an ACP
                // session whose bridge is still starting. Check agent_type via session status.
                try {
                  const statusResult = await agentAPIRef.current!.getSessionStatus(sessionId);
                  setAgentStatus({ ...statusResult, status: normalizeAgentStatus(statusResult.status) });
                  if (isACPAgentType(statusResult.agent_type)) {
                    if (statusResult.status === 'error') {
                      const detail = statusResult.message || '不明なエラー';
                      setError(`セッションの起動に失敗しました: ${detail}`);
                      setIsStarting(false);
                      return;
                    }
                    // Bridge not ready yet — wait and retry
                    console.log(`[ACP] initializeChat: ${statusResult.agent_type} bridge not ready, retrying (sessionId=${sessionId})`);
                    setIsStarting(true);
                    retryTimerRef.current = setTimeout(initializeChat, 2000);
                    return;
                  }
                } catch {
                  // Status check failed (e.g. session provisioning race condition) — fall through
                }
              }

            }

            // Session-based connection: load latest messages
            try {
              if (!agentAPIRef.current) return;
              // Fetch only the latest 50 messages initially (direction: tail gets most recent)
              const sessionMessagesResponse = await agentAPIRef.current.getSessionMessages(sessionId, {
                limit: 50,
                direction: 'tail'
              });

              // Validate and safely handle session messages response
              if (!isValidSessionMessageResponse(sessionMessagesResponse)) {
                console.warn('Invalid session messages response structure:', sessionMessagesResponse);
              }

              // Store latest messages
              const fetchedMessages = sessionMessagesResponse?.messages || [];
              setMessages(fetchedMessages);

              // Check if there are more messages using hasMore field or total count
              const hasMore = sessionMessagesResponse?.hasMore ??
                             (sessionMessagesResponse?.total ? sessionMessagesResponse.total > fetchedMessages.length : false);
              setHasMoreMessages(hasMore);
              setError(null);
              setIsStarting(false);

              // Mark initial load as complete
              setIsInitialLoadComplete(true);

              // Scroll to bottom after messages are loaded
              setTimeout(() => {
                messagesEndRef.current?.scrollIntoView({ behavior: 'auto' });
              }, 100);

              return;
            } catch (err) {
              console.error('Failed to load session messages:', err);
              setIsConnected(false); // Only set disconnected on actual error
              if (err instanceof AgentAPIProxyError && (err.status === 404 || err.status === 502 || err.status === 503)) {
                // 404: セッションがまだセッションマネージャーに登録されていない可能性がある（プロビジョニング中の race condition）
                // 502/503: サービス起動中の可能性がある
                // いずれの場合もプロビジョナーのステータスを確認してから再試行
                if (agentAPIRef.current) {
                  try {
                    const provStatus = await agentAPIRef.current.getSessionStatus(sessionId);
                    if (provStatus.status === 'error') {
                      // プロビジョナーが恒久的に失敗した場合は再試行を停止してエラーを表示
                      const detail = provStatus.message || '不明なエラー';
                      setError(`セッションの起動に失敗しました: ${detail}`);
                      setIsStarting(false);
                      return;
                    }
                  } catch {
                    // ステータス確認に失敗した場合は通常の再試行を続行
                  }
                }
                setIsStarting(true);
                retryTimerRef.current = setTimeout(initializeChat, 2000);
                return;
              }
              if (err instanceof AgentAPIProxyError) {
                setError(`セッションメッセージの読み込みに失敗しました: ${err.message} (セッション: ${sessionId})`);
              } else {
                setError(`セッション ${sessionId} への接続に失敗しました`);
              }
              return;
            }
          } else {
            setError('No session ID provided. Please provide a session ID to connect.');
            setIsConnected(false);
            return;
          }
        } catch (err) {
          console.error('Failed to initialize chat:', err);
          setIsConnected(false);
          if (err instanceof AgentAPIProxyError && (err.status === 404 || err.status === 502 || err.status === 503)) {
            // サービス起動中またはセッション登録中の可能性があるため、処理中として扱い再試行
            setIsStarting(true);
            retryTimerRef.current = setTimeout(initializeChat, 2000);
            return;
          }
          if (err instanceof AgentAPIProxyError) {
            setError(`接続に失敗しました: ${err.message}`);
          } else {
            setError('AgentAPI Proxyへの接続に失敗しました');
          }
        }
      };

      // Use setTimeout to defer heavy initialization
      const timeoutId = setTimeout(initializeChat, 0);
      return () => {
        clearTimeout(timeoutId);
        if (retryTimerRef.current) {
          clearTimeout(retryTimerRef.current);
          retryTimerRef.current = null;
        }
      };
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId, agentAPI, acpServerEnabled]);

  const [messages, setMessages] = useState<SessionMessage[]>([]);
  const [hasMoreMessages, setHasMoreMessages] = useState(true);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [isInitialLoadComplete, setIsInitialLoadComplete] = useState(false);
  const [inputValue, setInputValue] = useState('');
  const messageInputRef = useRef<HTMLTextAreaElement>(null);
  const imageInputRef = useRef<HTMLInputElement>(null);
  const [attachedImages, setAttachedImages] = useState<Array<{
    name: string;
    mimeType: string;
    data: string;
  }>>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [fontSettings, setFontSettings] = useState<FontSettings>(() => getFontSettings());
  const [agentStatus, setAgentStatus] = useState<AgentStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const [messageSSEConnectionStatus, setMessageSSEConnectionStatus] = useState<MessageSSEConnectionStatus>('connecting');
  const [isStarting, setIsStarting] = useState(false);
  const retryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [shouldAutoScroll, setShouldAutoScroll] = useState(true);
  const [hasNewMessages, setHasNewMessages] = useState(false);
  const [showControlPanel, setShowControlPanel] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [sidebarVisible, setSidebarVisible] = useState(false); // initialized via effect

  // Restore sidebar visibility from localStorage after mount
  useEffect(() => {
    const saved = localStorage.getItem(SIDEBAR_VISIBLE_KEY);
    setSidebarVisible(saved !== 'false');
  }, []);

  const toggleSidebar = useCallback(() => {
    setSidebarVisible(prev => {
      const next = !prev;
      localStorage.setItem(SIDEBAR_VISIBLE_KEY, String(next));
      return next;
    });
  }, []);
  const [templates, setTemplates] = useState<MessageTemplate[]>([]);
  const [showTemplates, setShowTemplates] = useState(false);
  const [recentMessages, setRecentMessages] = useState<string[]>([]);
  const [showTemplateModal, setShowTemplateModal] = useState(false);
  const [showPRLinks, setShowPRLinks] = useState(false);
  const [sessionAnnotations, setSessionAnnotations] = useState<SessionAnnotations | undefined>();
  const [showFontSettings, setShowFontSettings] = useState(false);
  const [showSessionInfo, setShowSessionInfo] = useState(false);
  const [showPlanModal, setShowPlanModal] = useState(false);
  const [planContent, setPlanContent] = useState<string>('');
  const [agentType, setAgentType] = useState<string | null>(null);
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null);
  const [showQuestionModal, setShowQuestionModal] = useState(false);

  // ACP session state
  const [acpInfo, setACPInfo] = useState<ACPSessionInfo | null>(null);
  const acpModelConfigOption = useMemo(() => getACPModelConfigOption(acpInfo), [acpInfo]);
  const acpModelConfigId = useMemo(() => getACPConfigOptionId(acpModelConfigOption ?? undefined), [acpModelConfigOption]);
  const acpModelOptions = useMemo(() => flattenACPModelOptions(acpModelConfigOption?.options), [acpModelConfigOption]);
  const acpCurrentModelValue = useMemo(() => getACPConfigOptionCurrentValue(acpModelConfigOption ?? undefined), [acpModelConfigOption]);
  const acpModelDisplay = useMemo(() => getACPModelDisplay(acpInfo), [acpInfo]);
  const [selectedACPModel, setSelectedACPModel] = useState('');
  const [isSettingACPModel, setIsSettingACPModel] = useState(false);
  const [acpModelMessage, setACPModelMessage] = useState<string | null>(null);
  const acpEffortConfigOption = useMemo(() => getACPEffortConfigOption(acpInfo), [acpInfo]);
  const acpEffortConfigId = useMemo(() => getACPConfigOptionId(acpEffortConfigOption ?? undefined), [acpEffortConfigOption]);
  const acpEffortOptions = useMemo(() => flattenACPModelOptions(acpEffortConfigOption?.options), [acpEffortConfigOption]);
  const acpCurrentEffortValue = useMemo(() => getACPConfigOptionCurrentValue(acpEffortConfigOption ?? undefined), [acpEffortConfigOption]);
  const [selectedACPEffort, setSelectedACPEffort] = useState('');
  const [isSettingACPEffort, setIsSettingACPEffort] = useState(false);
  const [acpEffortMessage, setACPEffortMessage] = useState<string | null>(null);
  const isACPSession = !!acpInfo || isACPAgentType(agentType) || acpServerEnabled;
  const isMessageSSEConnected = !isACPSession || messageSSEConnectionStatus === 'connected';
  const isConnectionStatusConnected = isConnected && isMessageSSEConnected;
  const connectionStatusLabel = isConnectionStatusConnected ? 'Connected' : 'Connecting';
  const connectionStatusTextClass = isConnectionStatusConnected
    ? 'text-green-600 dark:text-green-400'
    : 'text-yellow-600 dark:text-yellow-400';
  const connectionStatusDotClass = isConnectionStatusConnected ? 'bg-green-500' : 'bg-yellow-500';
  const tokenUsage = useMemo(() => getSessionTokenUsage(acpInfo, messages), [acpInfo, messages]);
  const relatedLinks = useMemo(() => getRelatedLinks(sessionAnnotations), [sessionAnnotations]);
  const [acpPendingPermission, setACPPendingPermission] = useState<{ action: PendingAction; rpcId: number } | null>(null);
  const [acpUserPrompts, setACPUserPrompts] = useState<ACPUserPromptInfo[]>([]);
  const [isLoadingACPPromptHistory, setIsLoadingACPPromptHistory] = useState(false);
  const [loadedACPStartPromptIndex, setLoadedACPStartPromptIndex] = useState<number | null>(null);
  const [acpPullDistance, setACPPullDistance] = useState(0);
  const acpNextPromptId = useRef(1);
  const acpEventSourceRef = useRef<{ close: () => void } | null>(null);
  const loadedACPStartPromptIndexRef = useRef<number | null>(null);
  const acpPullStartYRef = useRef<number | null>(null);
  const acpPullTrackingRef = useRef(false);
  const acpPullDistanceRef = useRef(0);

  const messagesEndRef = useRef<HTMLDivElement>(null);
  const messagesContainerRef = useRef<HTMLDivElement>(null);
  const messagesTopRef = useRef<HTMLDivElement>(null);
  const prevMessagesLengthRef = useRef(0);
  const prevAgentStatusRef = useRef<AgentStatus | null>(null);
  const lastLoadTimeRef = useRef<number>(0);
  const lastSessionAnnotationLoadTimeRef = useRef<number>(0);
  const acpPullThreshold = 72;

  useEffect(() => {
    loadedACPStartPromptIndexRef.current = loadedACPStartPromptIndex;
  }, [loadedACPStartPromptIndex]);

  useEffect(() => {
    if (isACPSession) {
      setShowControlPanel(false);
    } else {
      setShowSessionInfo(false);
    }
  }, [isACPSession]);

  useEffect(() => {
    setSelectedACPModel(acpCurrentModelValue ?? '');
    setACPModelMessage(null);
  }, [acpCurrentModelValue, acpModelConfigId]);

  useEffect(() => {
    setSelectedACPEffort(acpCurrentEffortValue ?? '');
    setACPEffortMessage(null);
  }, [acpCurrentEffortValue, acpEffortConfigId]);

  const applyACPConfigOptions = useCallback((configOptions: ACPConfigOption[]) => {
    setACPInfo(prev => {
      if (!prev) return prev;
      return {
        ...prev,
        configOptions,
      };
    });
  }, []);

  const loadPreviousACPTurn = useCallback(async () => {
    if (!sessionId || !agentAPIRef.current) return;
    const acpSessionId = acpInfo?.sessionId;
    if (!acpSessionId) return;
    if (isLoadingACPPromptHistory) return;

    const currentStartIndex = loadedACPStartPromptIndexRef.current ?? getLatestACPUserPromptIndex(acpUserPrompts);
    if (currentStartIndex === null || currentStartIndex <= 0) return;
    const previousPromptIndex = currentStartIndex - 1;
    const container = messagesContainerRef.current;
    const previousScrollHeight = container?.scrollHeight ?? 0;
    const previousScrollTop = container?.scrollTop ?? 0;
    const restoreScrollPosition = () => {
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          if (!container) return;
          const scrollDiff = container.scrollHeight - previousScrollHeight;
          container.scrollTop = previousScrollTop + scrollDiff;
        });
      });
    };

    setIsLoadingACPPromptHistory(true);
    try {
      const result = await agentAPIRef.current.getACPMessageHistory(
        sessionId,
        acpSessionId,
        previousPromptIndex
      );
      const olderMessages = withACPHistoryMessageIds(result.messages, previousPromptIndex);
      setMessages(prev => [...olderMessages, ...prev]);
      setACPUserPrompts(result.userPrompts);
      setLoadedACPStartPromptIndex(previousPromptIndex);
      setHasMoreMessages(false);
      restoreScrollPosition();
    } catch (err) {
      console.warn('[ACP] Failed to load previous prompt history:', err);
    } finally {
      setIsLoadingACPPromptHistory(false);
    }
  }, [sessionId, acpInfo?.sessionId, acpUserPrompts, isLoadingACPPromptHistory]);

  const canLoadPreviousACPTurn = useCallback(() => {
    if (!isACPSession || acpUserPrompts.length <= 1 || isLoadingACPPromptHistory) return false;
    const currentStartIndex = loadedACPStartPromptIndexRef.current ?? getLatestACPUserPromptIndex(acpUserPrompts);
    return currentStartIndex !== null && currentStartIndex > 0;
  }, [isACPSession, acpUserPrompts, isLoadingACPPromptHistory]);

  const handleMessagesTouchStart = useCallback((event: TouchEvent<HTMLDivElement>) => {
    const container = messagesContainerRef.current;
    if (!container || container.scrollTop > 0 || !canLoadPreviousACPTurn()) {
      acpPullTrackingRef.current = false;
      acpPullStartYRef.current = null;
      return;
    }
    acpPullTrackingRef.current = true;
    acpPullStartYRef.current = event.touches[0]?.clientY ?? null;
  }, [canLoadPreviousACPTurn]);

  const handleMessagesTouchMove = useCallback((event: TouchEvent<HTMLDivElement>) => {
    if (!acpPullTrackingRef.current || acpPullStartYRef.current === null) return;
    const container = messagesContainerRef.current;
    if (!container || container.scrollTop > 0) return;

    const currentY = event.touches[0]?.clientY;
    if (currentY === undefined) return;
    const delta = currentY - acpPullStartYRef.current;
    if (delta <= 0) {
      acpPullDistanceRef.current = 0;
      setACPPullDistance(0);
      return;
    }

    const distance = Math.min(delta * 0.55, 96);
    acpPullDistanceRef.current = distance;
    setACPPullDistance(distance);
    if (distance > 8) {
      event.preventDefault();
    }
  }, []);

  const finishMessagesPull = useCallback(() => {
    const shouldLoad = acpPullTrackingRef.current && acpPullDistanceRef.current >= acpPullThreshold;
    acpPullTrackingRef.current = false;
    acpPullStartYRef.current = null;
    acpPullDistanceRef.current = 0;
    setACPPullDistance(0);
    if (shouldLoad) {
      void loadPreviousACPTurn();
    }
  }, [loadPreviousACPTurn]);

  const handleSetACPModel = useCallback(async () => {
    if (!sessionId || !acpModelConfigId || !selectedACPModel) return;
    if (selectedACPModel === acpCurrentModelValue) return;

    setIsSettingACPModel(true);
    setACPModelMessage(null);

    try {
      let result: { configOptions?: ACPConfigOption[] } | undefined;
      if (acpServerEnabled && acpServerClientRef.current) {
        result = await acpServerClientRef.current.setSessionConfigOption(
          sessionId,
          acpModelConfigId,
          selectedACPModel
        );
      } else {
        if (!acpInfo || !agentAPIRef.current) return;
        result = await agentAPIRef.current.setACPSessionConfigOption(
          sessionId,
          acpInfo.sessionId,
          acpModelConfigId,
          selectedACPModel
        );
      }

      if (result?.configOptions) {
        applyACPConfigOptions(result.configOptions);
      }
      setACPModelMessage('Model updated');
    } catch (err) {
      console.error('Failed to update ACP model:', err);
      setACPModelMessage(err instanceof Error ? err.message : 'Failed to update model');
    } finally {
      setIsSettingACPModel(false);
    }
  }, [
    sessionId,
    acpInfo,
    acpServerEnabled,
    acpModelConfigId,
    selectedACPModel,
    acpCurrentModelValue,
    applyACPConfigOptions,
  ]);

  const handleSetACPEffort = useCallback(async () => {
    if (!sessionId || !acpEffortConfigId || !selectedACPEffort) return;
    if (selectedACPEffort === acpCurrentEffortValue) return;

    setIsSettingACPEffort(true);
    setACPEffortMessage(null);
    try {
      let result: { configOptions?: ACPConfigOption[] } | undefined;
      if (acpServerEnabled && acpServerClientRef.current) {
        result = await acpServerClientRef.current.setSessionConfigOption(
          sessionId,
          acpEffortConfigId,
          selectedACPEffort
        );
      } else {
        if (!acpInfo || !agentAPIRef.current) return;
        result = await agentAPIRef.current.setACPSessionConfigOption(
          sessionId,
          acpInfo.sessionId,
          acpEffortConfigId,
          selectedACPEffort
        );
      }
      if (result?.configOptions) applyACPConfigOptions(result.configOptions);
      setACPEffortMessage('Effort updated');
    } catch (err) {
      console.error('Failed to update ACP effort:', err);
      setACPEffortMessage(err instanceof Error ? err.message : 'Failed to update effort');
    } finally {
      setIsSettingACPEffort(false);
    }
  }, [
    sessionId,
    acpInfo,
    acpServerEnabled,
    acpEffortConfigId,
    selectedACPEffort,
    acpCurrentEffortValue,
    applyACPConfigOptions,
  ]);

  const handleImageSelection = useCallback(async (files: FileList | File[] | null) => {
    if (!files) return;
    const selected = Array.from(files).filter(file => file.type.startsWith('image/'));
    const encoded = await Promise.all(selected.map(file => new Promise<{
      name: string;
      mimeType: string;
      data: string;
    }>((resolve, reject) => {
      const reader = new FileReader();
      reader.onerror = () => reject(reader.error ?? new Error(`Failed to read ${file.name}`));
      reader.onload = () => {
        const result = String(reader.result ?? '');
        resolve({
          name: file.name,
          mimeType: file.type,
          data: result.split(',', 2)[1] ?? '',
        });
      };
      reader.readAsDataURL(file);
    })));
    setAttachedImages(prev => [...prev, ...encoded].slice(0, 4));
    if (imageInputRef.current) imageInputRef.current.value = '';
  }, []);

  const handleImagePaste = useCallback((event: ClipboardEvent<HTMLTextAreaElement>) => {
    const images = Array.from(event.clipboardData.items)
      .filter(item => item.kind === 'file' && item.type.startsWith('image/'))
      .map(item => item.getAsFile())
      .filter((file): file is File => file !== null);
    if (images.length === 0) return;

    event.preventDefault();
    void handleImageSelection(images);
  }, [handleImageSelection]);

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  };

  const checkIfAtBottom = () => {
    if (!messagesContainerRef.current) return false;
    const container = messagesContainerRef.current;
    const threshold = 100; // 100px以内なら「下部にいる」と判断
    return container.scrollHeight - container.scrollTop - container.clientHeight <= threshold;
  };

  const handleScroll = useCallback(() => {
    if (!messagesContainerRef.current) return;

    const isAtBottom = checkIfAtBottom();
    setShouldAutoScroll(isAtBottom);

    // 下部にスクロールしたら新着メッセージ通知をクリア
    if (isAtBottom) {
      setHasNewMessages(false);
    }
  }, []);

  const loadMoreMessages = useCallback(async () => {
    if (!hasMoreMessages || isLoadingMore || !sessionId || !agentAPIRef.current) return;

    // Prevent loading if less than 2 seconds have passed since last load
    const now = Date.now();
    const timeSinceLastLoad = now - lastLoadTimeRef.current;
    const cooldownPeriod = 2000; // 2 seconds

    if (timeSinceLastLoad < cooldownPeriod) {
      console.log(`[loadMoreMessages] Cooldown active. Wait ${Math.ceil((cooldownPeriod - timeSinceLastLoad) / 1000)}s before loading more.`);
      return;
    }

    const container = messagesContainerRef.current;
    if (!container) return;

    // Update last load time
    lastLoadTimeRef.current = now;

    // Save current scroll position
    const previousScrollHeight = container.scrollHeight;
    const previousScrollTop = container.scrollTop;

    setIsLoadingMore(true);
    try {
      // Get the oldest message ID (smallest ID)
      const oldestMessage = messages[0];
      if (!oldestMessage || typeof oldestMessage.id !== 'number') {
        console.error('Invalid oldest message:', oldestMessage);
        setIsLoadingMore(false);
        setHasMoreMessages(false);
        return;
      }

      console.log('[loadMoreMessages] Fetching messages before ID:', oldestMessage.id);

      // Fetch messages before the oldest message ID using cursor-based pagination
      const response = await agentAPIRef.current.getSessionMessages(sessionId, {
        before: oldestMessage.id,
        limit: 50
      });

      if (!isValidSessionMessageResponse(response)) {
        console.warn('Invalid session messages response:', response);
        setIsLoadingMore(false);
        return;
      }

      const olderMessages = response?.messages || [];
      console.log(`[loadMoreMessages] Fetched ${olderMessages.length} older messages`);

      if (olderMessages.length > 0) {
        // Prepend older messages to the beginning
        setMessages(prev => [...olderMessages, ...prev]);

        // Use hasMore field from response to determine if there are more messages
        const hasMore = response?.hasMore ?? (olderMessages.length >= 50);
        setHasMoreMessages(hasMore);

        // Restore scroll position after DOM update
        setTimeout(() => {
          if (container) {
            const newScrollHeight = container.scrollHeight;
            const scrollDiff = newScrollHeight - previousScrollHeight;
            container.scrollTop = previousScrollTop + scrollDiff;
            console.log('[loadMoreMessages] Restored scroll position, scrollDiff:', scrollDiff);
          }
        }, 0);
      } else {
        console.log('[loadMoreMessages] No more older messages');
        setHasMoreMessages(false);
      }
    } catch (err) {
      console.error('Failed to load more messages:', err);
    } finally {
      setIsLoadingMore(false);
    }
  }, [hasMoreMessages, isLoadingMore, sessionId, messages]);

  // ESCキーでモーダルを閉じる
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        if (showQuestionModal) {
          setShowQuestionModal(false)
        } else if (showTemplateModal) {
          setShowTemplateModal(false)
        } else if (showPRLinks) {
          setShowPRLinks(false)
        } else if (showFontSettings) {
          setShowFontSettings(false)
        }
      }
    }

    if (showQuestionModal || showTemplateModal || showPRLinks || showFontSettings) {
      document.addEventListener('keydown', handleKeyDown)
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [showQuestionModal, showTemplateModal, showPRLinks, showFontSettings])

  // Listen for font settings changes
  useEffect(() => {
    const handleFontSettingsChange = (event: Event) => {
      const customEvent = event as CustomEvent<FontSettings>
      if (customEvent.detail) {
        setFontSettings(customEvent.detail)
      }
    }

    window.addEventListener('fontSettingsChanged', handleFontSettingsChange)

    return () => {
      window.removeEventListener('fontSettingsChanged', handleFontSettingsChange)
    }
  }, []);

  // IntersectionObserver for infinite scroll (load more on scroll up)
  // Only activate after initial load is complete to prevent premature loading
  // Disabled while loading to prevent multiple simultaneous requests
  useEffect(() => {
    const topElement = messagesTopRef.current;

    // Don't observe if:
    // - Element doesn't exist
    // - No more messages to load
    // - Initial load not complete
    // - Currently loading (prevents rapid multiple requests)
    if (!topElement || !hasMoreMessages || !isInitialLoadComplete || isLoadingMore) {
      console.log('[IntersectionObserver] Skip setup:', {
        hasElement: !!topElement,
        hasMoreMessages,
        isInitialLoadComplete,
        isLoadingMore
      });
      return;
    }

    console.log('[IntersectionObserver] Setting up observer');

    const observer = new IntersectionObserver(
      (entries) => {
        console.log('[IntersectionObserver] Callback triggered:', {
          isIntersecting: entries[0].isIntersecting,
          hasMoreMessages
        });
        // Only trigger if the top element is visible
        if (entries[0].isIntersecting) {
          console.log('[IntersectionObserver] Loading more messages...');
          loadMoreMessages();
        }
      },
      { threshold: 0.1, root: messagesContainerRef.current }
    );

    observer.observe(topElement);

    return () => {
      console.log('[IntersectionObserver] Disconnecting observer');
      observer.disconnect();
    };
  }, [hasMoreMessages, isLoadingMore, loadMoreMessages, isInitialLoadComplete]);


  const loadTemplates = useCallback(async () => {
    try {
      const allTemplates = await messageTemplateManager.getTemplates();
      setTemplates(allTemplates);
    } catch (error) {
      console.error('Failed to load templates:', error);
      setTemplates([]);
    }
  }, []);

  const loadRecentMessages = useCallback(async () => {
    try {
      const messages = await recentMessagesManager.getRecentMessages();
      setRecentMessages(messages.map(msg => msg.content));
    } catch (error) {
      console.error('Failed to load recent messages:', error);
    }
  }, []);

  useEffect(() => {
    // Defer template and recent message loading to improve initial render speed
    const timeoutId = setTimeout(() => {
      loadTemplates();
      loadRecentMessages();
    }, 100);
    return () => clearTimeout(timeoutId);
  }, [loadRecentMessages, loadTemplates]);

  // Session-based polling for messages (optimized interval)
  const pollMessages = useCallback(async () => {
    if (!isConnected || !sessionId || !agentAPIRef.current) return;

    try {
      // Get the latest message ID for polling new messages only (claude agent only)
      const latestMessage = messages[messages.length - 1];
      const latestMessageId = latestMessage && typeof latestMessage.id === 'number' ? latestMessage.id : undefined;

      // Poll messages, status, pending actions, and session annotations.
      // For claude agents: fetch only new messages using 'after' parameter
      // For other agents: always fetch all messages to ensure timeline is updated
      const shouldRefreshAnnotations = Date.now() - lastSessionAnnotationLoadTimeRef.current > 5000;
      const [sessionMessagesResponse, sessionStatus, pendingActions, sessionListResponse] = await Promise.all([
        agentAPIRef.current.getSessionMessages(sessionId,
          agentType === 'claude' && latestMessageId !== undefined ? {
            after: latestMessageId, // Get messages with ID > latestMessageId (newer messages)
            limit: 50
          } : {
            limit: 50,
            direction: 'tail' // Get latest 50 messages
          }
        ),
        agentAPIRef.current.getSessionStatus(sessionId),
        agentAPIRef.current.getPendingActions(sessionId),
        shouldRefreshAnnotations ? agentAPIRef.current.search({ limit: 100 }) : Promise.resolve(null)
      ]);

      // Validate and safely handle session messages response
      if (!isValidSessionMessageResponse(sessionMessagesResponse)) {
        console.warn('Invalid session messages response structure during polling:', sessionMessagesResponse);
        return;
      }

      // Use SessionMessage directly for display
      const newMessages = sessionMessagesResponse?.messages || [];

      // Handle message updates based on agent type
      if (newMessages.length > 0) {
        if (agentType === 'claude') {
          // For claude agents: filter and append only truly new messages
          setMessages(prevMessages => {
            const existingIds = new Set(prevMessages.map(m => m.id));
            const trulyNewMessages = newMessages.filter(m => !existingIds.has(m.id));

            if (trulyNewMessages.length === 0) return prevMessages;

            return [...prevMessages, ...trulyNewMessages];
          });
        } else {
          // For other agents: always update with all messages to reflect timeline changes
          setMessages(prevMessages => mergeRefreshedMessageHistory(prevMessages, newMessages));
        }
      }

      // Handle pending actions
      const questionAction = pendingActions.find(a => a.type === 'answer_question');
      if (questionAction && !pendingAction) {
        setPendingAction(questionAction);
        setShowQuestionModal(true);
      } else if (!questionAction && pendingAction) {
        // Question was answered/cleared
        setPendingAction(null);
        setShowQuestionModal(false);
      }

      const normalizedSessionStatus = { ...sessionStatus, status: normalizeAgentStatus(sessionStatus.status) };
      setAgentStatus(normalizedSessionStatus);
      if (sessionListResponse) {
        setSessionAnnotations(getSessionFromList(sessionListResponse.sessions, sessionId)?.annotations);
        lastSessionAnnotationLoadTimeRef.current = Date.now();
      }
      // When the provisioner has permanently failed, surface the error once.
      if (
        sessionStatus.status === 'error' &&
        prevAgentStatusRef.current?.status !== 'error'
      ) {
        const detail = sessionStatus.message || '不明なエラー';
        setError(`セッションの起動に失敗しました: ${detail}`);
      }
      prevAgentStatusRef.current = normalizedSessionStatus;
    } catch (err) {
      console.error('Failed to poll session data:', err);
      if (err instanceof AgentAPIProxyError) {
        if (err.status === 502 || err.status === 503) {
          // サービス一時停止の可能性があるため、エラーを表示せず継続
          console.warn('Received 502/503 during polling, continuing...');
        } else {
          setError(`Failed to update messages: ${err.message}`);
        }
      }
    }
  }, [isConnected, sessionId, pendingAction, messages, agentType]); // agentAPIを依存配列から除去

  const handleAnswerSubmit = useCallback(async (answers: Record<string, string | string[]>) => {
    if (!sessionId || !agentAPIRef.current || !pendingAction) return;

    try {
      if (acpInfo && acpPendingPermission) {
        // ── ACP: reply to session/request_permission ──
        // answers format: { "0": "selectedOptionLabel" }
        const selectedLabel = Object.values(answers)[0] as string | undefined;
        const options = acpPendingPermission.action.content?.questions?.[0]?.options ?? [];
        const matched = options.find(o => o.label === selectedLabel);
        const optionId = matched ? matched.label : (selectedLabel ?? '');
        if (acpServerEnabled && acpServerClientRef.current) {
          await acpServerClientRef.current.sendPermissionResponse(sessionId, acpPendingPermission.rpcId, optionId);
        } else {
          await agentAPIRef.current.replyToACPPermission(sessionId, acpPendingPermission.rpcId, optionId);
        }
        setACPPendingPermission(null);
      } else {
        // ── Regular session ───────────────────────────────────────────────
        await agentAPIRef.current.sendAction(sessionId, {
          type: 'answer_question',
          answers
        });
      }

      // Clear pending action and close modal
      setPendingAction(null);
      setShowQuestionModal(false);
    } catch (err) {
      console.error('Failed to submit answers:', err);
      if (err instanceof AgentAPIProxyError) {
        setError(`Failed to submit answers: ${err.message}`);
      }
    }
  }, [sessionId, pendingAction, acpInfo, acpPendingPermission, acpServerEnabled]);

  const handleQuestionModalClose = useCallback(() => {
    setShowQuestionModal(false);
  }, []);

  // 1秒インターバルポーリング（接続中かつ非ACPセッションのみ動作）
  const pollingControl = useBackgroundAwareInterval(pollMessages, 1000, false);

  useEffect(() => {
    // Don't poll for ACP sessions
    if (isConnected && sessionId && !acpInfo) {
      pollingControl.start();
    } else {
      pollingControl.stop();
    }
    return () => pollingControl.stop();
  }, [isConnected, sessionId, acpInfo, pollingControl]);

  // ACP EventSource cleanup on unmount / session change.
  useEffect(() => {
    return () => {
      if (acpEventSourceRef.current) {
        acpEventSourceRef.current.close();
        acpEventSourceRef.current = null;
      }
    };
  }, [sessionId]);

  // Page visibility tracking for ACP reconnection.
  const isPageVisible = usePageVisibility();
  const prevIsPageVisibleRef = useRef<boolean>(true);

  // When the page transitions from hidden → visible with an active ACP session,
  // refresh the message history and reconnect the SSE stream if needed.
  useEffect(() => {
    const wasHidden = !prevIsPageVisibleRef.current;
    prevIsPageVisibleRef.current = isPageVisible;

    // Only act on hidden → visible transitions
    if (!isPageVisible || !wasHidden) return;
    if (!acpInfo || !sessionId || !agentAPIRef.current) return;

    const reconnectACP = async () => {
      console.log('[ACP] Page became visible, refreshing messages and checking SSE connection...');

      // 1. Fetch fresh message history from the bridge
      try {
        const latestResult = await agentAPIRef.current!.getACPMessageHistory(sessionId, acpInfo.sessionId);
        const latestPromptIndex = latestResult.userPromptIndex ?? getLatestACPUserPromptIndex(latestResult.userPrompts);
        const currentStartIndex = loadedACPStartPromptIndexRef.current;

        if (currentStartIndex !== null && latestPromptIndex !== null && currentStartIndex < latestPromptIndex) {
          const historicalTurns = await Promise.all(
            Array.from({ length: latestPromptIndex - currentStartIndex }, (_, offset) => {
              const promptIndex = currentStartIndex + offset;
              return agentAPIRef.current!.getACPMessageHistory(sessionId, acpInfo.sessionId, promptIndex)
                .then(result => withACPHistoryMessageIds(result.messages, promptIndex));
            })
          );
          const refreshedMessages = [...historicalTurns.flat(), ...latestResult.messages];
          setMessages(currentMessages => mergeRefreshedMessageHistory(currentMessages, refreshedMessages));
          setLoadedACPStartPromptIndex(currentStartIndex);
        } else {
          setMessages(currentMessages => mergeRefreshedMessageHistory(currentMessages, latestResult.messages));
          setLoadedACPStartPromptIndex(latestPromptIndex);
        }
        setACPUserPrompts(latestResult.userPrompts);
        console.log(`[ACP] Refreshed messages after visibility change`);
      } catch (err) {
        console.warn('[ACP] Failed to refresh message history on visibility change:', err);
      }

      // 2. Fetch current agent status
      try {
        const currentStatus = await agentAPIRef.current!.getSessionStatus(sessionId);
        setAgentStatus(currentStatus);
      } catch {
        // Non-fatal — status will be updated via SSE events
      }

      // 3. Reconnect SSE
      console.log('[ACP] Reconnecting SSE after visibility change...');
      if (acpEventSourceRef.current) {
        acpEventSourceRef.current.close();
      }
      setMessageSSEConnectionStatus('connecting');

      const reconnectCallbacks = {
          onMessage: (msg: SessionMessage) => {
            setMessages(prev => [...prev, msg]);
          },
          onChunk: (msgId: number, text: string) => {
            setMessages(prev => prev.map(m =>
              m.id === msgId ? { ...m, content: m.content + text } : m
            ));
          },
          onImageChunk: (msgId: number, image: { mimeType: string; data: string }) => {
            setMessages(prev => prev.map(m =>
              m.id === msgId ? { ...m, images: [...(m.images ?? []), image] } : m
            ));
          },
          onThoughtChunk: (msgId: number, thought: string) => {
            setMessages(prev => prev.map(m =>
              m.id === msgId ? { ...m, thought: (m.thought ?? '') + thought } : m
            ));
          },
          onToolUpdate: (toolCallId: string, status: string) => {
            setMessages(prev => prev.map(m =>
              m.toolUseId === toolCallId
                ? { ...m, content: JSON.stringify({ ...JSON.parse(m.content || '{}'), _status: status }) }
                : m
            ));
          },
          onToolInputUpdate: (toolCallId: string, input: unknown, title?: string, locations?: Array<{ path: string; line?: number }>) => {
            setMessages(prev => prev.map(m => {
              if (m.toolUseId !== toolCallId) return m;
              try {
                const toolObj = JSON.parse(m.content || '{}');
                return { ...m, content: JSON.stringify({
                  ...toolObj,
                  input,
                  ...(title != null ? { title } : {}),
                  ...(locations != null ? { locations } : {}),
                })};
              } catch { return m; }
            }));
          },
          onModeUpdate: (modeId: string) => {
            console.log('[ACP] current_mode_update:', modeId);
          },
          onConfigOptionsUpdate: (configOptions: ACPConfigOption[]) => {
            applyACPConfigOptions(configOptions);
          },
          onStatus: (status: { status: 'stable' | 'running' | 'error'; agent_type?: string }) => {
            setAgentStatus(status);
            if (status.status === 'stable') {
              void agentAPIRef.current?.getACPMessageHistory(sessionId, '').then(result => {
                setACPUserPrompts(result.userPrompts);
                setLoadedACPStartPromptIndex(prev => prev ?? result.userPromptIndex ?? getLatestACPUserPromptIndex(result.userPrompts));
              });
            }
          },
          onPermission: (action: PendingAction, rpcId: number) => {
            setACPPendingPermission({ action, rpcId });
            setPendingAction(action);
            setShowQuestionModal(true);
          },
          onConnectionOpen: () => {
            setMessageSSEConnectionStatus('connected');
          },
          onConnectionConnecting: () => {
            setMessageSSEConnectionStatus('connecting');
          },
          onConnectionClosed: () => {
            setMessageSSEConnectionStatus('connecting');
          },
          onError: (err: Event | Error) => {
            console.error('[ACP] SSE error callback (from visibility reconnect):', err);
          },
      };

      if (acpServerEnabled && acpServerClientRef.current) {
        acpEventSourceRef.current = acpServerClientRef.current.subscribeToEvents(
          sessionId,
          reconnectCallbacks
        );
      } else {
        acpEventSourceRef.current = agentAPIRef.current!.subscribeToACPSessionEvents(
          sessionId,
          acpInfo.sessionId,
          {
            ...reconnectCallbacks,
            onCommandsUpdate: (commands) => {
              console.log('[ACP] available_commands_update:', commands);
            },
          }
        );
      }
      console.log('[ACP] SSE reconnected after visibility change');
    };

    reconnectACP();
  }, [isPageVisible, acpInfo, sessionId, acpServerEnabled, applyACPConfigOptions]);

  // Seed agent type and status independently from message-history bootstrap.
  // This lets the loading screen reflect running/error even while history is slow.
  useEffect(() => {
    const fetchAgentStatus = async () => {
      if (!sessionId || !agentAPIRef.current) {
        return;
      }

      try {
        const status = await agentAPIRef.current.getSessionStatus(sessionId);
        setAgentType(status.agent_type || null);
        setAgentStatus({ ...status, status: normalizeAgentStatus(status.status) });
      } catch (error) {
        console.error('Failed to get agent type:', error);
        // If we can't get the agent type, assume it's not claude
        setAgentType(null);
      }
    };

    fetchAgentStatus();
  }, [sessionId]);

  // Handle new messages and auto-scroll
  useEffect(() => {
    const currentLength = messages.length;
    const previousLength = prevMessagesLengthRef.current;
    
    // 新しいメッセージが追加された場合
    if (currentLength > previousLength) {
      if (shouldAutoScroll) {
        // ユーザーが下部にいる場合のみ自動スクロール
        scrollToBottom();
      } else {
        // ユーザーが上部を見ている場合は新着通知を表示
        setHasNewMessages(true);
      }
    }
    
    prevMessagesLengthRef.current = currentLength;
  }, [messages, shouldAutoScroll]);

  const sendMessage = useCallback(async (messageType: 'user' | 'raw' = 'user', content?: string) => {
    const messageContent = content || inputValue.trim();
    
    if (!messageContent && attachedImages.length === 0 && messageType === 'user') return;
    if (isLoading || !isConnected) return;
    
    if (agentStatus?.status === 'running' && messageType === 'user') {
      setError('Agent is currently running. Please wait for it to become stable.');
      return;
    }

    setIsLoading(true);
    setError(null);

    try {
      if (sessionId) {
        // Send message via session
        if (!agentAPIRef.current) {
          setError('AgentAPI client not available');
          return;
        }

        if (acpInfo) {
          // ── ACP session: send prompt ──────────────────────────────────────
          const promptId = acpNextPromptId.current++;
          // Do NOT add the user message locally here.
          // The bridge broadcasts a synthetic user_message_chunk via SSE,
          // which will arrive via onMessage and be added to the message list.
          setAgentStatus({ status: 'running' });
          const prompt = [
            ...(messageContent ? [{ type: 'text' as const, text: messageContent }] : []),
            ...attachedImages.map(image => ({
              type: 'image' as const,
              mimeType: image.mimeType,
              data: image.data,
            })),
          ];
          if (acpServerEnabled && acpServerClientRef.current) {
            // ACP server mode: global POST /acp endpoint with proxy session ID
            await acpServerClientRef.current.sendPrompt(sessionId!, prompt, promptId);
          } else {
            // Per-session bridge mode: POST /{proxySessionId}/rpc directly to the bridge.
            // acpInfo.sessionId is the bridge-internal ACP session ID.
            await agentAPIRef.current.sendACPPrompt(sessionId!, acpInfo.sessionId, prompt, promptId);
          }
        } else if (acpServerEnabled && acpServerClientRef.current) {
          // ── Global ACP server only (no per-session bridge) ────────────
          const promptId = acpNextPromptId.current++;
          setAgentStatus({ status: 'running' });
          const prompt = [
            ...(messageContent ? [{ type: 'text' as const, text: messageContent }] : []),
            ...attachedImages.map(image => ({
              type: 'image' as const,
              mimeType: image.mimeType,
              data: image.data,
            })),
          ];
          await acpServerClientRef.current.sendPrompt(sessionId, prompt, promptId);
        } else {
          // ── Regular session ───────────────────────────────────────────
          const sessionMessage = await agentAPIRef.current.sendSessionMessage(sessionId, {
            content: messageContent,
            type: messageType
          });

          // For user messages, add to messages
          if (messageType === 'user') {
            setMessages(prev => [...prev, sessionMessage]);
          }
        }
      } else {
        setError('No session ID available. Cannot send message.');
        return;
      }

      if (messageType === 'user') {
        // 最近のメッセージに保存
        await recentMessagesManager.saveMessage(messageContent);
        await loadRecentMessages();

        setInputValue('');
        setAttachedImages([]);
        // メッセージ送信時は必ずスクロール
        setShouldAutoScroll(true);
        setTimeout(() => scrollToBottom(), 100);
      }
    } catch (err) {
      console.error('Failed to send message:', err);
      // Reset status to stable if the send failed so the UI doesn't get stuck at 'running'
      setAgentStatus(prev => prev?.status === 'running' ? { status: 'stable' } : prev);
      if (err instanceof AgentAPIProxyError) {
        // Handle timeout errors specially
        if (err.code === 'TIMEOUT_ERROR') {
          setError(`${err.message}`);
        } else {
          setError(`メッセージ送信に失敗しました: ${err.message} (セッション: ${sessionId})`);
        }
      } else {
        setError(`メッセージ送信に失敗しました: ${err instanceof Error ? err.message : '不明なエラー'}`);
      }
    } finally {
      setIsLoading(false);
    }
  }, [inputValue, attachedImages, isLoading, isConnected, sessionId, agentStatus, loadRecentMessages, acpInfo, acpServerEnabled]);

  const handleShowPlanModal = useCallback((content: string) => {
    setPlanContent(content);
    setShowPlanModal(true);
  }, []);

  // Memoize plan modal callbacks for each message to prevent unnecessary re-renders
  const planModalCallbacks = useMemo(() => {
    const callbacks = new Map<string, () => void>();
    messages.forEach(message => {
      if (message.type === 'plan') {
        callbacks.set(message.id.toString(), () => handleShowPlanModal(message.content));
      }
    });
    return callbacks;
  }, [messages, handleShowPlanModal]);

  const handleApprovePlan = useCallback(async (approved: boolean) => {
    if (!sessionId || !agentAPIRef.current) {
      setError('Session not available for plan approval');
      return;
    }

    setIsLoading(true);
    setError(null);

    try {
      if (acpInfo && acpPendingPermission) {
        // ACP: approve/reject via per-session bridge (always).
        const options = acpPendingPermission.action.content?.questions?.[0]?.options ?? [];
        const allowOpt = options.find(o => o.description?.includes('allow'));
        const optionId = approved
          ? (allowOpt?.label ?? options[0]?.label ?? 'allow-once')
          : 'plan';
        await agentAPIRef.current.replyToACPPermission(sessionId, acpPendingPermission.rpcId, optionId);
        setACPPendingPermission(null);
      } else {
        await agentAPIRef.current.sendAction(sessionId, {
          type: 'approve_plan',
          approved
        });
      }

      // モーダルを閉じる
      setShowPlanModal(false);

      // スクロールを有効にして下部へ移動
      setShouldAutoScroll(true);
      setTimeout(() => scrollToBottom(), 100);
    } catch (err) {
      console.error('Failed to approve plan:', err);
      if (err instanceof AgentAPIProxyError) {
        setError(`プラン承認に失敗しました: ${err.message}`);
      } else {
        setError(`プラン承認に失敗しました: ${err instanceof Error ? err.message : '不明なエラー'}`);
      }
    } finally {
      setIsLoading(false);
    }
  }, [sessionId, acpInfo, acpPendingPermission]);

  const sendStopSignal = async () => {
    if (!sessionId || !agentAPIRef.current) {
      setError('セッションが利用できません');
      return;
    }

    try {
      if (acpInfo) {
        // ACP セッション: cancel
        if (acpServerEnabled && acpServerClientRef.current) {
          // ACP server mode: global POST /acp endpoint
          await acpServerClientRef.current.cancelSession(sessionId);
          console.log('Stop signal sent via ACP session/cancel (global ACP server)');
        } else {
          // Per-session bridge mode: POST /{proxySessionId}/rpc directly
          await agentAPIRef.current.cancelACPSession(sessionId, acpInfo.sessionId);
          console.log('Stop signal sent via ACP session/cancel (per-session bridge)');
        }
        setAgentStatus({ status: 'stable' });
      } else if (acpServerEnabled && acpServerClientRef.current) {
        // グローバル ACP サーバーモード (per-session bridge なし): ACP cancel を使用
        await acpServerClientRef.current.cancelSession(sessionId);
        setAgentStatus({ status: 'stable' });
        console.log('Stop signal sent via ACP session/cancel (global ACP server)');
      } else if (agentType === 'claude' || agentType === 'codex') {
        // agentapi ベースのエージェント（claude, codex）: /action エンドポイントを使用
        await agentAPIRef.current.sendAction(sessionId, { type: 'stop_agent' });
        console.log('Stop signal sent via /action endpoint (agent type:', agentType, ')');
      } else {
        // デフォルト・素のシェルセッション: Ctrl+C を送信
        await agentAPIRef.current.sendSessionMessage(sessionId, {
          content: '\x03', // Ctrl+C
          type: 'raw'
        });
        console.log('Stop signal sent via Ctrl+C (raw message)');
      }
    } catch (err) {
      console.error('Failed to send stop signal:', err);
      setError('停止シグナルの送信に失敗しました');
    }
  };

  const sendArrowUp = () => {
    // Send up arrow key (raw message)
    sendMessage('raw', '\u001b[A');
  };

  const sendArrowDown = () => {
    // Send down arrow key (raw message)
    sendMessage('raw', '\u001b[B');
  };

  const sendEnterKey = () => {
    // Send enter key (raw message)
    sendMessage('raw', '\r');
  };


  const deleteSession = useCallback(async () => {
    if (!sessionId) return;
    
    const confirmed = window.confirm('このセッションを削除しますか？この操作は元に戻せません。');
    if (!confirmed) return;

    setIsDeleting(true);
    setError(null);

    try {
      if (!agentAPIRef.current) {
        setError('AgentAPI client not available');
        return;
      }
      await agentAPIRef.current.delete(sessionId);
      // セッション削除後、conversation画面にリダイレクト
      router.push('/chats');
    } catch (err) {
      console.error('Failed to delete session:', err);
      if (err instanceof AgentAPIProxyError) {
        setError(`セッションの削除に失敗しました: ${err.message}`);
      } else {
        setError('セッションの削除に失敗しました');
      }
    } finally {
      setIsDeleting(false);
    }
  }, [sessionId, router]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      const enterKeyBehavior = getEnterKeyBehavior();
      // 'send' モード: Enter で送信、Cmd/Ctrl+Enter で改行
      // 'newline' モード: Enter で改行、Cmd/Ctrl+Enter で送信
      const isModifierKeyPressed = e.metaKey || e.ctrlKey;
      const shouldSend = enterKeyBehavior === 'send' ? !isModifierKeyPressed : isModifierKeyPressed;

      if (shouldSend) {
        e.preventDefault();
        sendMessage();
      }
    }
  };

  const handleFontSizeChange = (newSize: number) => {
    const newSettings = { ...fontSettings, fontSize: newSize };
    saveFontSettings(newSettings);  // Save to localStorage and trigger event
  };

  const handleFontFamilyChange = (newFamily: FontFamily) => {
    const newSettings = { ...fontSettings, fontFamily: newFamily };
    saveFontSettings(newSettings);  // Save to localStorage and trigger event
  };

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
      {/* ── Unified full-width header ── */}
      <div className="bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-700 px-3 sm:px-4 py-1.5 sm:py-2 flex-shrink-0">
        <div className="flex items-center justify-between">
          <div className="flex items-center space-x-1 sm:space-x-2">
            {/* Sidebar toggle */}
            <button
              onClick={toggleSidebar}
              className="hidden md:flex items-center justify-center p-1.5 text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors"
              title={sidebarVisible ? 'セッション一覧を隠す' : 'セッション一覧を表示'}
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
              </svg>
            </button>
            <Link
              href="/chats"
              className="flex items-center space-x-1 sm:space-x-2 px-2 py-1 text-xs sm:text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors"
              title="Go to Conversations"
            >
              <svg className="w-3 h-3 sm:w-4 sm:h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
              </svg>
              <span className="hidden sm:inline">Conversations</span>
            </Link>
            <div>
              <h2 className="text-sm sm:text-base font-semibold text-gray-900 dark:text-white">
                <span className="inline">Chat</span>
                {sessionId && (
                  <span className="ml-2 text-xs font-normal text-gray-500 dark:text-gray-400 inline">
                    #{sessionId.substring(0, 6)}
                  </span>
                )}
              </h2>
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


            {/* Stop Button */}
            {agentStatus?.status === 'running' && (
              <button
                onClick={sendStopSignal}
                disabled={!isConnected || isLoading}
                className="px-2 sm:px-3 py-1 bg-red-600 hover:bg-red-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white text-xs sm:text-sm rounded-md transition-colors disabled:cursor-not-allowed flex items-center space-x-1"
                title="Force Stop (ESC)"
              >
                <svg className="w-3 h-3 sm:w-4 sm:h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 10h6v4H9z" />
                </svg>
                <span className="hidden sm:inline">Stop</span>
              </button>
            )}
            
            {/* Connection Status */}
            <div className="flex items-center space-x-1 sm:space-x-2">
              <div className={`w-2 h-2 rounded-full ${connectionStatusDotClass}`}></div>
              <span className={`text-xs sm:text-sm ${connectionStatusTextClass} hidden sm:inline`}>
                {connectionStatusLabel}
              </span>
            </div>

            {/* Delete Session Button */}
            {sessionId && (
              <button
                onClick={deleteSession}
                disabled={isDeleting}
                className="p-2 text-red-500 hover:text-red-700 dark:text-red-400 dark:hover:text-red-200 hover:bg-red-50 dark:hover:bg-red-900/20 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                title={isDeleting ? 'セッションを削除中...' : 'セッションを削除'}
              >
                {isDeleting ? (
                  <svg className="animate-spin w-5 h-5" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                  </svg>
                ) : (
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                  </svg>
                )}
              </button>
            )}

            {/* Related Links Button - 必要なときだけ表示 */}
            {relatedLinks.length > 0 && (
              <button
                onClick={() => setShowPRLinks(!showPRLinks)}
                className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors relative"
                title={`関連リンク (${relatedLinks.length}件)`}
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.5 6H15a3 3 0 010 6h-1.5m-3 0H9a3 3 0 010-6h1.5m-1 6h5" />
                </svg>
                <span className="absolute -top-1 -right-1 bg-gray-500 text-white text-xs rounded-full h-5 w-5 flex items-center justify-center">
                  {relatedLinks.length}
                </span>
              </button>
            )}


            {/* Share Session Button */}
            {sessionId && agentAPI && (
              <ShareSessionButton sessionId={sessionId} agentAPI={agentAPI} />
            )}

            {/* Settings Navigation Button */}
            <button
              onClick={() => router.push('/settings')}
              className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors"
              title="Settings"
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
              </svg>
            </button>
          </div>
        </div>
        {error && (
          <div className="mt-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-red-700 dark:text-red-400 text-sm">
            {error}
          </div>
        )}
        
      </div>

      {/* ── Body: sidebar + chat content ── */}
      <div className="flex-1 flex overflow-hidden min-h-0">

        {/* Session list sidebar (desktop only — hidden on mobile) */}
        {sessionId && (
          <div className="hidden md:flex">
            <SessionListSidebar
              currentSessionId={sessionId}
              isVisible={sidebarVisible}
            />
          </div>
        )}

        {/* Chat content column */}
        <div className="flex-1 flex flex-col min-w-0 min-h-0">

      {/* Messages */}
      <div
        ref={messagesContainerRef}
        onScroll={handleScroll}
        onTouchStart={handleMessagesTouchStart}
        onTouchMove={handleMessagesTouchMove}
        onTouchEnd={finishMessagesPull}
        onTouchCancel={finishMessagesPull}
        className="flex-1 overflow-y-auto bg-white dark:bg-gray-900 mobile-scroll min-h-0 relative"
        style={{ 
          overscrollBehavior: 'contain',
          WebkitOverflowScrolling: 'touch',
          transform: 'translateZ(0)' // GPU acceleration
        }}
      >
        {(acpPullDistance > 0 || isLoadingACPPromptHistory) && (
          <div
            className="pointer-events-none absolute inset-x-0 top-0 z-20 flex h-9 items-center justify-center border-b border-blue-100 bg-blue-50/95 text-xs text-blue-700 shadow-sm dark:border-blue-900/40 dark:bg-blue-950/80 dark:text-blue-200"
            style={{ transform: isLoadingACPPromptHistory ? 'translateY(0)' : `translateY(${Math.max(0, Math.min(acpPullDistance, 36)) - 36}px)` }}
          >
            {isLoadingACPPromptHistory ? (
              <span className="inline-flex items-center gap-2">
                <span className="h-3 w-3 animate-spin rounded-full border-2 border-blue-300 border-t-blue-700 dark:border-blue-700 dark:border-t-blue-200" />
                読み込み中...
              </span>
            ) : acpPullDistance >= acpPullThreshold ? (
              '離して読み込み'
            ) : (
              '前のターン'
            )}
          </div>
        )}

        {canLoadPreviousACPTurn() && (
          <div className="hidden justify-center border-b border-gray-100 bg-white px-4 py-3 md:flex dark:border-gray-800 dark:bg-gray-900">
            <button
              type="button"
              onClick={() => void loadPreviousACPTurn()}
              disabled={isLoadingACPPromptHistory}
              className="inline-flex items-center gap-2 rounded-full border border-blue-200 bg-white px-4 py-2 text-sm font-medium text-blue-700 shadow-sm transition-colors hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-blue-800 dark:bg-gray-900 dark:text-blue-200 dark:hover:bg-blue-950"
            >
              {isLoadingACPPromptHistory ? (
                <span className="h-4 w-4 animate-spin rounded-full border-2 border-blue-300 border-t-blue-700 dark:border-blue-700 dark:border-t-blue-200" />
              ) : (
                <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 19V5m0 0l-7 7m7-7l7 7" />
                </svg>
              )}
              {isLoadingACPPromptHistory ? '読み込み中...' : '前のターンを読み込む'}
            </button>
          </div>
        )}

        {agentStatus?.status === 'error' && (
          <div className="flex flex-col items-center justify-center py-12 px-6">
            <div className="mb-4 text-red-500 dark:text-red-400">
              <svg className="w-14 h-14 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                  d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
              </svg>
            </div>
            <p className="text-lg font-semibold text-red-600 dark:text-red-400">セッションの起動に失敗しました</p>
            {agentStatus.message && (
              <p className="mt-2 text-sm text-gray-600 dark:text-gray-400 max-w-lg text-center break-words">
                {agentStatus.message}
              </p>
            )}
            <p className="mt-4 text-xs text-gray-400 dark:text-gray-500">セッションを削除して再作成してください</p>
          </div>
        )}

        {!isInitialLoadComplete && agentStatus?.status !== 'error' && (
          <div className="text-center text-gray-500 dark:text-gray-400 py-12">
            <div className="mb-3">
              <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600 mx-auto"></div>
            </div>
            <p className="text-lg font-medium">処理中...</p>
            <p className="text-sm mt-1">セッションへの接続を待機しています</p>
          </div>
        )}

        {isInitialLoadComplete && messages.length === 0 && isConnected && (
          <div className="text-center text-gray-500 dark:text-gray-400 py-12">
            <div className="mb-3">
              <svg className="w-12 h-12 mx-auto text-gray-300 dark:text-gray-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
              </svg>
            </div>
            <p className="text-lg font-medium">No conversation yet</p>
            <p className="text-sm mt-1">Start a conversation with the agent below</p>
          </div>
        )}

        {/* Loading indicator for infinite scroll - at the top */}
        {isLoadingMore && (
          <div className="flex justify-center items-center py-4">
            <div className="animate-spin rounded-full h-6 w-6 border-b-2 border-blue-600"></div>
            <span className="ml-2 text-sm text-gray-600 dark:text-gray-400">読み込み中...</span>
          </div>
        )}

        {/* Intersection observer target for infinite scroll */}
        <div ref={messagesTopRef} style={{ height: '1px' }} />

        <div className="divide-y divide-gray-200 dark:divide-gray-700">
          {(() => {
            const renderedMessages: JSX.Element[] = [];
            const processedIds = new Set<number>();

            messages.forEach((message) => {
              // すでに処理済みのメッセージはスキップ
              if (processedIds.has(message.id)) return;

              // parentToolUseId を持つ tool_result は親の tool_use と一緒に表示されるのでスキップ
              if (message.role === 'tool_result' && message.parentToolUseId) {
                processedIds.add(message.id);
                return;
              }

              // tool_use の場合、対応する tool_result を探す
              if (message.role === 'agent' && message.toolUseId) {
                const toolResult = messages.find(m =>
                  m.role === 'tool_result' &&
                  m.parentToolUseId === message.toolUseId
                );

                if (toolResult) {
                  processedIds.add(toolResult.id);
                }

                renderedMessages.push(
                  <MessageItem
                    key={message.id}
                    message={message}
                    toolResult={toolResult}
                    formatTimestamp={formatTimestamp}
                    fontSettings={fontSettings}
                    onShowPlanModal={planModalCallbacks.get(message.id.toString())}
                    isClaudeAgent={agentType === 'claude' || agentType === 'codex' || isACPAgentType(agentType)}
                  />
                );
                processedIds.add(message.id);
                return;
              }

              // 通常のメッセージ (user, assistant, agent without toolUseId, standalone tool_result)
              if (
                message.role === 'user' ||
                message.role === 'assistant' ||
                message.role === 'agent' ||
                message.role === 'tool_result'
              ) {
                renderedMessages.push(
                  <MessageItem
                    key={message.id}
                    message={message}
                    formatTimestamp={formatTimestamp}
                    fontSettings={fontSettings}
                    onShowPlanModal={planModalCallbacks.get(message.id.toString())}
                    isClaudeAgent={agentType === 'claude' || agentType === 'codex' || isACPAgentType(agentType)}
                  />
                );
                processedIds.add(message.id);
              }
            });

            return renderedMessages;
          })()}
        </div>
        <div ref={messagesEndRef} />
        
        {/* 新着メッセージ通知とスクロールボタン */}
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
        
        {/* スクロールボタン（新着メッセージがない場合でも表示） */}
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

      {/* ツール実行確認ペーン */}
      {sessionId && <ToolExecutionPane sessionId={sessionId} agentStatus={agentStatus?.status} />}

      {/* Input */}
      <div className="bg-white dark:bg-gray-900 border-t border-gray-200 dark:border-gray-700 px-4 sm:px-6 py-3 flex-shrink-0">
        {/* Control Panel */}
        {showControlPanel && (
          <div className="mb-4 p-3 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md">
            <h3 className="text-sm font-medium text-gray-900 dark:text-white mb-3">Agent Controls</h3>
            <div className="flex flex-wrap gap-2">
              {/* Arrow Up Button */}
              <button
                onClick={sendArrowUp}
                disabled={!isConnected || isLoading}
                className="px-2 py-1 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white text-xs rounded-md transition-colors disabled:cursor-not-allowed flex items-center space-x-1"
                title="Send Up Arrow Key"
              >
                <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
                </svg>
                <span>↑</span>
              </button>
              
              {/* Arrow Down Button */}
              <button
                onClick={sendArrowDown}
                disabled={!isConnected || isLoading}
                className="px-2 py-1 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white text-xs rounded-md transition-colors disabled:cursor-not-allowed flex items-center space-x-1"
                title="Send Down Arrow Key"
              >
                <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                </svg>
                <span>↓</span>
              </button>
              
              {/* Enter Button */}
              <button
                onClick={sendEnterKey}
                disabled={!isConnected || isLoading}
                className="px-2 py-1 bg-green-600 hover:bg-green-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white text-xs rounded-md transition-colors disabled:cursor-not-allowed flex items-center space-x-1"
                title="Send Enter Key"
              >
                <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <span>Enter</span>
              </button>
              
              {/* ESC Button (existing functionality) */}
              <button
                onClick={sendStopSignal}
                disabled={!isConnected || isLoading}
                className="px-2 py-1 bg-red-600 hover:bg-red-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white text-xs rounded-md transition-colors disabled:cursor-not-allowed flex items-center space-x-1"
                title="Send ESC Key (Force Stop)"
              >
                <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <span>ESC</span>
              </button>

            </div>
          </div>
        )}

        {showSessionInfo && (
          <div className="mb-3 grid gap-2 rounded-md border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 p-3 text-xs">
            <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
              <span className="text-gray-500 dark:text-gray-400">Session ID</span>
              <span className="break-all font-mono text-gray-900 dark:text-gray-100">{sessionId || '-'}</span>
            </div>
            <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
              <span className="text-gray-500 dark:text-gray-400">Agent Type</span>
              <span className="break-all text-gray-900 dark:text-gray-100">{agentType || '-'}</span>
            </div>
            <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
              <span className="text-gray-500 dark:text-gray-400">Status</span>
              <span className="break-all text-gray-900 dark:text-gray-100">{agentStatus?.status || '-'}</span>
            </div>
            <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
              <span className="text-gray-500 dark:text-gray-400">Connection</span>
              <span className="break-all text-gray-900 dark:text-gray-100">{connectionStatusLabel.toLowerCase()}</span>
            </div>
            <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
              <span className="text-gray-500 dark:text-gray-400">Tokens so far</span>
              <span className="break-all text-gray-900 dark:text-gray-100">
                {formatNumber(tokenUsage.total)}
                {tokenUsage.source === 'estimated' ? ' estimated' : ''}
                {tokenUsage.source === 'reported' && tokenUsage.input != null && tokenUsage.output != null
                  ? ` (${formatNumber(tokenUsage.input)} in / ${formatNumber(tokenUsage.output)} out)`
                  : ''}
              </span>
            </div>
            {acpInfo && (
              <>
                <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
                  <span className="text-gray-500 dark:text-gray-400">ACP Session</span>
                  <span className="break-all font-mono text-gray-900 dark:text-gray-100">{acpInfo.sessionId || '-'}</span>
                </div>
                <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
                  <span className="text-gray-500 dark:text-gray-400">Model</span>
                  <span className="break-all text-gray-900 dark:text-gray-100">{acpModelDisplay || '-'}</span>
                </div>
                {acpModelConfigId && acpModelOptions.length > 0 && (
                  <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
                    <label htmlFor="acp-model-select" className="text-gray-500 dark:text-gray-400 pt-2">
                      Switch Model
                    </label>
                    <div className="min-w-0">
                      <div className="flex flex-col gap-2 sm:flex-row sm:items-start">
                        <select
                          id="acp-model-select"
                          value={selectedACPModel}
                          onChange={(event) => setSelectedACPModel(event.target.value)}
                          disabled={isSettingACPModel}
                          className="min-w-0 flex-1 rounded-md border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 px-2 py-1.5 text-xs text-gray-900 dark:text-gray-100 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:cursor-not-allowed disabled:bg-gray-100 dark:disabled:bg-gray-700"
                        >
                          {acpModelOptions.map(option => (
                            <option key={`${option.group ?? 'model'}:${option.value}`} value={option.value}>
                              {option.group ? `${option.group} / ${option.label}` : option.label}
                            </option>
                          ))}
                        </select>
                        <button
                          type="button"
                          onClick={handleSetACPModel}
                          disabled={
                            isSettingACPModel ||
                            !selectedACPModel ||
                            selectedACPModel === acpCurrentModelValue
                          }
                          className="shrink-0 rounded-md bg-blue-600 px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:bg-gray-300 dark:disabled:bg-gray-600"
                        >
                          {isSettingACPModel ? 'Applying...' : 'Apply'}
                        </button>
                      </div>
                      {acpModelOptions.find(option => option.value === selectedACPModel)?.description && (
                        <p className="mt-1 text-[11px] text-gray-500 dark:text-gray-400">
                          {acpModelOptions.find(option => option.value === selectedACPModel)?.description}
                        </p>
                      )}
                      {acpModelMessage && (
                        <p className={`mt-1 text-[11px] ${acpModelMessage === 'Model updated' ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}`}>
                          {acpModelMessage}
                        </p>
                      )}
                    </div>
                  </div>
                )}
                {acpEffortConfigId && acpEffortOptions.length > 0 && (
                  <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
                    <label htmlFor="acp-effort-select" className="pt-2 text-gray-500 dark:text-gray-400">
                      Effort
                    </label>
                    <div className="min-w-0">
                      <div className="flex flex-col gap-2 sm:flex-row sm:items-start">
                        <select
                          id="acp-effort-select"
                          value={selectedACPEffort}
                          onChange={(event) => setSelectedACPEffort(event.target.value)}
                          disabled={isSettingACPEffort || agentStatus?.status === 'running'}
                          className="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-2 py-1.5 text-xs text-gray-900 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:cursor-not-allowed disabled:bg-gray-100 dark:border-gray-600 dark:bg-gray-900 dark:text-gray-100 dark:disabled:bg-gray-700"
                        >
                          {acpEffortOptions.map(option => (
                            <option key={`effort:${option.value}`} value={option.value}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                        <button
                          type="button"
                          onClick={handleSetACPEffort}
                          disabled={
                            isSettingACPEffort ||
                            agentStatus?.status === 'running' ||
                            !selectedACPEffort ||
                            selectedACPEffort === acpCurrentEffortValue
                          }
                          className="shrink-0 rounded-md bg-blue-600 px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:bg-gray-300 dark:disabled:bg-gray-600"
                        >
                          {isSettingACPEffort ? 'Applying...' : 'Apply'}
                        </button>
                      </div>
                      {acpEffortMessage && (
                        <p className={`mt-1 text-[11px] ${acpEffortMessage === 'Effort updated' ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}`}>
                          {acpEffortMessage}
                        </p>
                      )}
                    </div>
                  </div>
                )}
                <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
                  <span className="text-gray-500 dark:text-gray-400">ACP Status</span>
                  <span className="break-all text-gray-900 dark:text-gray-100">{acpInfo.status || '-'}</span>
                </div>
              </>
            )}
          </div>
        )}
        
        <div className="flex items-start space-x-2 sm:space-x-3">
          <div className="flex-shrink-0">
            <div className="w-7 h-7 sm:w-8 sm:h-8 rounded-full bg-blue-500 text-white flex items-center justify-center">
              <svg className="w-3 h-3 sm:w-4 sm:h-4" fill="currentColor" viewBox="0 0 24 24">
                <path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/>
              </svg>
            </div>
          </div>
          <div className="flex-1 relative">
            {attachedImages.length > 0 && (
              <div className="mb-2 flex flex-wrap gap-2">
                {attachedImages.map((image, index) => (
                  <div key={`${image.name}:${index}`} className="relative">
                    {/* eslint-disable-next-line @next/next/no-img-element */}
                    <img
                      src={`data:${image.mimeType};base64,${image.data}`}
                      alt={image.name}
                      className="h-20 w-20 rounded-md border border-gray-300 object-cover dark:border-gray-600"
                    />
                    <button
                      type="button"
                      onClick={() => setAttachedImages(prev => prev.filter((_, itemIndex) => itemIndex !== index))}
                      className="absolute -right-2 -top-2 flex h-6 w-6 items-center justify-center rounded-full bg-gray-800 text-xs text-white shadow"
                      aria-label={`${image.name} を削除`}
                    >
                      ×
                    </button>
                  </div>
                ))}
              </div>
            )}
            <input
              ref={imageInputRef}
              type="file"
              accept="image/*"
              multiple
              className="hidden"
              onChange={(event) => void handleImageSelection(event.target.files)}
              aria-label="画像を添付"
            />
            <textarea
              ref={messageInputRef}
              aria-label="Message"
              value={inputValue}
              onChange={(e) => setInputValue(e.target.value)}
              onKeyDown={handleKeyDown}
              onPaste={handleImagePaste}
              onFocus={() => {
                // テンプレートの自動表示を無効化
              }}
              onBlur={() => {
                setTimeout(() => setShowTemplates(false), 150);
              }}
              placeholder={
                !isConnected || !isInitialLoadComplete
                  ? "Connecting..." 
                  : agentStatus?.status === 'running'
                    ? "Agent is running, please wait..."
                    : "Write a comment..."
              }
              className="w-full resize-none border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 placeholder-gray-500 dark:placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 min-h-[80px]"
              rows={3}
              disabled={!isConnected || !isInitialLoadComplete || isLoading || agentStatus?.status === 'running'}
            />
            {showTemplates && templates.length > 0 && (
              <div className="absolute z-50 w-full bottom-full mb-1 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-md shadow-lg max-h-60 overflow-y-auto">
                <div className="px-3 py-2 text-xs text-gray-500 dark:text-gray-400 border-b border-gray-200 dark:border-gray-700">
                  メッセージテンプレート
                </div>
                {templates.map((template) => (
                  <button
                    key={template.id}
                    type="button"
                    onClick={() => {
                      setInputValue(template.content);
                      setShowTemplates(false);
                    }}
                    className="w-full text-left px-3 py-3 hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-900 dark:text-white border-b border-gray-100 dark:border-gray-700 last:border-b-0"
                  >
                    <div className="font-medium text-sm">{template.name}</div>
                    <div className="line-clamp-2 text-xs text-gray-600 dark:text-gray-400 mt-1">
                      {template.content}
                    </div>
                  </button>
                ))}
              </div>
            )}
            <div className="flex items-center justify-between mt-3">
              <div className="text-xs text-gray-500 dark:text-gray-400 hidden sm:block">
                {(() => {
                  const enterKeyBehavior = getEnterKeyBehavior();
                  const isMac = typeof window !== 'undefined' && navigator.platform.includes('Mac');
                  const modifierKey = isMac ? '⌘' : 'Ctrl';

                  if (enterKeyBehavior === 'send') {
                    return `Press Enter to send, ${modifierKey}+Enter for new line`;
                  } else {
                    return `Press ${modifierKey}+Enter to send, Enter for new line`;
                  }
                })()}
              </div>
              <div className="text-xs text-gray-500 dark:text-gray-400 block sm:hidden">
                {(() => {
                  const enterKeyBehavior = getEnterKeyBehavior();
                  const isMac = typeof window !== 'undefined' && navigator.platform.includes('Mac');
                  const modifierKey = isMac ? '⌘' : 'Ctrl';

                  if (enterKeyBehavior === 'send') {
                    return `Enter: send`;
                  } else {
                    return `${modifierKey}+Enter: send`;
                  }
                })()}
              </div>
              <div className="flex items-center space-x-2">
                {isACPSession && (
                  <button
                    type="button"
                    onClick={() => imageInputRef.current?.click()}
                    disabled={!isConnected || isLoading || agentStatus?.status === 'running' || attachedImages.length >= 4}
                    className="rounded-md bg-sky-600 px-2 py-2 text-xs text-white transition-colors hover:bg-sky-700 disabled:cursor-not-allowed disabled:bg-gray-300 dark:disabled:bg-gray-600"
                    title="画像をアップロード（最大4枚、ペースト対応）"
                    aria-label="画像を添付"
                  >
                    <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15V3m0 0L8 7m4-4 4 4" />
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13v5a2 2 0 002 2h14a2 2 0 002-2v-5" />
                    </svg>
                  </button>
                )}
                {isACPSession ? (
                  <button
                    onClick={() => setShowSessionInfo(prev => !prev)}
                    disabled={!sessionId}
                    className={`px-2 py-2 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white text-xs rounded-md transition-colors disabled:cursor-not-allowed flex items-center ${
                      showSessionInfo ? 'bg-gray-700 hover:bg-gray-800' : 'bg-gray-600 hover:bg-gray-700'
                    }`}
                    title="セッション情報"
                    aria-label="セッション情報"
                  >
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                    </svg>
                  </button>
                ) : (
                  <button
                    onClick={() => setShowControlPanel(!showControlPanel)}
                    disabled={!isConnected}
                    className="px-2 py-2 bg-gray-600 hover:bg-gray-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white text-xs rounded-md transition-colors disabled:cursor-not-allowed flex items-center"
                    title="Toggle Control Panel"
                  >
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 100 4m0-4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 100 4m0-4v2m0-6V4" />
                    </svg>
                  </button>
                )}

                {/* Font Settings Button */}
                <button
                  onClick={() => setShowFontSettings(!showFontSettings)}
                  className="px-2 py-2 bg-purple-600 hover:bg-purple-700 text-white text-xs rounded-md transition-colors flex items-center relative"
                  title="フォント設定"
                >
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 5h12M9 3v2m1.048 9.5A18.022 18.022 0 016.412 9m6.088 9h7M11 21l5-10 5 10M12.751 5C11.783 10.77 8.07 15.61 3 18.129" />
                  </svg>
                </button>

                {/* Template Button */}
                <button
                  onClick={() => setShowTemplateModal(true)}
                  disabled={!isConnected || isLoading}
                  className="px-2 py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white text-xs rounded-md transition-colors disabled:cursor-not-allowed flex items-center"
                  title="テンプレートから選択"
                >
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                  </svg>
                </button>
                
                <button
                  onClick={() => sendMessage()}
                  disabled={!isConnected || isLoading || (!inputValue.trim() && attachedImages.length === 0) || agentStatus?.status === 'running'}
                  aria-label="Send"
                  className="px-3 sm:px-4 py-2 bg-green-600 text-white rounded-md hover:bg-green-700 focus:outline-none focus:ring-2 focus:ring-green-500 focus:ring-offset-2 disabled:bg-gray-300 dark:disabled:bg-gray-600 disabled:cursor-not-allowed text-sm font-medium"
                >
                  {isLoading ? 'Sending...' : 'Comment'}
                </button>
              </div>
            </div>
          </div>
        </div>

      </div>

        </div> {/* end: Chat content column */}
      </div> {/* end: Body (sidebar + chat) */}

      {/* Template Selection Modal */}
      {showTemplateModal && (
        <div 
          className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
          onClick={(e) => {
            if (e.target === e.currentTarget) {
              setShowTemplateModal(false)
            }
          }}
        >
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-2xl w-full max-h-[80vh] overflow-hidden">
            <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold text-gray-900 dark:text-white">テンプレートから選択</h2>
                <button
                  onClick={() => setShowTemplateModal(false)}
                  className="text-gray-400 hover:text-gray-500 dark:hover:text-gray-300"
                >
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
            </div>
            
            <div className="overflow-y-auto max-h-[calc(80vh-8rem)]">
              {/* Recent Messages Section */}
              {recentMessages.length > 0 && (
                <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
                  <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">最近のメッセージ</h3>
                  <div className="space-y-2">
                    {recentMessages.map((message, index) => (
                      <button
                        key={index}
                        onClick={() => {
                          setInputValue(message);
                          setShowTemplateModal(false);
                        }}
                        className="w-full text-left px-3 py-2 bg-gray-50 dark:bg-gray-700 hover:bg-gray-100 dark:hover:bg-gray-600 rounded-md transition-colors"
                      >
                        <div className="text-sm text-gray-900 dark:text-white line-clamp-2">
                          {message}
                        </div>
                      </button>
                    ))}
                  </div>
                </div>
              )}
              
              {/* Templates Section */}
              <div className="px-6 py-4">
                <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">テンプレート</h3>
                {templates.length > 0 ? (
                  <div className="space-y-2">
                    {templates.map((template) => (
                      <button
                        key={template.id}
                        onClick={() => {
                          setInputValue(template.content);
                          setShowTemplateModal(false);
                        }}
                        className="w-full text-left px-3 py-3 bg-gray-50 dark:bg-gray-700 hover:bg-gray-100 dark:hover:bg-gray-600 rounded-md transition-colors"
                      >
                        <div className="font-medium text-sm text-gray-900 dark:text-white">{template.name}</div>
                        <div className="text-sm text-gray-600 dark:text-gray-400 mt-1 line-clamp-2">
                          {template.content}
                        </div>
                      </button>
                    ))}
                  </div>
                ) : (
                  <div className="text-center py-8 text-gray-500 dark:text-gray-400">
                    <p className="text-sm">テンプレートがありません</p>
                    <button
                      onClick={() => {
                        setShowTemplateModal(false);
                        router.push('/settings?tab=templates');
                      }}
                      className="mt-2 text-sm text-blue-600 dark:text-blue-400 hover:underline"
                    >
                      テンプレートを作成
                    </button>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Related Links Modal */}
      {showPRLinks && (
        <div 
          className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
          onClick={(e) => {
            if (e.target === e.currentTarget) {
              setShowPRLinks(false)
            }
          }}
        >
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-2xl w-full max-h-[80vh] overflow-hidden">
            <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
                  関連リンク ({relatedLinks.length}件)
                </h2>
                <button
                  onClick={() => setShowPRLinks(false)}
                  className="text-gray-400 hover:text-gray-500 dark:hover:text-gray-300"
                >
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
            </div>
            
            <div className="overflow-y-auto max-h-[calc(80vh-8rem)] px-6 py-4">
              {relatedLinks.length > 0 ? (
                <div className="space-y-3">
                  {relatedLinks.map((link, index) => {
                    const { domain, number, repoName, isGitHubCom } = getGitHubLinkParts(link.url, link.type);
                    const label = link.type === 'pr' ? 'Pull Request' : 'Issue';
                    const shortLabel = link.type === 'pr' ? 'PR' : 'Issue';
                    
                    return (
                      <div key={index} className="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-700 rounded-md">
                        <div className="flex-1 min-w-0">
                          <div className="font-medium text-sm text-gray-900 dark:text-white truncate">
                            {repoName || link.url}
                          </div>
                          <div className="text-xs text-gray-500 dark:text-gray-400">
                            {!isGitHubCom && (
                              <span>{domain} • </span>
                            )}
                            {label}{number ? ` #${number}` : ''}
                          </div>
                        </div>
                        <div className="flex items-center space-x-2 ml-4">
                          <button
                            onClick={() => navigator.clipboard.writeText(link.url)}
                            className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 rounded"
                            title="URLをコピー"
                          >
                            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                            </svg>
                          </button>
                          <a
                            href={link.url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center px-3 py-1 bg-gray-900 hover:bg-gray-800 dark:bg-gray-100 dark:hover:bg-gray-200 text-white dark:text-gray-900 text-xs rounded-md transition-colors"
                          >
                            {shortLabel}
                            <svg className="w-3 h-3 ml-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
                            </svg>
                          </a>
                        </div>
                      </div>
                    );
                  })}
                </div>
              ) : (
                <div className="text-center py-8 text-gray-500 dark:text-gray-400">
                  <svg className="w-12 h-12 mx-auto mb-3 text-gray-300 dark:text-gray-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1} d="M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4" />
                  </svg>
                  <p className="text-sm">関連リンクが見つかりませんでした</p>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Font Settings Popup */}
      {showFontSettings && (
        <div
          className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
          onClick={(e) => {
            if (e.target === e.currentTarget) {
              setShowFontSettings(false)
            }
          }}
        >
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-md w-full">
            <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
                  フォント設定
                </h2>
                <button
                  onClick={() => setShowFontSettings(false)}
                  className="text-gray-400 hover:text-gray-500 dark:hover:text-gray-300"
                >
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
            </div>

            <div className="px-6 py-4 space-y-6">
              {/* Font Size Slider */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  フォントサイズ: {fontSettings.fontSize}px
                </label>
                <input
                  type="range"
                  min="12"
                  max="20"
                  value={fontSettings.fontSize}
                  onChange={(e) => handleFontSizeChange(parseInt(e.target.value))}
                  className="w-full h-2 bg-gray-200 dark:bg-gray-700 rounded-lg appearance-none cursor-pointer accent-purple-600"
                />
                <div className="flex justify-between text-xs text-gray-500 dark:text-gray-400 mt-1">
                  <span>12px</span>
                  <span>20px</span>
                </div>
              </div>

              {/* Font Family */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
                  フォントファミリー
                </label>
                <div className="space-y-2">
                  <label className={`flex items-center p-3 rounded-lg border cursor-pointer transition-colors ${
                    fontSettings.fontFamily === 'sans-serif'
                      ? 'border-purple-500 bg-purple-50 dark:bg-purple-900/20'
                      : 'border-gray-200 dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-800'
                  }`}>
                    <input
                      type="radio"
                      name="fontFamily"
                      value="sans-serif"
                      checked={fontSettings.fontFamily === 'sans-serif'}
                      onChange={() => handleFontFamilyChange('sans-serif')}
                      className="h-4 w-4 text-purple-600 focus:ring-purple-500"
                    />
                    <div className="ml-3">
                      <span className="block text-sm font-medium text-gray-900 dark:text-white">
                        Sans-serif
                      </span>
                      <span className="block text-xs text-gray-500 dark:text-gray-400">
                        通常のフォント（読みやすさ重視）
                      </span>
                    </div>
                  </label>

                  <label className={`flex items-center p-3 rounded-lg border cursor-pointer transition-colors ${
                    fontSettings.fontFamily === 'monospace'
                      ? 'border-purple-500 bg-purple-50 dark:bg-purple-900/20'
                      : 'border-gray-200 dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-800'
                  }`}>
                    <input
                      type="radio"
                      name="fontFamily"
                      value="monospace"
                      checked={fontSettings.fontFamily === 'monospace'}
                      onChange={() => handleFontFamilyChange('monospace')}
                      className="h-4 w-4 text-purple-600 focus:ring-purple-500"
                    />
                    <div className="ml-3">
                      <span className="block text-sm font-medium text-gray-900 dark:text-white font-mono">
                        Monospace
                      </span>
                      <span className="block text-xs text-gray-500 dark:text-gray-400">
                        等幅フォント（コード表示に適している）
                      </span>
                    </div>
                  </label>
                </div>
              </div>
            </div>

            <div className="px-6 py-4 border-t border-gray-200 dark:border-gray-700">
              <div className="flex justify-end">
                <button
                  onClick={() => setShowFontSettings(false)}
                  className="px-4 py-2 bg-purple-600 hover:bg-purple-700 text-white rounded-md transition-colors"
                >
                  閉じる
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Plan Approval Modal */}
      <PlanApprovalModal
        isOpen={showPlanModal}
        planContent={planContent}
        onApprove={() => handleApprovePlan(true)}
        onReject={() => handleApprovePlan(false)}
        onClose={() => setShowPlanModal(false)}
        isLoading={isLoading}
      />

      {/* AskUserQuestion Modal */}
      {showQuestionModal && pendingAction?.content?.questions && (
        <AskUserQuestionModal
          questions={pendingAction.content.questions}
          onSubmit={handleAnswerSubmit}
          onClose={handleQuestionModalClose}
        />
      )}
    </div>
  );
}
