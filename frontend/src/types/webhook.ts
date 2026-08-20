import { ResourceScope } from './agentapi';

// Webhook types
export type WebhookType = 'github' | 'custom';
export type WebhookStatus = 'active' | 'paused';
export type WebhookSignatureType = 'hmac' | 'static';

// GitHub webhook configuration
export interface GitHubWebhookConfig {
  enterprise_url?: string;
  allowed_events?: string[];
  allowed_repositories?: string[];
}

// GitHub-specific trigger conditions
export interface GitHubConditions {
  events?: string[];
  actions?: string[];
  branches?: string[];
  repositories?: string[];
  labels?: string[];
  paths?: string[];
  base_branches?: string[];
  draft?: boolean;
  sender?: string[];
}

// Trigger conditions
export interface TriggerConditions {
  github?: GitHubConditions;
  go_template?: string;
}

// Session parameters for webhook triggers
export interface SessionParams {
  message?: string;
  github_token?: string;
  agent_type?: string;
  oneshot?: boolean;
  credential_source?: 'session_user' | 'triggered_user' | 'team' | 'none';
}

// Session configuration for webhook triggers
export interface WebhookSessionConfig {
  environment?: Record<string, string>;
  tags?: Record<string, string>;
  initial_message_template?: string;
  reuse_message_template?: string;
  reuse_session?: boolean;
  mount_payload?: boolean;
  params?: SessionParams;
  session_profile_id?: string;
}

// Webhook trigger
export interface WebhookTrigger {
  id?: string;
  name: string;
  priority?: number;
  enabled?: boolean;
  conditions: TriggerConditions;
  session_config?: WebhookSessionConfig;
  stop_on_match?: boolean;
}

// Webhook delivery record
export interface WebhookDeliveryRecord {
  id: string;
  received_at: string;
  status: 'processed' | 'skipped' | 'failed';
  matched_trigger?: string;
  session_id?: string;
  session_reused?: boolean;
  error?: string;
}

// Webhook entity
export interface Webhook {
  id: string;
  name: string;
  user_id?: string;
  scope?: ResourceScope;
  team_id?: string;
  status: WebhookStatus;
  type: WebhookType;
  secret?: string;
  webhook_url?: string;
  signature_header?: string;
  signature_type?: WebhookSignatureType;
  signature_prefix?: string;
  github?: GitHubWebhookConfig;
  triggers: WebhookTrigger[];
  session_config?: WebhookSessionConfig;
  created_at: string;
  updated_at: string;
  last_delivery?: WebhookDeliveryRecord;
  delivery_count?: number;
}

// Create webhook request
export interface CreateWebhookRequest {
  name: string;
  type: WebhookType;
  secret?: string;
  signature_header?: string;
  signature_type?: WebhookSignatureType;
  signature_prefix?: string;
  github?: GitHubWebhookConfig;
  triggers: WebhookTrigger[];
  session_config?: WebhookSessionConfig;
  scope?: ResourceScope;
  team_id?: string;
}

// Update webhook request
export interface UpdateWebhookRequest {
  name?: string;
  status?: WebhookStatus;
  secret?: string;
  signature_header?: string;
  signature_type?: WebhookSignatureType;
  signature_prefix?: string;
  github?: GitHubWebhookConfig;
  triggers?: WebhookTrigger[];
  session_config?: WebhookSessionConfig;
}

// Webhook list parameters
export interface WebhookListParams {
  type?: WebhookType;
  status?: WebhookStatus;
  page?: number;
  limit?: number;
  scope?: ResourceScope;
  team_id?: string;
}

// Webhook list response
export interface WebhookListResponse {
  webhooks: Webhook[];
  total?: number;
  page?: number;
  limit?: number;
}

// Regenerate secret response
export interface RegenerateSecretResponse {
  id: string;
  secret: string;
}

// Common GitHub events for UI
export const GITHUB_EVENTS = [
  { label: 'Pull Request', value: 'pull_request', description: 'PRの作成、更新、クローズ時' },
  { label: 'Push', value: 'push', description: 'コードがプッシュされた時' },
  { label: 'Issues', value: 'issues', description: 'Issueの作成、更新、クローズ時' },
  { label: 'Issue Comment', value: 'issue_comment', description: 'Issue/PRにコメントされた時' },
  { label: 'Pull Request Review', value: 'pull_request_review', description: 'PRレビューが送信された時' },
  { label: 'Pull Request Review Comment', value: 'pull_request_review_comment', description: 'PRレビューコメントが追加された時' },
  { label: 'Create', value: 'create', description: 'ブランチ/タグが作成された時' },
  { label: 'Delete', value: 'delete', description: 'ブランチ/タグが削除された時' },
  { label: 'Release', value: 'release', description: 'リリースが公開された時' },
  { label: 'Workflow Run', value: 'workflow_run', description: 'GitHub Actionsのワークフローが実行された時' },
];

// Common GitHub actions for UI
export const GITHUB_ACTIONS = {
  pull_request: [
    { label: 'Opened', value: 'opened' },
    { label: 'Synchronize', value: 'synchronize' },
    { label: 'Closed', value: 'closed' },
    { label: 'Reopened', value: 'reopened' },
    { label: 'Ready for Review', value: 'ready_for_review' },
    { label: 'Converted to Draft', value: 'converted_to_draft' },
    { label: 'Labeled', value: 'labeled' },
    { label: 'Unlabeled', value: 'unlabeled' },
  ],
  issues: [
    { label: 'Opened', value: 'opened' },
    { label: 'Closed', value: 'closed' },
    { label: 'Reopened', value: 'reopened' },
    { label: 'Labeled', value: 'labeled' },
    { label: 'Unlabeled', value: 'unlabeled' },
    { label: 'Assigned', value: 'assigned' },
  ],
  issue_comment: [
    { label: 'Created', value: 'created' },
    { label: 'Edited', value: 'edited' },
    { label: 'Deleted', value: 'deleted' },
  ],
  pull_request_review: [
    { label: 'Submitted', value: 'submitted' },
    { label: 'Edited', value: 'edited' },
    { label: 'Dismissed', value: 'dismissed' },
  ],
  release: [
    { label: 'Published', value: 'published' },
    { label: 'Created', value: 'created' },
    { label: 'Edited', value: 'edited' },
    { label: 'Prereleased', value: 'prereleased' },
    { label: 'Released', value: 'released' },
  ],
};

// Trigger webhook request
export interface TriggerWebhookRequest {
  payload: Record<string, unknown>;
  event?: string;
  dry_run?: boolean;
}

// Matched trigger info
export interface TriggerMatchedTriggerInfo {
  id: string;
  name: string;
}

// Trigger webhook response
export interface TriggerWebhookResponse {
  matched: boolean;
  matched_trigger?: TriggerMatchedTriggerInfo;
  session_id?: string;
  session_reused?: boolean;
  dry_run: boolean;
  initial_message?: string;
  tags?: Record<string, string>;
  environment?: Record<string, string>;
  error?: string;
}
