// AgentAPI TypeScript types based on the API documentation

// Define AgentStatus locally
export interface AgentStatus {
  status: 'stable' | 'running' | 'error';
  last_activity?: string;
  current_task?: string;
  /** Provisioner error message when status is 'error' */
  message?: string;
}

export interface Agent {
  id: string;
  name: string;
  status: 'active' | 'inactive' | 'error';
  created_at: string;
  updated_at: string;
  config: AgentConfig;
  metrics?: AgentMetrics;
}

export interface AgentConfig {
  type: string;
  parameters: Record<string, unknown>;
  timeout?: number;
  retry_policy?: {
    max_retries: number;
    backoff_factor: number;
  };
}

export interface CreateAgentRequest {
  name: string;
  config: AgentConfig;
}

export interface UpdateAgentRequest {
  name?: string;
  config?: Partial<AgentConfig>;
}

export interface AgentListResponse {
  agents: Agent[];
  total: number;
  page: number;
  limit: number;
}

export interface AgentListParams {
  page?: number;
  limit?: number;
  status?: Agent['status'];
  name?: string;
}

// Metrics types
export interface SystemMetrics {
  cpu_usage: number;
  memory_usage: number;
  disk_usage: number;
  uptime: number;
}

export interface AgentMetrics {
  agent_id: string;
  requests_processed: number;
  success_rate: number;
  average_response_time: number;
  last_activity: string;
}

export interface RequestMetrics {
  total: number;
  per_minute: number;
  success_rate: number;
  error_rate: number;
}

export interface Metrics {
  timestamp: string;
  system: SystemMetrics;
  agents: AgentMetrics[];
  requests: RequestMetrics;
}

export interface MetricsHistoryParams {
  from: string;
  to: string;
  interval?: string;
  agent_id?: string;
}

// Configuration types
export interface LoggingConfig {
  level: 'info' | 'debug' | 'warn' | 'error';
  format: 'json' | 'text';
}

export interface AuthConfig {
  enabled: boolean;
  provider: string;
}

export interface LimitsConfig {
  max_agents: number;
  request_rate: number;
}

export interface SystemConfig {
  logging: LoggingConfig;
  auth: AuthConfig;
  limits: LimitsConfig;
}

// WebSocket types
export interface WebSocketMessage {
  type: 'subscribe' | 'unsubscribe' | 'agent_update' | 'metrics_update';
  channel?: 'agents' | 'metrics';
  agent_id?: string;
  interval?: number;
  data?: unknown;
}

export interface WebSocketOptions {
  reconnect?: boolean;
  reconnectInterval?: number;
  maxReconnectAttempts?: number;
}

// Error types
export interface APIError {
  code: string;
  message: string;
  details?: Record<string, unknown>;
  timestamp: string;
}

export interface APIErrorResponse {
  error: APIError;
}

// API Client configuration
export interface AgentAPIClientConfig {
  baseURL: string;
  apiKey?: string;
  timeout?: number;
  retryAttempts?: number;
  retryDelay?: number;
  debug?: boolean;
}

// Rate limiting
export interface RateLimitInfo {
  limit: number;
  remaining: number;
  reset: number;
}

// Session status types based on agentapi-proxy OpenAPI specification
export type SessionStatus = 'creating' | 'starting' | 'running' | 'active' | 'suspended' | 'unhealthy' | 'stopped' | 'error' | 'timeout' | 'unknown';

// Resource scope types for team support
export type ResourceScope = 'user' | 'team';

// Session types for agentapi-proxy
export interface SessionAnnotations {
  pr_url?: string;
  issue_url?: string;
  description?: string;
  running_task?: string;
}

export interface Session {
  session_id: string;
  user_id: string;
  status: SessionStatus;
  started_at: string;
  updated_at?: string;
  addr?: string;
  description?: string;
  environment?: Record<string, string>;
  metadata?: Record<string, unknown>;
  annotations?: SessionAnnotations;
  tags?: Record<string, string>;
  scope?: ResourceScope;
  team_id?: string;
  sandbox_policy_id?: string;
  error_message?: string;
}

export interface SessionListParams {
  user_id?: string;
  status?: Session['status'];
  page?: number;
  limit?: number;
  scope?: ResourceScope;
  team_id?: string;
}

export interface SessionListResponse {
  sessions: Session[];
  total: number;
  page: number;
  limit: number;
}

export interface SandboxConfig {
  enabled: boolean;
  policy_id?: string;
  allowed_domains?: string[];
  denied_domains?: string[];
  count_mode?: boolean;
}

export interface DockerRegistry {
  server?: string;
  username?: string;
  password?: string;
  secret_name?: string;
  insecure?: boolean;
}

export interface DockerConfig {
  enabled: boolean;
  registries?: DockerRegistry[];
}

export interface CreateSessionRequest {
  user_id: string;
  environment?: Record<string, string>;
  metadata?: Record<string, unknown>;
  tags?: Record<string, string>;
  params?: {
    message?: string;
    github_token?: string;
    oneshot?: boolean;
    sandbox?: SandboxConfig;
    docker?: DockerConfig;
    auth_proxy?: boolean;
    [key: string]: unknown;
  };
  scope?: ResourceScope;
  team_id?: string;
  memory_key?: Record<string, string>;
  memory_summarize_drafts?: boolean;
  session_profile_id?: string;
}

// Session message types
export interface SessionMessage {
  id: number; // Changed from string to number (matches API spec)
  role: 'user' | 'assistant' | 'system' | 'tool_result' | 'agent';
  content: string;
  /** Agent thinking / reasoning text (ACP agent_thought_chunk). Shown in a collapsible section. */
  thought?: string;
  /** Images carried by ACP image content blocks. */
  images?: Array<{ mimeType: string; data: string }>;
  timestamp?: string; // Made optional (for backwards compatibility)
  time: string; // ISO 8601 timestamp from API
  session_id?: string; // Made optional (not in API spec)
  metadata?: Record<string, unknown>;
  type?: 'normal' | 'error' | 'question' | 'plan';
  toolUseId?: string;
  parentToolUseId?: string;
  status?: 'success' | 'error';
}

export interface SessionMessageListResponse {
  messages: SessionMessage[];
  total?: number; // Total number of messages in conversation
  hasMore?: boolean; // Whether there are more messages available
  page?: number; // Deprecated, kept for backwards compatibility
  limit?: number; // Deprecated, kept for backwards compatibility
}

export interface SessionMessageListParams {
  // Cursor-based pagination
  before?: number; // Get messages with ID < before (load older messages)
  after?: number; // Get messages with ID > after (load newer messages)

  // Limit-based pagination
  limit?: number; // Number of messages to retrieve
  direction?: 'head' | 'tail'; // Direction for limit-based pagination

  // Context-based pagination
  around?: number; // Message ID to center around
  context?: number; // Number of messages before and after "around"

  // Deprecated, kept for backwards compatibility
  page?: number;
  from?: string;
  to?: string;
}

export interface SendSessionMessageRequest {
  content: string;
  type: 'user' | 'system' | 'raw';
}

// Session events types for Server-Sent Events
export interface SessionEventData {
  type: 'message' | 'status' | 'error';
  data: SessionMessage | AgentStatus | { error: string };
  timestamp: string;
}

export interface SessionEventsOptions {
  reconnect?: boolean;
  reconnectInterval?: number;
  maxReconnectAttempts?: number;
}

// Tool status types
export interface ToolStatusResponseBody {
  messages: SessionMessage[];
}

// Action types for AskUserQuestion
export interface QuestionOption {
  label: string;
  description: string;
}

export interface Question {
  question: string;
  header: string;
  options: QuestionOption[];
  multiSelect: boolean;
}

export interface PendingAction {
  type: 'answer_question' | 'approve_plan' | 'stop_agent';
  tool_use_id: string;
  content?: {
    questions?: Question[];
    [key: string]: unknown;
  };
}

export interface ActionRequest {
  type: 'answer_question' | 'approve_plan' | 'stop_agent';
  answers?: Record<string, string | string[]>;
  approved?: boolean;
}

export interface ActionResponse {
  ok?: boolean;
  pending_actions?: PendingAction[];
}
