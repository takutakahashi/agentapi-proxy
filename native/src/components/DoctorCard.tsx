import type { DoctorResult } from "../types";

interface Props {
  doctor: DoctorResult;
}

export function DoctorCard({ doctor }: Props) {
  return (
    <section className="card">
      <header className="card__header">
        <h2>Doctor</h2>
        <span className={`pill pill--${doctor.ok ? "ok" : "bad"}`}>
          {doctor.ok ? "healthy" : "issues"}
        </span>
      </header>
      <ul className="doctor__checks">
        {doctor.checks.map((check) => (
          <li key={check.id} className={check.status === "ok" ? "doctor__check--ok" : "doctor__check--bad"}>
            <span>{check.status === "ok" ? "✓" : "!"}</span>
            <div><strong>{check.id.replaceAll("_", " ")}</strong><p>{check.message}</p></div>
          </li>
        ))}
      </ul>
    </section>
  );
}
