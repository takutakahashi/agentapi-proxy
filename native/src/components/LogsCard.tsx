import { useCallback, useEffect, useRef, useState } from "react";
import { fetchLogs } from "../api";
import type { NativeSession } from "../types";

interface Props {
  instance: string;
  sessions: NativeSession[];
}

const MANAGER = "__manager__";

export function LogsCard({ instance, sessions }: Props) {
  const [target, setTarget] = useState(MANAGER);
  const [content, setContent] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [live, setLive] = useState(true);
  const output = useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (target !== MANAGER && !sessions.some((session) => session.id === target)) {
      setTarget(MANAGER);
    }
  }, [sessions, target]);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const next = await fetchLogs(
        instance,
        target === MANAGER ? { daemon: true } : { daemon: false, sessionId: target },
      );
      setContent(next);
      setError("");
      requestAnimationFrame(() => {
        if (output.current) output.current.scrollTop = output.current.scrollHeight;
      });
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  }, [instance, target]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    if (!live) return;
    const id = window.setInterval(() => {
      if (document.visibilityState === "visible") void refresh();
    }, 3_000);
    return () => window.clearInterval(id);
  }, [live, refresh]);

  return (
    <section className="card logs">
      <header className="card__header logs__header">
        <h2>Logs</h2>
        <div className="logs__controls">
          <select
            aria-label="Log source"
            value={target}
            onChange={(event) => setTarget(event.target.value)}
          >
            <option value={MANAGER}>Session manager</option>
            {sessions.map((session) => (
              <option key={session.id} value={session.id}>
                Session {shortId(session.id)} (provisioner + agent)
              </option>
            ))}
          </select>
          <label className="logs__live">
            <input type="checkbox" checked={live} onChange={(event) => setLive(event.target.checked)} />
            Live
          </label>
          <button className="btn btn--ghost" onClick={() => void refresh()} disabled={loading}>
            {loading ? "Loading…" : "Refresh"}
          </button>
        </div>
      </header>
      <p className="logs__hint">
        Showing the latest 500 lines. Session output combines provisioner and agent stdout/stderr.
      </p>
      {error ? (
        <div className="state state--error state--inline">
          <p className="state__detail">{error}</p>
        </div>
      ) : (
        <pre ref={output} className="logs__output">{content || "No log output yet."}</pre>
      )}
    </section>
  );
}

function shortId(id: string): string {
  return id.length > 20 ? `${id.slice(0, 10)}…${id.slice(-6)}` : id;
}
