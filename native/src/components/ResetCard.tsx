import { useState, type FormEvent } from "react";
import { resetDaemon } from "../api";

export function ResetCard({
  activeSessions,
  instance,
  onReset,
}: {
  activeSessions: number;
  instance: string;
  onReset: () => Promise<void>;
}) {
  const [expanded, setExpanded] = useState(false);
  const [confirmation, setConfirmation] = useState("");
  const [force, setForce] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      const result = await resetDaemon({ instance, force });
      if (!result.ok) throw new Error(result.message || "Reset failed");
      setConfirmation("");
      await onReset();
    } catch (cause) {
      setError(String(cause));
    } finally {
      setSubmitting(false);
    }
  }

  if (!expanded) {
    return (
      <section className="card danger-zone">
        <header className="card__header"><h2>Danger Zone</h2></header>
        <p>Stop and remove the <strong>{instance}</strong> instance's local configuration and data.</p>
        <button className="btn btn--danger danger-zone__button" onClick={() => setExpanded(true)}>
          Remove instance…
        </button>
      </section>
    );
  }

  const canSubmit = confirmation === "RESET" && (activeSessions === 0 || force);
  return (
    <section className="card danger-zone danger-zone--expanded">
      <header className="card__header"><h2>Remove {instance}</h2></header>
      <p>This removes the daemon deployment, local configuration and credentials, and session state. The registration on the parent server is kept.</p>
      <form onSubmit={(event) => void submit(event)}>
        {activeSessions > 0 && (
          <label className="danger-zone__force">
            <input type="checkbox" checked={force} onChange={(event) => setForce(event.target.checked)} />
            Terminate {activeSessions} active session{activeSessions === 1 ? "" : "s"}
          </label>
        )}
        <label>Type <strong>RESET</strong> to confirm<input required autoComplete="off" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} /></label>
        {error && <p className="setup__error">{error}</p>}
        <div className="danger-zone__actions">
          <button className="btn btn--ghost" type="button" disabled={submitting} onClick={() => setExpanded(false)}>Cancel</button>
          <button className="btn btn--danger" type="submit" disabled={submitting || !canSubmit}>{submitting ? "Resetting…" : "Reset everything"}</button>
        </div>
      </form>
      <p className="setup__hint">No API key is required. Reinstalling can reuse the existing parent registration.</p>
    </section>
  );
}
