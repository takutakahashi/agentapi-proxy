use crate::binary::{run_binary_plain, run_native_json, run_native_logs, run_native_plain};
use crate::types::{
    CommandCheck, CommandResult, DoctorResult, InstallRequest, ManagerEnvironment, NativeInstance,
    NativeSession, NativeStatus, ResetRequest, UpdateManagerEnvironmentRequest,
};
use serde::de::DeserializeOwned;
use std::path::{Path, PathBuf};
use std::process::Command;
use tauri::AppHandle;

/// Whether this Mac has completed the native daemon installation.
///
/// `native doctor` can return a structured result even before installation,
/// so the frontend cannot infer this reliably from the dashboard commands.
#[tauri::command]
pub fn native_is_installed(instance: Option<String>) -> bool {
    native_config_path(instance.as_deref()).is_some_and(|path| path.is_file())
}

fn native_config_path(instance: Option<&str>) -> Option<std::path::PathBuf> {
    let directory = match instance.filter(|name| !name.is_empty() && *name != "default") {
        Some(name) => format!("agentapi-native-{name}"),
        None => "agentapi-native".to_string(),
    };
    std::env::var_os("HOME").map(|home| {
        std::path::PathBuf::from(home)
            .join("Library")
            .join("Application Support")
            .join(directory)
            .join("config.json")
    })
}

/// Parse the stdout of a `native <sub> --json` command into a typed value.
/// Tolerates leading/trailing whitespace and empty output (treated as an
/// explicit error so the UI can surface it).
fn parse_json<T: DeserializeOwned>(stdout: &[u8], sub: &str) -> Result<T, String> {
    let trimmed = stdout
        .iter()
        .skip_while(|b| b.is_ascii_whitespace())
        .copied()
        .collect::<Vec<_>>();
    if trimmed.is_empty() {
        return Err(format!("`native {sub} --json` produced no output"));
    }
    serde_json::from_slice::<T>(&trimmed)
        .map_err(|e| format!("failed to parse `native {sub} --json` output: {e}"))
}

/// `agentapi-proxy native status --json`.
#[tauri::command]
pub async fn native_status(
    app: AppHandle,
    instance: Option<String>,
) -> Result<NativeStatus, String> {
    let stdout = run_native_json(&app, "status", instance.as_deref()).await?;
    parse_json::<NativeStatus>(&stdout, "status")
}

/// `agentapi-proxy native sessions --json`.
#[tauri::command]
pub async fn native_sessions(
    app: AppHandle,
    instance: Option<String>,
) -> Result<Vec<NativeSession>, String> {
    let stdout = run_native_json(&app, "sessions", instance.as_deref()).await?;
    // `native sessions` is an alias of `session-list`; both emit a JSON array.
    parse_json::<Vec<NativeSession>>(&stdout, "sessions")
}

/// Return the tail of the session-manager log or a session's combined
/// provisioner/agent log. Following is intentionally handled by UI polling so
/// no long-lived child process crosses the Tauri IPC boundary.
#[tauri::command]
pub async fn native_logs(
    app: AppHandle,
    instance: Option<String>,
    session_id: Option<String>,
    daemon: bool,
    tail: Option<usize>,
) -> Result<String, String> {
    let tail = tail.unwrap_or(500).clamp(1, 5000);
    let session_id = session_id
        .as_deref()
        .map(str::trim)
        .filter(|id| !id.is_empty());
    if !daemon && session_id.is_none() {
        return Err("a session ID is required for session logs".to_string());
    }
    if daemon && session_id.is_some() {
        return Err("session ID cannot be combined with daemon logs".to_string());
    }
    run_native_logs(&app, instance.as_deref(), session_id, daemon, tail).await
}

/// `agentapi-proxy native doctor --json`.
///
#[tauri::command]
pub async fn native_doctor(
    app: AppHandle,
    instance: Option<String>,
) -> Result<DoctorResult, String> {
    let stdout = run_native_json(&app, "doctor", instance.as_deref()).await?;
    parse_json::<DoctorResult>(&stdout, "doctor")
}

/// `agentapi-proxy native restart`.
#[tauri::command]
pub async fn native_restart(
    app: AppHandle,
    instance: Option<String>,
) -> Result<CommandResult, String> {
    let (ok, message) = run_native_plain(&app, "restart", instance.as_deref()).await;
    Ok(CommandResult {
        ok,
        message: if message.is_empty() {
            if ok {
                "Daemon restarted.".to_string()
            } else {
                "Restart failed.".to_string()
            }
        } else {
            message
        },
    })
}

/// Replace the installed manager with the app-bundled binary and restart it.
#[tauri::command]
pub async fn native_update(
    app: AppHandle,
    instance: Option<String>,
) -> Result<CommandResult, String> {
    let args = update_args(instance.as_deref())
        .into_iter()
        .map(str::to_string)
        .collect();
    let (ok, message) = run_binary_plain(&app, args).await?;
    Ok(CommandResult {
        ok,
        message: if message.is_empty() {
            if ok {
                "Native manager updated and restarted.".to_string()
            } else {
                "Native manager update failed.".to_string()
            }
        } else {
            message
        },
    })
}

fn update_args(instance: Option<&str>) -> Vec<&str> {
    let mut args = vec!["native", "update"];
    if let Some(name) = instance.filter(|name| !name.is_empty() && *name != "default") {
        args.extend(["--instance", name]);
    }
    args
}

#[tauri::command]
pub async fn native_list(app: AppHandle) -> Result<Vec<NativeInstance>, String> {
    let stdout = run_native_json(&app, "list", None).await?;
    parse_json::<Vec<NativeInstance>>(&stdout, "list")
}

/// Register and install the per-user LaunchAgent using the bundled CLI.
#[tauri::command]
pub async fn native_install(
    app: AppHandle,
    request: InstallRequest,
) -> Result<CommandResult, String> {
    let upstream = request.upstream.trim();
    let name = request.name.trim();
    let instance = request.instance.trim();
    if !is_http_url(upstream) {
        return Err("upstream must start with http:// or https://".to_string());
    }
    if name.is_empty() || request.registration_token.trim().is_empty() {
        return Err("name and registration token are required".to_string());
    }
    if instance != "default" && instance != "" && request.listen.trim().is_empty() {
        return Err("listen address is required for a named instance".to_string());
    }

    let mise_path = if request.setup_nodejs || request.setup_github_cli {
        Some(setup_agent_tools_with_mise(
            request
                .setup_nodejs
                .then_some(request.nodejs_version.trim()),
            request
                .setup_github_cli
                .then_some(request.github_cli_version.trim()),
        )?)
    } else {
        None
    };

    let mut args = vec![
        "native",
        "install",
        "--upstream",
        upstream,
        "--name",
        name,
        "--registration-token",
        request.registration_token.trim(),
    ];
    if !instance.is_empty() && instance != "default" {
        args.extend(["--instance", instance]);
    }
    if !request.listen.trim().is_empty() {
        args.extend(["--listen", request.listen.trim()]);
    }
    if request.filesystem_sandbox {
        args.push("--filesystem-sandbox");
    }
    let manager_path;
    let manager_env;
    if let Some(mise) = mise_path.as_ref() {
        manager_path = mise_manager_path(mise, request.setup_nodejs, request.setup_github_cli)?;
        manager_env = format!("PATH={manager_path}");
        args.extend(["--manager-env", manager_env.as_str()]);
    }
    let (ok, message) =
        run_binary_plain(&app, args.into_iter().map(str::to_string).collect()).await?;
    Ok(CommandResult { ok, message })
}

const REQUIRED_MANAGER_COMMANDS: [&str; 5] = ["node", "npx", "mise", "gh", "git"];

/// Read the configured manager PATH and check the commands needed to launch agents.
#[tauri::command]
pub fn native_environment(instance: Option<String>) -> Result<ManagerEnvironment, String> {
    let config_path =
        native_config_path(instance.as_deref()).ok_or_else(|| "HOME is not set".to_string())?;
    let config = read_native_config(&config_path)?;
    let path = config
        .pointer("/manager_environment/PATH")
        .and_then(serde_json::Value::as_str)
        .unwrap_or("/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin")
        .to_string();
    let commands = REQUIRED_MANAGER_COMMANDS
        .iter()
        .map(|command| check_manager_command(command, &path))
        .collect();
    Ok(ManagerEnvironment { path, commands })
}

/// Atomically save the manager PATH, then restart the selected LaunchAgent.
#[tauri::command]
pub async fn native_update_environment(
    app: AppHandle,
    request: UpdateManagerEnvironmentRequest,
) -> Result<CommandResult, String> {
    let path = normalize_manager_path(&request.path)?;
    let config_path =
        native_config_path(Some(&request.instance)).ok_or_else(|| "HOME is not set".to_string())?;
    let mut config = read_native_config(&config_path)?;
    let root = config
        .as_object_mut()
        .ok_or_else(|| "native config must be a JSON object".to_string())?;
    let environment = root
        .entry("manager_environment")
        .or_insert_with(|| serde_json::json!({}));
    let environment = environment
        .as_object_mut()
        .ok_or_else(|| "manager_environment must be a JSON object".to_string())?;
    environment.insert("PATH".to_string(), serde_json::Value::String(path));
    write_native_config(&config_path, &config)?;

    let (ok, message) = run_native_plain(&app, "restart", Some(&request.instance)).await;
    Ok(CommandResult {
        ok,
        message: if message.is_empty() {
            if ok {
                "Manager PATH saved and daemon restarted.".to_string()
            } else {
                "Manager PATH was saved, but the daemon restart failed.".to_string()
            }
        } else {
            message
        },
    })
}

fn read_native_config(path: &Path) -> Result<serde_json::Value, String> {
    let bytes = std::fs::read(path)
        .map_err(|e| format!("failed to read native config {}: {e}", path.display()))?;
    serde_json::from_slice(&bytes)
        .map_err(|e| format!("failed to parse native config {}: {e}", path.display()))
}

fn write_native_config(path: &Path, config: &serde_json::Value) -> Result<(), String> {
    use std::io::Write;
    use std::os::unix::fs::OpenOptionsExt;

    let parent = path
        .parent()
        .ok_or_else(|| "native config has no parent directory".to_string())?;
    let temporary = parent.join(format!("config.json.dashboard.{}.tmp", std::process::id()));
    let bytes = serde_json::to_vec_pretty(config)
        .map_err(|e| format!("failed to serialize native config: {e}"))?;
    let mut file = std::fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(&temporary)
        .map_err(|e| format!("failed to create temporary native config: {e}"))?;
    file.write_all(&bytes)
        .and_then(|_| file.write_all(b"\n"))
        .and_then(|_| file.sync_all())
        .map_err(|e| format!("failed to write temporary native config: {e}"))?;
    std::fs::rename(&temporary, path).map_err(|e| format!("failed to replace native config: {e}"))
}

fn normalize_manager_path(value: &str) -> Result<String, String> {
    let mut entries = Vec::new();
    for entry in value
        .split(':')
        .map(str::trim)
        .filter(|entry| !entry.is_empty())
    {
        if !entry.starts_with('/') {
            return Err(format!("PATH entry must be absolute: {entry}"));
        }
        if !entries.contains(&entry) {
            entries.push(entry);
        }
    }
    if entries.is_empty() {
        return Err("PATH must contain at least one absolute directory".to_string());
    }
    Ok(entries.join(":"))
}

fn check_manager_command(command: &str, path: &str) -> CommandCheck {
    let executable = std::env::split_paths(&std::ffi::OsString::from(path))
        .map(|directory| directory.join(command))
        .find(|candidate| is_executable(candidate));
    let Some(executable) = executable else {
        return CommandCheck {
            command: command.to_string(),
            required: true,
            found: false,
            path: String::new(),
            version: String::new(),
            message: format!("{command} was not found in the manager PATH"),
        };
    };
    let output = Command::new(&executable).arg("--version").output();
    let (version, message) = match output {
        Ok(output) => {
            let text = if output.stdout.is_empty() {
                &output.stderr
            } else {
                &output.stdout
            };
            let version = String::from_utf8_lossy(text)
                .lines()
                .next()
                .unwrap_or("")
                .trim()
                .to_string();
            let message = if output.status.success() {
                String::new()
            } else {
                "command exists but its version check failed".to_string()
            };
            (version, message)
        }
        Err(error) => (String::new(), format!("could not run command: {error}")),
    };
    CommandCheck {
        command: command.to_string(),
        required: true,
        found: true,
        path: executable.to_string_lossy().into_owned(),
        version,
        message,
    }
}

/// Stop the LaunchAgent and remove local configuration and data.
/// The parent registration is deliberately retained so reset needs no API key.
#[tauri::command]
pub async fn native_reset(app: AppHandle, request: ResetRequest) -> Result<CommandResult, String> {
    let args = uninstall_args(request.force, &request.instance);
    let (ok, message) = run_binary_plain(&app, args).await?;
    Ok(CommandResult { ok, message })
}

fn uninstall_args(force: bool, instance: &str) -> Vec<String> {
    let mut args = vec![
        "native".into(),
        "uninstall".into(),
        "--keep-registration".into(),
    ];
    if !instance.is_empty() && instance != "default" {
        args.extend(["--instance".into(), instance.into()]);
    }
    if force {
        args.push("--force".into());
    }
    args
}

fn is_http_url(value: &str) -> bool {
    value.starts_with("https://") || value.starts_with("http://")
}

fn setup_agent_tools_with_mise(
    node_version: Option<&str>,
    github_cli_version: Option<&str>,
) -> Result<PathBuf, String> {
    let mise = find_mise().ok_or_else(|| {
        "mise was not found. Install mise first (https://mise.jdx.dev), then retry.".to_string()
    })?;
    let mut tools = Vec::new();
    if let Some(version) = node_version {
        if version.is_empty() {
            return Err("Node.js version is required when mise setup is enabled".to_string());
        }
        tools.push(format!("node@{version}"));
    }
    if let Some(version) = github_cli_version {
        if version.is_empty() {
            return Err("GitHub CLI version is required when mise setup is enabled".to_string());
        }
        tools.push(format!("github-cli@{version}"));
    }
    let mut args = vec!["use", "--global"];
    args.extend(tools.iter().map(String::as_str));
    let output = Command::new(&mise)
        .args(args)
        .output()
        .map_err(|e| format!("failed to run mise: {e}"))?;
    if !output.status.success() {
        let detail = String::from_utf8_lossy(if output.stderr.is_empty() {
            &output.stdout
        } else {
            &output.stderr
        });
        return Err(format!(
            "mise could not set up agent tools: {}",
            detail.trim()
        ));
    }
    Ok(mise)
}

fn find_mise() -> Option<PathBuf> {
    let home = std::env::var_os("HOME").map(PathBuf::from);
    let mut candidates = Vec::new();
    if let Some(path) = std::env::var_os("PATH") {
        candidates.extend(std::env::split_paths(&path).map(|dir| dir.join("mise")));
    }
    if let Some(home) = home {
        candidates.push(home.join(".local/bin/mise"));
    }
    candidates.extend([
        PathBuf::from("/opt/homebrew/bin/mise"),
        PathBuf::from("/usr/local/bin/mise"),
    ]);
    candidates.into_iter().find(|path| is_executable(path))
}

fn mise_tool_bin(mise: &Path, tool: &str) -> Result<PathBuf, String> {
    let output = Command::new(mise)
        .args(["which", tool])
        .output()
        .map_err(|e| format!("failed to resolve {tool} with mise: {e}"))?;
    if !output.status.success() {
        let detail = String::from_utf8_lossy(if output.stderr.is_empty() {
            &output.stdout
        } else {
            &output.stderr
        });
        return Err(format!(
            "mise could not resolve the {tool} executable: {}",
            detail.trim()
        ));
    }
    let executable = PathBuf::from(String::from_utf8_lossy(&output.stdout).trim());
    executable
        .parent()
        .filter(|path| !path.as_os_str().is_empty())
        .map(Path::to_path_buf)
        .ok_or_else(|| format!("mise returned an invalid {tool} executable path"))
}

fn mise_manager_path(mise: &Path, node: bool, github_cli: bool) -> Result<String, String> {
    let mut tool_bins = Vec::new();
    if node {
        tool_bins.push(mise_tool_bin(mise, "node")?);
    }
    if github_cli {
        tool_bins.push(mise_tool_bin(mise, "gh")?);
    }
    mise_manager_path_with_tool_bins(mise, &tool_bins)
}

fn mise_manager_path_with_tool_bins(mise: &Path, tool_bins: &[PathBuf]) -> Result<String, String> {
    let home = std::env::var_os("HOME")
        .map(PathBuf::from)
        .ok_or("HOME is not set")?;
    let data_dir = std::env::var_os("MISE_DATA_DIR")
        .map(PathBuf::from)
        .unwrap_or_else(|| {
            std::env::var_os("XDG_DATA_HOME")
                .map(|base| PathBuf::from(base).join("mise"))
                .unwrap_or_else(|| home.join(".local/share/mise"))
        });
    // Native sessions use an isolated HOME. A mise shim would therefore resolve
    // configuration and installs relative to the session instead of the host.
    // Put each selected tool's concrete bin directory first so it remains usable
    // without exposing the host HOME to the session.
    let mut entries = tool_bins.to_vec();
    entries.push(data_dir.join("shims"));
    if let Some(parent) = mise.parent() {
        entries.push(parent.to_path_buf());
    }
    if let Some(current) = std::env::var_os("PATH") {
        entries.extend(std::env::split_paths(&current));
    } else {
        entries.extend([
            PathBuf::from("/usr/local/bin"),
            PathBuf::from("/usr/bin"),
            PathBuf::from("/bin"),
            PathBuf::from("/usr/sbin"),
            PathBuf::from("/sbin"),
        ]);
    }
    entries.dedup();
    std::env::join_paths(entries)
        .map(|path| path.to_string_lossy().into_owned())
        .map_err(|e| format!("failed to build mise PATH: {e}"))
}

fn is_executable(path: &Path) -> bool {
    use std::os::unix::fs::PermissionsExt;
    std::fs::metadata(path)
        .map(|meta| meta.is_file() && meta.permissions().mode() & 0o111 != 0)
        .unwrap_or(false)
}

#[cfg(test)]
mod tests {
    use super::{
        check_manager_command, is_http_url, mise_manager_path_with_tool_bins, native_config_path,
        normalize_manager_path, parse_json, uninstall_args, update_args,
    };
    use crate::types::{DoctorResult, NativeStatus};

    #[test]
    fn parses_status_contract() {
        let json = br#"{
          "service":"running","manager_id":"manager-1","upstream":"https://parent.example",
          "labels":{"os":"darwin"},"version":"dev",
          "filesystem_sandbox":true,"active_sessions":2,"health":"ok","state":"/tmp/state"
        }"#;
        let status = parse_json::<NativeStatus>(json, "status").expect("status JSON");
        assert_eq!(status.manager_id, "manager-1");
        assert_eq!(status.active_sessions, 2);
    }

    #[test]
    fn parses_doctor_checks() {
        let json = br#"{"ok":false,"checks":[{"id":"local_health","status":"error","message":"unreachable"}]}"#;
        let doctor = parse_json::<DoctorResult>(json, "doctor").expect("doctor JSON");
        assert!(!doctor.ok);
        assert_eq!(doctor.checks[0].id, "local_health");
    }

    #[test]
    fn rejects_empty_json() {
        assert!(parse_json::<NativeStatus>(b"  \n", "status").is_err());
    }

    #[test]
    fn accepts_only_http_install_urls() {
        assert!(is_http_url("https://parent.example"));
        assert!(is_http_url("http://10.0.0.10:8080"));
        assert!(!is_http_url("file:///tmp/socket"));
    }

    #[test]
    fn native_config_uses_the_macos_application_support_directory() {
        let path = native_config_path(None).expect("HOME should be set during tests");
        assert!(path.ends_with("Library/Application Support/agentapi-native/config.json"));
        let named = native_config_path(Some("ios")).expect("HOME should be set during tests");
        assert!(named.ends_with("Library/Application Support/agentapi-native-ios/config.json"));
    }

    #[test]
    fn reset_only_forces_session_termination_when_requested() {
        assert!(!uninstall_args(false, "default").contains(&"--force".to_string()));
        assert!(uninstall_args(true, "ios").contains(&"--force".to_string()));
        assert!(uninstall_args(false, "default").contains(&"--keep-registration".to_string()));
        assert!(uninstall_args(false, "ios")
            .windows(2)
            .any(|args| args == ["--instance", "ios"]));
    }

    #[test]
    fn update_selects_the_requested_instance() {
        assert_eq!(update_args(None), ["native", "update"]);
        assert_eq!(update_args(Some("default")), ["native", "update"]);
        assert_eq!(
            update_args(Some("ios")),
            ["native", "update", "--instance", "ios"]
        );
    }

    #[test]
    fn mise_path_starts_with_node_bin_then_shims_and_mise_binary_directory() {
        let path = mise_manager_path_with_tool_bins(
            std::path::Path::new("/opt/homebrew/bin/mise"),
            &[
                std::path::PathBuf::from("/Users/test/.local/share/mise/installs/node/24.18.1/bin"),
                std::path::PathBuf::from(
                    "/Users/test/.local/share/mise/installs/github-cli/2.97.0/bin",
                ),
            ],
        )
        .expect("mise PATH");
        let entries = std::env::split_paths(&std::ffi::OsString::from(path)).collect::<Vec<_>>();
        assert_eq!(
            entries[0],
            std::path::PathBuf::from("/Users/test/.local/share/mise/installs/node/24.18.1/bin")
        );
        assert_eq!(
            entries[1],
            std::path::PathBuf::from(
                "/Users/test/.local/share/mise/installs/github-cli/2.97.0/bin"
            )
        );
        assert!(entries[2].ends_with(".local/share/mise/shims"));
        assert_eq!(entries[3], std::path::PathBuf::from("/opt/homebrew/bin"));
    }

    #[test]
    fn manager_path_requires_absolute_entries_and_removes_duplicates() {
        assert_eq!(
            normalize_manager_path(" /opt/homebrew/bin:/usr/bin:/opt/homebrew/bin ").unwrap(),
            "/opt/homebrew/bin:/usr/bin"
        );
        assert!(normalize_manager_path("relative/bin:/usr/bin").is_err());
        assert!(normalize_manager_path("::").is_err());
    }

    #[test]
    fn command_check_uses_only_the_configured_manager_path() {
        let found = check_manager_command("sh", "/bin");
        assert!(found.found);
        assert_eq!(found.path, "/bin/sh");

        let missing = check_manager_command("sh", "/directory/that/does/not/exist");
        assert!(!missing.found);
        assert!(missing.message.contains("manager PATH"));
    }
}
