import { useCallback, useEffect, useState } from "react";
import { fetchEnvironment, updateEnvironment } from "../api";
import type { ManagerEnvironment } from "../types";

interface Props {
  instance: string;
  activeSessions: number;
  onSaved: () => Promise<void>;
}

export function EnvironmentCard({ instance, activeSessions, onSaved }: Props) {
  const [environment, setEnvironment] = useState<ManagerEnvironment | null>(null);
  const [path, setPath] = useState("");
  const [newEntry, setNewEntry] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setMessage("");
    try {
      const next = await fetchEnvironment(instance);
      setEnvironment(next);
      setPath(next.path);
    } catch (error) {
      setMessage(String(error));
    } finally {
      setLoading(false);
    }
  }, [instance]);

  useEffect(() => { void load(); }, [load]);

  const save = useCallback(async () => {
    setSaving(true);
    setMessage("");
    try {
      const result = await updateEnvironment(instance, path);
      setMessage(result.message);
      await load();
      await onSaved();
    } catch (error) {
      setMessage(String(error));
    } finally {
      setSaving(false);
    }
  }, [instance, load, onSaved, path]);

  const missing = environment?.commands.filter((command) => !command.found).length ?? 0;

  const addEntry = useCallback(() => {
    const entry = newEntry.trim().replace(/\/+$/, "");
    if (!entry) return;
    const entries = path.split(":").map((value) => value.trim()).filter(Boolean);
    if (!entries.includes(entry)) setPath([entry, ...entries].join(":"));
    setNewEntry("");
  }, [newEntry, path]);

  return (
    <section className="card">
      <header className="card__header">
        <h2>Manager environment</h2>
        <span className={`pill pill--${missing === 0 ? "ok" : "bad"}`}>
          {loading ? "checking" : missing === 0 ? "ready" : `${missing} missing`}
        </span>
      </header>
      <p className="card__hint">This PATH is inherited by the daemon and every native agent session.</p>
      <label className="environment__field">
        <span>PATH</span>
        <textarea className="environment__path mono" value={path} onChange={(event) => setPath(event.target.value)} rows={4} spellCheck={false} />
      </label>
      <div className="environment__add">
        <input className="mono" value={newEntry} onChange={(event) => setNewEntry(event.target.value)} placeholder="/opt/homebrew/bin" />
        <button className="btn btn--ghost" onClick={addEntry} disabled={!newEntry.trim()}>Add to PATH</button>
      </div>
      <div className="environment__actions">
        <button className="btn" onClick={() => void save()} disabled={saving || loading || activeSessions > 0 || path === environment?.path}>
          {saving ? "Saving…" : "Save and restart"}
        </button>
        <button className="btn btn--ghost" onClick={() => void load()} disabled={saving || loading}>Recheck</button>
        {activeSessions > 0 && <span>Finish active sessions before changing PATH.</span>}
      </div>
      {message && <p className="environment__message">{message}</p>}
      <ul className="environment__commands">
        {environment?.commands.map((command) => (
          <li key={command.command} className={command.found ? "environment__command--ok" : "environment__command--bad"}>
            <span>{command.found ? "✓" : "!"}</span>
            <div>
              <strong className="mono">{command.command}</strong>
              <p>{command.found ? [command.path, command.version].filter(Boolean).join(" · ") : command.message}</p>
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}
