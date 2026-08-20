import { ResourceScope } from './agentapi';

export interface SandboxPolicy {
  id: string;
  name: string;
  description?: string;
  allowed_domains?: string[];
  denied_domains?: string[];
  count_mode?: boolean;
  scope: ResourceScope;
  owner_id: string;
  team_id?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateSandboxPolicyRequest {
  name: string;
  description?: string;
  allowed_domains?: string[];
  denied_domains?: string[];
  count_mode?: boolean;
  scope: ResourceScope;
  team_id?: string;
}

export interface UpdateSandboxPolicyRequest {
  name?: string;
  description?: string;
  allowed_domains?: string[];
  denied_domains?: string[];
  count_mode?: boolean;
}

export interface SandboxPolicyListParams {
  scope?: ResourceScope;
  team_id?: string;
}

export interface SandboxPolicyListResponse {
  sandbox_policies: SandboxPolicy[];
  total?: number;
}

export interface SandboxPolicyDomainsResponse {
  allowed: string[];
  denied: string[];
  ignored: string[];
  updated_at?: string;
}
