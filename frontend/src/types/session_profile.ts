import { ResourceScope, SandboxConfig, DockerConfig } from './agentapi';
import type { APIMCPServerConfig } from './settings';

// Session profile params
export interface SessionProfileParams {
  initial_message?: string;
  github_token?: string;
  agent_type?: string;
  sandbox?: SandboxConfig;
  docker?: DockerConfig;
  auth_proxy?: boolean;
  session_ttl?: string;
  unsynced_file_paths?: string[];
  credential_source?: CredentialSource;
}

export type CredentialSource = 'session_user' | 'team' | 'none';

// Session profile config
export interface SessionProfileConfig {
  environment?: Record<string, string>;
  tags?: Record<string, string>;
  pool?: string;
  initial_message_template?: string;
  reuse_message_template?: string;
  reuse_session?: boolean;
  memory_key?: Record<string, string>;
  params?: SessionProfileParams;
  sandbox_policy_id?: string;
  session_ttl?: string;
  unsynced_file_paths?: string[];
  mcp_servers?: Record<string, APIMCPServerConfig>;
}

// SessionProfile entity
export interface SessionProfile {
  id: string;
  name: string;
  description?: string;
  user_id?: string;
  scope?: ResourceScope;
  team_id?: string;
  is_default?: boolean;
  config?: SessionProfileConfig;
  created_at: string;
  updated_at: string;
}

// Create SessionProfile request
export interface CreateSessionProfileRequest {
  name: string;
  description?: string;
  scope?: ResourceScope;
  team_id?: string;
  is_default?: boolean;
  config?: SessionProfileConfig;
}

// Update SessionProfile request
export interface UpdateSessionProfileRequest {
  name?: string;
  description?: string;
  is_default?: boolean;
  config?: SessionProfileConfig;
}

// SessionProfile list parameters
export interface SessionProfileListParams {
  scope?: ResourceScope;
  team_id?: string;
}

// SessionProfile list response
export interface SessionProfileListResponse {
  session_profiles: SessionProfile[];
  total?: number;
  page?: number;
  limit?: number;
}
