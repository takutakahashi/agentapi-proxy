use serde::{Deserialize, Serialize};

/// Mirrors `nativeStatusOutput` in `backend/cmd/native.go`.
/// Produced by `agentapi-proxy native status --json`.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
#[serde(rename_all = "snake_case")]
pub struct NativeStatus {
    #[serde(default = "default_instance")]
    pub instance: String,
    pub service: String,
    pub manager_id: String,
    pub upstream: String,
    #[serde(default)]
    pub labels: std::collections::BTreeMap<String, String>,
    pub version: String,
    #[serde(default)]
    pub filesystem_sandbox: bool,
    #[serde(default)]
    pub active_sessions: i64,
    pub health: String,
    pub state: String,
}

fn default_instance() -> String {
    "default".to_string()
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NativeInstance {
    pub instance: String,
    pub service: String,
    pub manager_id: String,
    pub upstream: String,
    pub state: String,
    pub config: String,
    #[serde(default)]
    pub running: bool,
}

/// Mirrors `nativeSessionListEntry` in `backend/cmd/native.go`.
/// Produced by `agentapi-proxy native sessions --json`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NativeSession {
    pub id: String,
    #[serde(default)]
    pub pid: i64,
    /// RFC3339 timestamp serialized by Go's `time.Time`.
    #[serde(default)]
    pub started_at: String,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub log_path: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DoctorResult {
    pub ok: bool,
    #[serde(default)]
    pub checks: Vec<DoctorCheck>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DoctorCheck {
    pub id: String,
    pub status: String,
    pub message: String,
}

/// Generic outcome for `native restart`, which only signals success via the
/// process exit status.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CommandResult {
    pub ok: bool,
    pub message: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ManagerEnvironment {
    pub path: String,
    pub commands: Vec<CommandCheck>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CommandCheck {
    pub command: String,
    pub required: bool,
    pub found: bool,
    pub path: String,
    pub version: String,
    pub message: String,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct UpdateManagerEnvironmentRequest {
    #[serde(default = "default_instance")]
    pub instance: String,
    pub path: String,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct InstallRequest {
    #[serde(default = "default_instance")]
    pub instance: String,
    pub upstream: String,
    pub listen: String,
    pub name: String,
    pub registration_token: String,
    #[serde(default)]
    pub filesystem_sandbox: bool,
    #[serde(default)]
    pub setup_nodejs: bool,
    #[serde(default = "default_nodejs_version")]
    pub nodejs_version: String,
    #[serde(default)]
    pub setup_github_cli: bool,
    #[serde(default = "default_github_cli_version")]
    pub github_cli_version: String,
}

fn default_nodejs_version() -> String {
    "lts".to_string()
}

fn default_github_cli_version() -> String {
    "latest".to_string()
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ResetRequest {
    #[serde(default = "default_instance")]
    pub instance: String,
    pub force: bool,
}
