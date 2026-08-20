import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import type {
  DashboardData,
  DoctorResult,
  InstallRequest,
  NativeSession,
  NativeStatus,
  NativeInstance,
  ManagerEnvironment,
  ResetRequest,
  RestartResult,
} from "./types";

// Thin wrapper around `invoke` so the rest of the UI never touches the IPC
// layer directly. All Tauri command names live here.

export async function fetchInstances(): Promise<NativeInstance[]> {
  return invoke<NativeInstance[]>("native_list");
}

export async function fetchStatus(instance: string): Promise<NativeStatus> {
  return invoke<NativeStatus>("native_status", { instance });
}

export async function fetchSessions(instance: string): Promise<NativeSession[]> {
  return invoke<NativeSession[]>("native_sessions", { instance });
}

export async function fetchLogs(
  instance: string,
  target: { daemon: true } | { daemon: false; sessionId: string },
  tail = 500,
): Promise<string> {
  return invoke<string>("native_logs", {
    instance,
    daemon: target.daemon,
    sessionId: target.daemon ? null : target.sessionId,
    tail,
  });
}

export async function fetchDoctor(instance: string): Promise<DoctorResult> {
  return invoke<DoctorResult>("native_doctor", { instance });
}

export async function restartDaemon(instance: string): Promise<RestartResult> {
  return invoke<RestartResult>("native_restart", { instance });
}

export async function updateDaemon(instance: string): Promise<RestartResult> {
  return invoke<RestartResult>("native_update", { instance });
}

export async function fetchEnvironment(instance: string): Promise<ManagerEnvironment> {
  return invoke<ManagerEnvironment>("native_environment", { instance });
}

export async function updateEnvironment(instance: string, path: string): Promise<RestartResult> {
  return invoke<RestartResult>("native_update_environment", {
    request: { instance, path },
  });
}

export async function installDaemon(request: InstallRequest): Promise<RestartResult> {
  return invoke<RestartResult>("native_install", { request });
}

export async function resetDaemon(request: ResetRequest): Promise<RestartResult> {
  return invoke<RestartResult>("native_reset", { request });
}

export async function showDashboard(): Promise<void> {
  await invoke("show_dashboard");
}

/** Load every dashboard panel in parallel. Partial failures are surfaced. */
export async function fetchDashboard(instance: string, includeDoctor = true): Promise<DashboardData> {
  const [status, sessions, doctor] = await Promise.allSettled([
    fetchStatus(instance),
    fetchSessions(instance),
    includeDoctor ? fetchDoctor(instance) : Promise.resolve(null),
  ]);

  const data: DashboardData = {
    status: status.status === "fulfilled" ? status.value : null,
    sessions: sessions.status === "fulfilled" ? sessions.value : [],
    doctor: doctor.status === "fulfilled" ? doctor.value : null,
  };

  // If everything rejected, throw the first error so the caller can render
  // a global error state. Otherwise return partial data plus the aggregated
  // error message via a thrown property for non-fatal warnings.
  const errors: string[] = [];
  for (const [result, name] of [
    [status, "status"],
    [sessions, "sessions"],
    [doctor, "doctor"],
  ] as const) {
    if (result.status === "rejected") {
      errors.push(`${name}: ${String(result.reason)}`);
    }
  }

  if (data.status === null && data.sessions.length === 0 && data.doctor === null) {
    throw new Error(errors.join("; ") || "All panels failed to load");
  }
  data.warnings = errors;
  return data;
}

/** Subscribe to tray-driven refresh events. Returns an unsubscribe function. */
export async function onRefreshRequest(handler: () => void): Promise<UnlistenFn> {
  return listen("dashboard://refresh", () => handler());
}
