/**
 * API Token types for the agentapi-proxy multi-token management API.
 *
 * Backend contract:
 *   GET    /api-tokens?scope=personal
 *   GET    /api-tokens?scope=team&team_id=org%2Fteam
 *   POST   /api-tokens            -> returns the plaintext token exactly once
 *   GET    /api-tokens/:id        -> metadata only (no plaintext)
 *   DELETE /api-tokens/:id
 *
 * The list endpoint returns `{ items: [...] }`.
 */

/** Token scope. The API uses the public vocabulary `personal` (not `user`) for user tokens. */
export type ApiTokenScope = 'personal' | 'team';

/**
 * Permissions offered when creating a token. Mirrors the proxy RBAC model
 * (internal/domain/entities/user.go) and the OpenAPI `CreateAPITokenRequest`
 * enum: session:create, session:read, session:update, session:delete, admin.
 */
export const API_TOKEN_PERMISSIONS = [
  'session:create',
  'session:read',
  'session:update',
  'session:delete',
  'admin',
] as const;

export type ApiTokenPermission = (typeof API_TOKEN_PERMISSIONS)[number];

/** Human-readable labels for the permission checkboxes. */
export const API_TOKEN_PERMISSION_LABELS: Record<ApiTokenPermission, string> = {
  'session:create': 'セッション作成 (session:create)',
  'session:read': 'セッション読み取り (session:read)',
  'session:update': 'セッション更新 (session:update)',
  'session:delete': 'セッション削除 (session:delete)',
  'admin': '管理者 (admin)',
};

/**
 * Token metadata returned by list / GET / DELETE. Never contains plaintext.
 * Mirrors the OpenAPI `APITokenMetadata` schema: `token_prefix`, `permissions`,
 * `created_by` and `created_at` are required by the contract; they are typed
 * as required here to match the canonical response shape.
 */
export interface ApiToken {
  id: string;
  name: string;
  /** Short, safe prefix of the secret for display, e.g. `ap_user_abc…`. */
  token_prefix: string;
  scope: ApiTokenScope;
  /** Identity the token authenticates as (owner for personal, service account id for team). */
  user_id?: string;
  /** Team ID (populated when scope is 'team'). */
  team_id?: string;
  /** Granted permissions, bounded by the creator's permissions. */
  permissions: string[];
  /** User ID of the token creator. */
  created_by: string;
  created_at: string;
  /** Last update timestamp. */
  updated_at?: string;
  /** ISO 8601 expiry timestamp, or null/undefined for no expiration. */
  expires_at?: string | null;
}

export interface ApiTokenListParams {
  scope: ApiTokenScope;
  team_id?: string;
}

export interface ApiTokenListResponse {
  items: ApiToken[];
}

export interface CreateApiTokenRequest {
  name: string;
  scope: ApiTokenScope;
  team_id?: string;
  permissions?: string[];
  /** ISO 8601 expiry timestamp, or null/omitted for no expiration. */
  expires_at?: string | null;
}

/**
 * Response from POST /api-tokens. Contains the full plaintext token exactly
 * once. The UI must never persist this value (no localStorage, no logs).
 */
export interface CreateApiTokenResponse {
  token: ApiToken;
  /** Plaintext API token. Shown once, then discarded. */
  plaintext_token: string;
}