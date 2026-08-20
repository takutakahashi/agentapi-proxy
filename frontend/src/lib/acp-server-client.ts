/**
 * ACP (Agent Client Protocol) Server Client
 *
 * Communicates with the agentapi-proxy's global ACP server endpoints:
 *   POST /acp        – JSON-RPC 2.0 request/response
 *   GET  /acp        – SSE stream with Acp-Session-Id header (Streamable HTTP draft)
 *
 * Unlike per-session ACP bridge endpoints (/{sessionId}/session, /{sessionId}/sse),
 * these are proxy-wide endpoints that handle full session lifecycle via JSON-RPC 2.0.
 */

import { SessionMessage, PendingAction } from '../types/agentapi';
import { loadFullGlobalSettings, getDefaultProxySettings } from '../types/settings';

// ─── JSON-RPC types ───────────────────────────────────────────────────────────

interface JSONRPCRequest {
  jsonrpc: '2.0';
  id: number;
  method: string;
  params?: unknown;
}

interface JSONRPCResponse<T = unknown> {
  jsonrpc: '2.0';
  id?: number | string | null;
  result?: T;
  error?: { code: number; message: string; data?: unknown };
  method?: string;
  params?: unknown;
}

// ─── ACP session types ────────────────────────────────────────────────────────

export interface ACPServerSession {
  sessionId: string;
  cwd?: string;
  title?: string;
  updatedAt?: string;
  _meta?: {
    status?: string;
    scope?: string;
    teamId?: string;
    tags?: Record<string, string>;
  };
}

export interface ACPServerCapabilities {
  list?: boolean;
  close?: boolean;
  resume?: boolean;
  loadSession?: boolean;
}

export interface ACPServerInfo {
  protocolVersion: string;
  capabilities: ACPServerCapabilities;
  serverInfo: { name: string; version?: string };
}

export interface ACPServerCreateSessionParams {
  cwd?: string;
  message?: string;
  agentType?: string;
  mcpServers?: unknown[];
  tags?: Record<string, string>;
}

export interface ACPServerListSessionsParams {
  cwd?: string;
  cursor?: string;
}

// ─── SSE event types (mirrors the per-session bridge format) ─────────────────

interface ACPSessionUpdate {
  sessionUpdate: string;
  content?: unknown;
  messageId?: string;
  toolCallId?: string;
  kind?: string;
  title?: string;
  status?: string;
  rawInput?: unknown;
  rawOutput?: unknown;
  locations?: Array<{ path: string; line?: number }>;
  entries?: Array<{ content: string; status?: string; priority?: number }>;
  mode?: string;
  configOptions?: ACPConfigOption[];
}

export interface ACPConfigOption {
  id?: string;
  key?: string;
  name?: string;
  description?: string;
  category?: string;
  type?: string;
  value?: unknown;
  currentValue?: unknown;
  default?: unknown;
  options?: unknown;
}

export interface ACPPromptContentBlock {
  type: 'text' | 'image';
  text?: string;
  mimeType?: string;
  data?: string;
}

interface ACPPermissionOption {
  optionId: string;
  name: string;
  description?: string;
}

interface ACPPermissionParams {
  sessionId: string;
  toolCall: { toolCallId: string; kind?: string };
  options: ACPPermissionOption[];
}

// ─── Callback types ───────────────────────────────────────────────────────────

export interface ACPServerEventCallbacks {
  onMessage: (message: SessionMessage) => void;
  onChunk?: (msgId: number, text: string) => void;
  onImageChunk?: (msgId: number, image: { mimeType: string; data: string }) => void;
  onThoughtChunk?: (msgId: number, thought: string) => void;
  onToolUpdate?: (toolCallId: string, status: string) => void;
  onToolInputUpdate?: (toolCallId: string, input: unknown, title?: string, locations?: Array<{ path: string; line?: number }>) => void;
  onStatus?: (status: { status: 'stable' | 'running' | 'error'; agent_type?: string }) => void;
  onPermission?: (action: PendingAction, rpcId: number) => void;
  onTitleUpdate?: (title: string) => void;
  onModeUpdate?: (mode: string) => void;
  onConfigOptionsUpdate?: (configOptions: ACPConfigOption[]) => void;
  onConnectionOpen?: () => void;
  onConnectionConnecting?: (attempt?: number, delayMs?: number) => void;
  onConnectionClosed?: () => void;
  onError?: (err: Event | Error) => void;
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

const ACP_KIND_TO_NAME: Record<string, string> = {
  execute: 'Bash',
  read: 'Read',
  write: 'Write',
  edit: 'Edit',
  search: 'Search',
  glob: 'Glob',
  other: 'Tool',
  think: 'Think',
  task: 'Task',
  explore: 'Explore',
};

function titleToToolName(title: string | undefined): string | undefined {
  if (!title) return undefined;
  const match = title.match(/^[A-Za-z][A-Za-z0-9_-]*/);
  if (!match) return undefined;
  return match[0];
}

function acpToolDisplayName(kind?: string, title?: string): string {
  if (kind && ACP_KIND_TO_NAME[kind]) return ACP_KIND_TO_NAME[kind];
  return titleToToolName(title) || kind || 'tool';
}

function acpToolNameFromRawInput(rawInput: unknown): string | undefined {
  if (!rawInput || typeof rawInput !== 'object' || Array.isArray(rawInput)) return undefined;
  const toolName = (rawInput as Record<string, unknown>)._toolName;
  if (typeof toolName !== 'string' || toolName.length === 0) return undefined;
  return ACP_KIND_TO_NAME[toolName] || titleToToolName(toolName) || toolName;
}

function acpExtractText(content: unknown): string {
  if (!content || typeof content !== 'object' || Array.isArray(content)) return '';
  const obj = content as Record<string, unknown>;
  if (typeof obj.text === 'string') return obj.text;
  return '';
}

function acpExtractImage(content: unknown): { mimeType: string; data: string } | null {
  if (!content || typeof content !== 'object' || Array.isArray(content)) return null;
  const obj = content as Record<string, unknown>;
  if (obj.type === 'image' && typeof obj.mimeType === 'string' && typeof obj.data === 'string') {
    return { mimeType: obj.mimeType, data: obj.data };
  }
  return null;
}

function extractACPToolContent(content: unknown): string {
  if (!content || !Array.isArray(content)) return '';
  const texts: string[] = [];
  for (const item of content) {
    if (typeof item === 'object' && item !== null) {
      const block = item as Record<string, unknown>;
      if (block.type === 'content' && block.content) {
        const inner = block.content as Record<string, unknown>;
        if (typeof inner.text === 'string') texts.push(inner.text);
      } else if (block.type === 'diff' || block.type === 'terminal') {
        if (typeof block.content === 'string') texts.push(block.content);
      }
    }
  }
  return texts.join('\n');
}

function extractACPToolImages(content: unknown): Array<{ mimeType: string; data: string }> {
  if (!Array.isArray(content)) return [];
  return content.flatMap(item => {
    if (!item || typeof item !== 'object') return [];
    const block = item as Record<string, unknown>;
    if (block.type !== 'content' || !block.content || typeof block.content !== 'object') return [];
    const image = acpExtractImage(block.content);
    return image ? [image] : [];
  });
}

function extractRawOutputText(rawOutput: unknown): string {
  if (rawOutput == null) return '';
  if (typeof rawOutput === 'string') return rawOutput;
  if (Array.isArray(rawOutput)) {
    return rawOutput.map(b => {
      if (typeof b === 'string') return b;
      if (typeof b === 'object' && b !== null) {
        const obj = b as Record<string, unknown>;
        if (typeof obj.text === 'string') return obj.text;
        if (typeof obj.content === 'string') return obj.content;
        return JSON.stringify(obj);
      }
      return '';
    }).join('\n');
  }
  if (typeof rawOutput === 'object' && rawOutput !== null) {
    const obj = rawOutput as Record<string, unknown>;
    if (typeof obj.text === 'string') return obj.text;
    if (typeof obj.content === 'string') return obj.content;
    const outputParts: string[] = [];
    if (typeof obj.stdout === 'string' && obj.stdout.length > 0) outputParts.push(obj.stdout);
    if (typeof obj.stderr === 'string' && obj.stderr.length > 0) outputParts.push(obj.stderr);
    if (outputParts.length > 0) {
      if (typeof obj.exitCode === 'number') outputParts.push(`exit code: ${obj.exitCode}`);
      return outputParts.join('\n');
    }
    return JSON.stringify(rawOutput);
  }
  return '';
}

function acpPlanToMarkdown(entries: NonNullable<ACPSessionUpdate['entries']>): string {
  let md = '## Plan\n\n';
  for (const e of entries) {
    const check = e.status === 'done' ? '[x]' : '[ ]';
    md += `- ${check} ${e.content}\n`;
  }
  return md;
}

// ─── ACP Server Client ────────────────────────────────────────────────────────

export class ACPServerClient {
  private baseURL: string;
  private apiKey?: string;
  private requestId = 1;

  constructor(baseURL: string, apiKey?: string) {
    // baseURL is the proxy base, e.g. "http://localhost:3000/api/proxy"
    // We append /acp to reach the global ACP server endpoint.
    this.baseURL = baseURL;
    this.apiKey = apiKey;
  }

  private get acpUrl(): string {
    return `${this.baseURL}/acp`;
  }

  private getHeaders(): Record<string, string> {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    if (this.apiKey) {
      headers['Authorization'] = `Bearer ${this.apiKey}`;
    }
    return headers;
  }

  private async rpc<T>(method: string, params?: unknown): Promise<T> {
    const id = this.requestId++;
    const body: JSONRPCRequest = { jsonrpc: '2.0', id, method };
    if (params !== undefined) body.params = params;

    const response = await fetch(this.acpUrl, {
      method: 'POST',
      headers: this.getHeaders(),
      body: JSON.stringify(body),
    });

    if (!response.ok) {
      throw new Error(`ACP RPC failed: ${response.status} ${response.statusText}`);
    }

    const data: JSONRPCResponse<T> = await response.json();
    if (data.error) {
      throw new Error(`ACP RPC error [${data.error.code}]: ${data.error.message}`);
    }
    return data.result as T;
  }

  /** Protocol handshake. Call once before using other methods. */
  async initialize(): Promise<ACPServerInfo> {
    return this.rpc<ACPServerInfo>('initialize', {
      protocolVersion: '2025-01-15',
      clientInfo: { name: 'agentapi-ui', version: '1.0' },
    });
  }

  /** Create a new agent session. Returns the proxy session ID. */
  async createSession(params: ACPServerCreateSessionParams): Promise<{ sessionId: string }> {
    return this.rpc<{ sessionId: string }>('session/new', {
      cwd: params.cwd || '/home/user',
      mcpServers: params.mcpServers || [],
      _meta: {
        tags: params.tags || {},
        params: {
          message: params.message,
          agentType: params.agentType,
        },
      },
    });
  }

  /** List sessions for the authenticated user. */
  async listSessions(params?: ACPServerListSessionsParams): Promise<{ sessions: ACPServerSession[] }> {
    return this.rpc<{ sessions: ACPServerSession[] }>('session/list', params);
  }

  /** Close (delete) a session. */
  async closeSession(sessionId: string): Promise<void> {
    await this.rpc<void>('session/close', { sessionId });
  }

  /**
   * Confirm a session is active. Returns without error if accessible.
   * Throws if the session is not active/running.
   */
  async resumeSession(sessionId: string): Promise<void> {
    await this.rpc<void>('session/resume', { sessionId });
  }

  /** Confirm a session exists and is accessible. */
  async loadSession(sessionId: string): Promise<void> {
    await this.rpc<void>('session/load', { sessionId });
  }

  /** Send a prompt to the session. Response arrives via the SSE stream. */
  async sendPrompt(sessionId: string, prompt: ACPPromptContentBlock[], promptId?: number): Promise<void> {
    const id = promptId ?? this.requestId++;
    const body: JSONRPCRequest = {
      jsonrpc: '2.0',
      id,
      method: 'session/prompt',
      params: {
        sessionId,
        prompt,
      },
    };
    const response = await fetch(this.acpUrl, {
      method: 'POST',
      headers: this.getHeaders(),
      body: JSON.stringify(body),
    });
    if (!response.ok) {
      throw new Error(`ACP sendPrompt failed: ${response.status} ${response.statusText}`);
    }
    const data: JSONRPCResponse = await response.json();
    if (data.error) {
      throw new Error(`ACP sendPrompt error: ${data.error.message}`);
    }
  }

  /** Cancel the current session turn. */
  async cancelSession(sessionId: string): Promise<void> {
    await this.rpc<void>('session/cancel', { sessionId });
  }

  /** Set a session configuration option, such as the active model. */
  async setSessionConfigOption(
    sessionId: string,
    configId: string,
    value: string
  ): Promise<{ configOptions: ACPConfigOption[] }> {
    return this.rpc<{ configOptions: ACPConfigOption[] }>('session/set_config_option', {
      sessionId,
      configId,
      value,
    });
  }

  /**
   * Send a permission response back to the agent.
   * The agent sends a session/request_permission via SSE; the client
   * replies with the chosen optionId. sessionId is the proxy session ID
   * sent as Acp-Session-Id header so the proxy can route the result.
   */
  async sendPermissionResponse(sessionId: string, rpcId: number, optionId: string): Promise<void> {
    const body = {
      jsonrpc: '2.0',
      id: rpcId,
      result: { outcome: { outcome: 'selected', optionId } },
    };
    await fetch(this.acpUrl, {
      method: 'POST',
      headers: { ...this.getHeaders(), 'Acp-Session-Id': sessionId },
      body: JSON.stringify(body),
    });
  }

  /**
   * Subscribe to session/update events via GET /acp with Acp-Session-Id header.
   * Uses fetch() + ReadableStream to allow custom headers (EventSource cannot).
   * sessionId is the proxy session ID. Returns a handle with close() to disconnect.
   */
  subscribeToEvents(
    sessionId: string,
    callbacks: ACPServerEventCallbacks
  ): { close: () => void } {
    const abortController = new AbortController();

    let localIdCounter = Date.now();
    const nextId = () => localIdCounter++;

    let streamingMsgId: number | null = null;
    let streamingThoughtId: number | null = null;
    let streamingUserMsgId: number | null = null;

    const dispatchEvent = (data: string) => {
      try {
        const msg: JSONRPCResponse = JSON.parse(data);

        if (msg.method === 'session/update') {
          const acpParams = msg.params as { sessionId: string; update: ACPSessionUpdate; time?: string };
          const update = acpParams?.update;
          if (!update) return;
          const now = acpParams?.time || new Date().toISOString();

          switch (update.sessionUpdate) {
            case 'agent_message_chunk': {
              const text = acpExtractText(update.content);
              const image = acpExtractImage(update.content);
              if (!text && !image) return;
              if (streamingMsgId === null) {
                streamingMsgId = nextId();
                callbacks.onMessage({
                  id: streamingMsgId,
                  role: 'agent',
                  content: text,
                  images: image ? [image] : undefined,
                  time: now,
                  type: 'normal',
                });
              } else {
                if (text) callbacks.onChunk?.(streamingMsgId, text);
                if (image) callbacks.onImageChunk?.(streamingMsgId, image);
              }
              callbacks.onStatus?.({ status: 'running' });
              break;
            }

            case 'agent_thought_chunk': {
              const thought = acpExtractText(update.content);
              if (!thought) return;
              if (streamingThoughtId === null) {
                streamingThoughtId = nextId();
                callbacks.onMessage({ id: streamingThoughtId, role: 'agent', content: '', thought, time: now, type: 'normal' });
              } else {
                callbacks.onThoughtChunk?.(streamingThoughtId, thought);
              }
              break;
            }

            case 'tool_call': {
              streamingMsgId = null;
              streamingThoughtId = null;
              const toolObj = {
                type: 'tool_use',
                name: acpToolNameFromRawInput(update.rawInput) || acpToolDisplayName(update.kind, update.title),
                id: update.toolCallId,
                input: update.rawInput ?? {},
                ...(update.title != null ? { title: update.title } : {}),
                ...(update.locations != null ? { locations: update.locations } : {}),
              };
              callbacks.onMessage({
                id: nextId(),
                role: 'agent',
                content: JSON.stringify(toolObj),
                time: now,
                type: 'normal',
                toolUseId: update.toolCallId,
              });
              break;
            }

            case 'tool_call_update': {
              const isSuccess = update.status === 'completed' || update.status === 'success';
              const isError = update.status === 'failed' || update.status === 'error';
              const isRunning = update.status === 'running' || update.status === 'in_progress';

              if (update.status && !isSuccess && !isError && !isRunning) {
                callbacks.onToolInputUpdate?.(update.toolCallId!, update.rawInput, update.title, update.locations);
                return;
              }

              if (isRunning) return;

              callbacks.onToolUpdate?.(update.toolCallId!, isError ? 'error' : 'success');

              const acpText = extractACPToolContent(update.content);
              const images = extractACPToolImages(update.content);
              const resultContent = acpText || extractRawOutputText(update.rawOutput);
              callbacks.onMessage({
                id: nextId(),
                role: 'tool_result',
                content: resultContent,
                images: images.length > 0 ? images : undefined,
                time: now,
                type: 'normal',
                parentToolUseId: update.toolCallId,
                status: isError ? 'error' : 'success',
              });
              break;
            }

            case 'plan': {
              streamingMsgId = null;
              streamingThoughtId = null;
              if (!update.entries || update.entries.length === 0) return;
              callbacks.onMessage({
                id: nextId(),
                role: 'agent',
                content: acpPlanToMarkdown(update.entries),
                time: now,
                type: 'plan',
              });
              break;
            }

            case 'user_message_chunk': {
              const text = acpExtractText(update.content);
              const image = acpExtractImage(update.content);
              if (!text && !image) return;
              if (image && streamingUserMsgId !== null) {
                callbacks.onImageChunk?.(streamingUserMsgId, image);
              } else {
                streamingUserMsgId = nextId();
                callbacks.onMessage({
                  id: streamingUserMsgId,
                  role: 'user',
                  content: text,
                  images: image ? [image] : undefined,
                  time: now,
                  type: 'normal',
                });
              }
              break;
            }

            case 'session_info_update': {
              if (update.title) callbacks.onTitleUpdate?.(update.title);
              break;
            }

            case 'current_mode_update': {
              if (update.mode) callbacks.onModeUpdate?.(update.mode);
              break;
            }

            case 'config_option_update': {
              if (update.configOptions && Array.isArray(update.configOptions)) {
                callbacks.onConfigOptionsUpdate?.(update.configOptions);
              }
              break;
            }

            case 'agent_turn_end': {
              streamingMsgId = null;
              streamingThoughtId = null;
              callbacks.onStatus?.({ status: 'stable' });
              break;
            }

            default:
              break;
          }
          return;
        }

        if (msg.method === 'session/request_permission' && msg.id != null) {
          const permParams = msg.params as ACPPermissionParams;
          if (!permParams || !callbacks.onPermission) return;

          const pendingAction: PendingAction = {
            type: 'answer_question',
            tool_use_id: permParams.toolCall?.toolCallId ?? '',
            content: {
              questions: [{
                question: 'Permission required',
                header: 'Permission Required',
                options: (permParams.options ?? []).map(o => ({
                  label: o.name || o.optionId,
                  description: o.description ?? '',
                })),
                multiSelect: false,
              }],
            },
          };

          const rpcId = typeof msg.id === 'number' ? msg.id : parseInt(String(msg.id ?? '0'), 10);
          callbacks.onPermission(pendingAction, rpcId);
          return;
        }

        // ── Result of session/prompt (turn finished) ───────────────────────
        // Only call stable for end_turn; ignore permission ACKs and other
        // result messages that arrive mid-turn (e.g. {"result":{}} with no stopReason).
        if (msg.result != null && msg.id != null) {
          const stopReason = (msg.result as { stopReason?: string })?.stopReason;
          if (stopReason === 'end_turn') {
            streamingMsgId = null;
            streamingThoughtId = null;
            callbacks.onStatus?.({ status: 'stable' });
          }
          return;
        }

        // ── Error on session/prompt ────────────────────────────────────────
        if (msg.error && msg.id != null) {
          streamingMsgId = null;
          streamingThoughtId = null;
          callbacks.onStatus?.({ status: 'stable' });
          callbacks.onError?.(new Error(msg.error.message));
          return;
        }
      } catch (err) {
        console.error('[ACPServerClient] Failed to parse SSE event:', err, data);
      }
    };

    const connect = async (retryDelay = 1000) => {
      if (abortController.signal.aborted) return;
      try {
        const response = await fetch(this.acpUrl, {
          method: 'GET',
          headers: {
            ...this.getHeaders(),
            'Accept': 'text/event-stream',
            'Acp-Session-Id': sessionId,
          },
          signal: abortController.signal,
        });

        if (!response.ok || !response.body) {
          callbacks.onError?.(new Error(`[ACPServerClient] SSE connection failed: ${response.status}`));
          if (!abortController.signal.aborted) {
            callbacks.onConnectionConnecting?.(undefined, retryDelay);
            setTimeout(() => connect(Math.min(retryDelay * 2, 30000)), retryDelay);
          }
          return;
        }

        callbacks.onConnectionOpen?.();

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });

          const lines = buffer.split('\n');
          buffer = lines.pop() ?? '';

          for (const line of lines) {
            if (line.startsWith('data: ')) {
              const data = line.slice(6).trim();
              if (data) dispatchEvent(data);
            }
          }
        }

        // Stream closed normally — reconnect after a short delay
        if (!abortController.signal.aborted) {
          callbacks.onConnectionConnecting?.(undefined, 1000);
          setTimeout(() => connect(1000), 1000);
        }
      } catch (err) {
        if (abortController.signal.aborted) return;
        console.error('[ACPServerClient] SSE fetch error:', err);
        callbacks.onConnectionConnecting?.(undefined, retryDelay);
        setTimeout(() => connect(Math.min(retryDelay * 2, 30000)), retryDelay);
      }
    };

    connect();

    return {
      close: () => {
        abortController.abort();
        callbacks.onConnectionClosed?.();
      },
    };
  }
}

// ─── Factory ──────────────────────────────────────────────────────────────────

export function createACPServerClientFromStorage(): ACPServerClient {
  if (typeof window === 'undefined') {
    return new ACPServerClient('http://localhost:3000/api/proxy');
  }

  try {
    const globalSettings = loadFullGlobalSettings();
    const proxySettings = globalSettings.agentApiProxy || getDefaultProxySettings();
    return new ACPServerClient(
      proxySettings.endpoint || `${window.location.protocol}//${window.location.host}/api/proxy`,
      proxySettings.apiKey || undefined
    );
  } catch {
    return new ACPServerClient(
      `${window.location.protocol}//${window.location.host}/api/proxy`
    );
  }
}
