import type { NativeStatus } from "../types";

interface Props {
  status: NativeStatus;
}

export function StatusCard({ status }: Props) {
  return (
    <section className="card">
      <header className="card__header">
        <h2>Service</h2>
        <span className={`pill pill--${status.service === "running" ? "ok" : "bad"}`}>
          {status.service}
        </span>
      </header>
      <dl className="grid">
        <Field label="Health" value={status.health} mono />
        <Field label="Manager ID" value={status.manager_id || "—"} mono />
        <Field label="Version" value={status.version || "—"} mono />
        <Field label="Upstream" value={status.upstream || "—"} mono />
        <Field label="State dir" value={status.state || "—"} mono />
        <Field
          label="Filesystem sandbox"
          value={status.filesystem_sandbox ? "Enabled (macOS)" : "Disabled"}
        />
      </dl>
      {Object.keys(status.labels).length > 0 && (
        <div className="labels">
          <span className="labels__title">Labels</span>
          {Object.entries(status.labels).map(([key, value]) => (
            <span key={key} className="chip">{key}={value}</span>
          ))}
        </div>
      )}
    </section>
  );
}

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid__item">
      <dt>{label}</dt>
      <dd className={mono ? "mono" : undefined} title={value}>{value}</dd>
    </div>
  );
}
