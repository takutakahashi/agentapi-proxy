import type { NativeSession } from "../types";

interface Props {
  sessions: NativeSession[];
}

export function SessionsCard({ sessions }: Props) {
  return (
    <section className="card">
      <header className="card__header">
        <h2>Sessions</h2>
        <span className="pill pill--neutral">{sessions.length}</span>
      </header>
      {sessions.length === 0 ? (
        <div className="state state--empty state--inline">
          <p>No active sessions on this host.</p>
        </div>
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Status</th>
              <th>PID</th>
              <th>Started</th>
            </tr>
          </thead>
          <tbody>
            {sessions.map((session) => (
              <tr key={session.id}>
                <td className="mono" title={session.id}>{shortId(session.id)}</td>
                <td>
                  <span className={`pill pill--${statusPill(session.status)}`}>
                    {session.status || "—"}
                  </span>
                </td>
                <td className="mono">{session.pid || "—"}</td>
                <td>{formatStarted(session.started_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

function shortId(id: string): string {
  return id.length > 16 ? `${id.slice(0, 8)}…${id.slice(-4)}` : id;
}

function statusPill(status: string): "ok" | "warn" | "bad" | "neutral" {
  switch (status) {
    case "running":
      return "ok";
    case "exited":
    case "failed":
      return "bad";
    case "starting":
      return "warn";
    default:
      return "neutral";
  }
}

function formatStarted(startedAt: string): string {
  if (!startedAt) return "—";
  const date = new Date(startedAt);
  if (Number.isNaN(date.getTime())) return startedAt;
  return date.toLocaleString();
}