import { useCallback, useEffect, useMemo, useState } from "react";
import {
  fetchDashboard,
  fetchInstances,
  onRefreshRequest,
  restartDaemon,
  showDashboard,
  updateDaemon,
} from "./api";
import type { DashboardData, NativeInstance } from "./types";
import { StatusCard } from "./components/StatusCard";
import { SessionsCard } from "./components/SessionsCard";
import { DoctorCard } from "./components/DoctorCard";
import { SetupCard } from "./components/SetupCard";
import { ResetCard } from "./components/ResetCard";
import { LogsCard } from "./components/LogsCard";
import { EnvironmentCard } from "./components/EnvironmentCard";

type LoadState = "loading" | "setup" | "ready" | "error";

export function App() {
  const [data, setData] = useState<DashboardData | null>(null);
  const [state, setState] = useState<LoadState>("loading");
  const [error, setError] = useState<string>("");
  const [warnings, setWarnings] = useState<string[]>([]);
  const [restarting, setRestarting] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [instances, setInstances] = useState<NativeInstance[]>([]);
  const [selectedInstance, setSelectedInstance] = useState("default");
  const [adding, setAdding] = useState(false);

  const refresh = useCallback(async (includeDoctor = true) => {
    setState((prev) => (prev === "ready" ? "ready" : "loading"));
    try {
      const available = await fetchInstances();
      setInstances(available);
      if (available.length === 0) {
        setData(null);
        setWarnings([]);
        setError("");
        setState("setup");
        return;
      }
      const target = available.some((item) => item.instance === selectedInstance)
        ? selectedInstance
        : available[0].instance;
      if (target !== selectedInstance) setSelectedInstance(target);
      const next = await fetchDashboard(target, includeDoctor);
      setData((current) => includeDoctor ? next : { ...next, doctor: current?.doctor ?? null });
      setWarnings(next.warnings ?? []);
      setState("ready");
      setLastUpdated(new Date());
    } catch (err) {
      setError(String(err));
      setState("error");
    }
  }, [selectedInstance]);

  // Initial load.
  useEffect(() => {
    void refresh(true);
  }, [refresh]);

  // Refresh when the tray menu requests it.
  useEffect(() => {
    let unlisten: (() => void) | undefined;
    onRefreshRequest(() => void refresh(true)).then((fn) => {
      unlisten = fn;
    }).catch(() => {
      /* events unavailable outside Tauri; safe to ignore in dev */
    });
    return () => unlisten?.();
  }, [refresh]);

  // Optional auto-refresh every 15s while the window is visible.
  useEffect(() => {
    if (!autoRefresh) return;
    const id = window.setInterval(() => {
      if (document.visibilityState === "visible") void refresh(false);
    }, 15_000);
    return () => window.clearInterval(id);
  }, [autoRefresh, refresh]);

  const handleRestart = useCallback(async () => {
    setRestarting(true);
    try {
      const result = await restartDaemon(selectedInstance);
      if (!result.ok) setWarnings([result.message]);
      await refresh(true);
    } catch (err) {
      setWarnings([`restart failed: ${String(err)}`]);
    } finally {
      setRestarting(false);
    }
  }, [refresh, selectedInstance]);

  const activeCount = useMemo(
    () => data?.status?.active_sessions ?? data?.sessions.length ?? 0,
    [data],
  );

  const handleUpdate = useCallback(async () => {
    setUpdating(true);
    try {
      const result = await updateDaemon(selectedInstance);
      if (!result.ok) setWarnings([result.message]);
      await refresh(true);
    } catch (err) {
      setWarnings([`update failed: ${String(err)}`]);
    } finally {
      setUpdating(false);
    }
  }, [refresh, selectedInstance]);

  return (
    <div className="app">
      <header className="app__header">
        <div className="app__title">
          <h1>agentapi-proxy Native</h1>
          <span className="app__subtitle">External Session Manager</span>
        </div>
        <div className="app__actions">
          {instances.length > 0 && (
            <select value={selectedInstance} onChange={(event) => setSelectedInstance(event.target.value)}>
              {instances.map((item) => <option key={item.instance} value={item.instance}>{item.instance}</option>)}
            </select>
          )}
          <button className="btn btn--ghost" onClick={() => setAdding((value) => !value)}>
            {adding ? "Cancel add" : "Add instance"}
          </button>
          <button
            className="btn btn--ghost"
            onClick={() => void refresh(true)}
            disabled={state === "loading" || restarting}
          >
            {state === "loading" ? "Refreshing…" : "Refresh"}
          </button>
          <button
            className="btn btn--ghost"
            onClick={() => void handleUpdate()}
            disabled={updating || restarting || activeCount > 0}
            title={activeCount > 0 ? "Finish active sessions before updating" : "Replace the native manager with the binary bundled in this app"}
          >
            {updating ? "Updating…" : "Update manager"}
          </button>
          <button
            className="btn btn--danger"
            onClick={() => void handleRestart()}
            disabled={restarting || updating}
            title="Restart the native ESM daemon"
          >
            {restarting ? "Restarting…" : "Restart"}
          </button>
        </div>
      </header>

      <p className="app__meta">
        <span className={`pill pill--${healthPillClass(data?.status?.health)}`}>
          {data?.status?.health ?? "—"}
        </span>
        <span className="pill pill--neutral">{activeCount} active session{activeCount === 1 ? "" : "s"}</span>
        <label className="app__autorefresh">
          <input
            type="checkbox"
            checked={autoRefresh}
            onChange={(e) => setAutoRefresh(e.target.checked)}
          />
          Auto-refresh
        </label>
        {lastUpdated && (
          <span className="app__updated">Updated {lastUpdated.toLocaleTimeString()}</span>
        )}
      </p>

      {warnings.length > 0 && (
        <div className="app__warnings">
          {warnings.map((w) => (
            <p key={w}>{w}</p>
          ))}
        </div>
      )}

      <main className="app__body">
        {state === "loading" && <LoadingState />}
        {(state === "setup" || adding) && <SetupCard initialInstance={instances.length === 0 ? "default" : ""} onInstalled={async (instance) => { setSelectedInstance(instance); setAdding(false); await refresh(true); }} />}
        {state === "error" && (
          <>
            <ErrorState message={error} onRetry={() => void refresh(true)} />
          </>
        )}
        {state === "ready" && data && !adding && (
          <>
            {data.status ? <StatusCard status={data.status} /> : <EmptyState label="No status available" />}
            <SessionsCard sessions={data.sessions} />
            <LogsCard instance={selectedInstance} sessions={data.sessions} />
            <EnvironmentCard instance={selectedInstance} activeSessions={activeCount} onSaved={() => refresh(true)} />
            {data.doctor ? <DoctorCard doctor={data.doctor} /> : <EmptyState label="Doctor result unavailable" />}
            <ResetCard instance={selectedInstance} activeSessions={activeCount} onReset={() => refresh(true)} />
          </>
        )}
      </main>

      <footer className="app__footer">
        <button className="link" onClick={() => void showDashboard().catch(() => {})}>
          Keep window available in the menu bar
        </button>
        <span>Closing this window hides it; the daemon keeps running.</span>
      </footer>
    </div>
  );
}

function healthPillClass(health?: string): "ok" | "warn" | "bad" | "neutral" {
  switch (health) {
    case "ok":
      return "ok";
    case "unreachable":
      return "bad";
    default:
      return health ? "warn" : "neutral";
  }
}

function LoadingState() {
  return (
    <div className="state state--loading">
      <div className="spinner" />
      <p>Querying the native daemon…</p>
    </div>
  );
}

function ErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="state state--error">
      <h2>Could not reach the native ESM</h2>
      <p className="state__detail">{message}</p>
      <p className="state__hint">
        If this Mac is already configured, verify that its LaunchAgent is running and retry.
      </p>
      <button className="btn" onClick={onRetry}>Try again</button>
    </div>
  );
}

function EmptyState({ label }: { label: string }) {
  return (
    <div className="state state--empty">
      <p>{label}</p>
    </div>
  );
}
