// Shared types that mirror the Rust structs in src-tauri/src/types.rs.
// Keep these in sync with the serde JSON produced by the Tauri commands.

export interface NativeStatus {
  instance: string;
  service: string;
  manager_id: string;
  upstream: string;
  labels: Record<string, string>;
  version: string;
  filesystem_sandbox: boolean;
  active_sessions: number;
  health: string;
  state: string;
}

export interface NativeInstance {
  instance: string;
  service: string;
  manager_id: string;
  upstream: string;
  state: string;
  config: string;
  running: boolean;
}

export interface NativeSession {
  id: string;
  pid: number;
  started_at: string;
  status: string;
  log_path: string;
}

export interface DoctorResult {
  ok: boolean;
  checks: DoctorCheck[];
}

export interface DoctorCheck {
  id: string;
  status: "ok" | "error";
  message: string;
}

export interface RestartResult {
  ok: boolean;
  message: string;
}

export interface ManagerEnvironment {
  path: string;
  commands: CommandCheck[];
}

export interface CommandCheck {
  command: string;
  required: boolean;
  found: boolean;
  path: string;
  version: string;
  message: string;
}

export interface InstallRequest {
  instance: string;
  upstream: string;
  listen: string;
  name: string;
  registrationToken: string;
  filesystemSandbox: boolean;
  setupNodejs: boolean;
  nodejsVersion: string;
  setupGithubCli: boolean;
  githubCliVersion: string;
}

export interface ResetRequest {
  instance: string;
  force: boolean;
}

export type DashboardData = {
  status: NativeStatus | null;
  sessions: NativeSession[];
  doctor: DoctorResult | null;
  warnings?: string[];
};
