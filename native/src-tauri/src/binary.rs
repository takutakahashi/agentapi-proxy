use std::path::PathBuf;
use std::process::Command;

use tauri::AppHandle;
use tauri_plugin_shell::ShellExt;

/// Environment variable that overrides the ccplant binary name or path.
pub const BINARY_ENV: &str = "CCPLANT_BINARY_PATH";

/// The executable name looked up on `PATH` when the env override is unset.
pub const BINARY_NAME: &str = "ccplant";

/// The `native` subcommand used by every dashboard query.
const NATIVE_SUBCOMMAND: &str = "native";

/// Resolve the ccplant binary path.
///
/// Order of precedence:
/// 1. `CCPLANT_BINARY_PATH` (path to an executable file).
/// 2. The binary managed by `native install` on macOS.
/// 3. `ccplant` looked up on `PATH`.
///
/// Returns a descriptive error so the UI can render guidance.
pub fn resolve_binary() -> Result<PathBuf, String> {
    if let Ok(raw) = std::env::var(BINARY_ENV) {
        let trimmed = raw.trim();
        if trimmed.is_empty() {
            return Err(format!("{BINARY_ENV} is set but empty"));
        }
        let path = PathBuf::from(trimmed);
        if !is_executable(&path) {
            return Err(format!(
                "{BINARY_ENV} must point to an executable file; got {:?}",
                path.display()
            ));
        }
        return Ok(path);
    }

    if let Some(home) = std::env::var_os("HOME") {
        let application_support = PathBuf::from(home)
            .join("Library")
            .join("Application Support");
        let managed = application_support
            .join("agentapi-native")
            .join("bin")
            .join(BINARY_NAME);
        if is_executable(&managed) {
            return Ok(managed);
        }
        if let Ok(entries) = std::fs::read_dir(application_support) {
            for entry in entries.flatten() {
                let name = entry.file_name();
                if !name.to_string_lossy().starts_with("agentapi-native-") {
                    continue;
                }
                let candidate = entry.path().join("bin").join(BINARY_NAME);
                if is_executable(&candidate) {
                    return Ok(candidate);
                }
            }
        }
    }

    find_on_path(BINARY_NAME).ok_or_else(|| {
        format!("could not find `{BINARY_NAME}` on PATH; set {BINARY_ENV} to the proxy binary")
    })
}

/// Minimal `which` implementation: search `PATH` for an executable matching
/// `name`. Avoids pulling in the `which` crate for a single lookup.
fn find_on_path(name: &str) -> Option<PathBuf> {
    // An absolute or relative path is used as-is if it exists.
    let candidate = PathBuf::from(name);
    if candidate.is_absolute() || candidate.components().count() > 1 {
        if candidate.is_file() {
            return Some(candidate);
        }
        return None;
    }

    let path_env = std::env::var_os("PATH")?;
    for dir in std::env::split_paths(&path_env) {
        let full = dir.join(name);
        if is_executable(&full) {
            return Some(full);
        }
    }
    None
}

fn is_executable(path: &std::path::Path) -> bool {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let meta = match std::fs::metadata(path) {
            Ok(m) => m,
            Err(_) => return false,
        };
        if !meta.is_file() {
            return false;
        }
        meta.permissions().mode() & 0o111 != 0
    }
    #[cfg(not(unix))]
    {
        path.is_file()
    }
}

/// Build a `Command` pre-loaded with `<binary> native <sub>` and the JSON flag.
fn native_command(sub: &str, json: bool) -> Result<Command, String> {
    let binary = resolve_binary()?;
    let mut cmd = Command::new(binary);
    cmd.arg(NATIVE_SUBCOMMAND).arg(sub);
    if json {
        cmd.arg("--json");
    }
    Ok(cmd)
}

/// Run a `native <sub> --json` subcommand and return its stdout as bytes.
/// Captures stderr; on non-zero exit the stderr is returned as the error.
pub async fn run_native_json(
    app: &AppHandle,
    sub: &str,
    instance: Option<&str>,
) -> Result<Vec<u8>, String> {
    let mut args = vec![NATIVE_SUBCOMMAND, sub, "--json"];
    if let Some(name) = instance.filter(|name| !name.is_empty() && *name != "default") {
        args.extend(["--instance", name]);
    }
    if std::env::var_os(BINARY_ENV).is_none() {
        if let Ok(sidecar) = app.shell().sidecar(BINARY_NAME) {
            let output = sidecar
                .args(args.clone())
                .output()
                .await
                .map_err(|e| format!("failed to execute bundled binary: {e}"))?;
            if output.status.success() {
                return Ok(output.stdout);
            }
            return Err(command_error(sub, &output.stdout, &output.stderr));
        }
    }
    let mut cmd = native_command(sub, true)?;
    if let Some(name) = instance.filter(|name| !name.is_empty() && *name != "default") {
        cmd.arg("--instance").arg(name);
    }
    let output = cmd
        .output()
        .map_err(|e| format!("failed to execute: {e}"))?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
        let detail = if !stderr.is_empty() {
            stderr
        } else if !stdout.is_empty() {
            stdout
        } else {
            format!("exit code {}", output.status.code().unwrap_or(-1))
        };
        return Err(format!("`native {sub} --json` failed: {detail}"));
    }
    Ok(output.stdout)
}

/// Run `native <sub>` without JSON and return (ok, combined-message).
pub async fn run_native_plain(
    app: &AppHandle,
    sub: &str,
    instance: Option<&str>,
) -> (bool, String) {
    let mut args = vec![NATIVE_SUBCOMMAND, sub];
    if let Some(name) = instance.filter(|name| !name.is_empty() && *name != "default") {
        args.extend(["--instance", name]);
    }
    if std::env::var_os(BINARY_ENV).is_none() {
        if let Ok(sidecar) = app.shell().sidecar(BINARY_NAME) {
            match sidecar.args(args).output().await {
                Ok(output) => {
                    let message = output_message(&output.stdout, &output.stderr);
                    return (output.status.success(), message);
                }
                Err(error) => return (false, format!("failed to execute bundled binary: {error}")),
            }
        }
    }
    match native_command(sub, false) {
        Ok(mut cmd) => {
            if let Some(name) = instance.filter(|name| !name.is_empty() && *name != "default") {
                cmd.arg("--instance").arg(name);
            }
            match cmd.output() {
                Ok(output) => {
                    let msg = {
                        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
                        let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
                        if !stderr.is_empty() {
                            stderr
                        } else if !stdout.is_empty() {
                            stdout
                        } else {
                            String::new()
                        }
                    };
                    (output.status.success(), msg)
                }
                Err(e) => (false, format!("failed to execute: {e}")),
            }
        }
        Err(e) => (false, e),
    }
}

/// Run an arbitrary set of arguments against the configured proxy binary.
/// The bundled sidecar remains the default; setting `CCPLANT_BINARY_PATH`
/// makes install/update/reset use the same override as dashboard queries.
pub async fn run_binary_plain(
    app: &AppHandle,
    args: Vec<String>,
) -> Result<(bool, String), String> {
    if std::env::var_os(BINARY_ENV).is_none() {
        let output = app
            .shell()
            .sidecar(BINARY_NAME)
            .map_err(|e| format!("bundled {BINARY_NAME} is unavailable: {e}"))?
            .args(args)
            .output()
            .await
            .map_err(|e| format!("failed to execute bundled binary: {e}"))?;
        return Ok((
            output.status.success(),
            output_message(&output.stdout, &output.stderr),
        ));
    }

    let output = Command::new(resolve_binary()?)
        .args(args)
        .output()
        .map_err(|e| format!("failed to execute configured binary: {e}"))?;
    Ok((
        output.status.success(),
        output_message(&output.stdout, &output.stderr),
    ))
}

/// Run `native logs` with a bounded tail and return stdout.
pub async fn run_native_logs(
    app: &AppHandle,
    instance: Option<&str>,
    session_id: Option<&str>,
    daemon: bool,
    tail: usize,
) -> Result<String, String> {
    let tail_value = tail.to_string();
    let mut args = vec![NATIVE_SUBCOMMAND, "logs", "--tail", tail_value.as_str()];
    if daemon {
        args.push("--daemon");
    } else if let Some(id) = session_id {
        args.push(id);
    }
    if let Some(name) = instance.filter(|name| !name.is_empty() && *name != "default") {
        args.extend(["--instance", name]);
    }
    if std::env::var_os(BINARY_ENV).is_none() {
        if let Ok(sidecar) = app.shell().sidecar(BINARY_NAME) {
            let output = sidecar
                .args(args.clone())
                .output()
                .await
                .map_err(|e| format!("failed to execute bundled binary: {e}"))?;
            if output.status.success() {
                return Ok(String::from_utf8_lossy(&output.stdout).to_string());
            }
            return Err(command_error("logs", &output.stdout, &output.stderr));
        }
    }
    let mut cmd = native_command("logs", false)?;
    cmd.arg("--tail").arg(&tail_value);
    if daemon {
        cmd.arg("--daemon");
    } else if let Some(id) = session_id {
        cmd.arg(id);
    }
    if let Some(name) = instance.filter(|name| !name.is_empty() && *name != "default") {
        cmd.arg("--instance").arg(name);
    }
    let output = cmd
        .output()
        .map_err(|e| format!("failed to execute: {e}"))?;
    if !output.status.success() {
        return Err(command_error("logs", &output.stdout, &output.stderr));
    }
    Ok(String::from_utf8_lossy(&output.stdout).to_string())
}

fn output_message(stdout: &[u8], stderr: &[u8]) -> String {
    let stderr = String::from_utf8_lossy(stderr).trim().to_string();
    let stdout = String::from_utf8_lossy(stdout).trim().to_string();
    if !stderr.is_empty() {
        stderr
    } else {
        stdout
    }
}

fn command_error(sub: &str, stdout: &[u8], stderr: &[u8]) -> String {
    let detail = output_message(stdout, stderr);
    if detail.is_empty() {
        format!("`native {sub}` failed")
    } else {
        format!("`native {sub}` failed: {detail}")
    }
}
