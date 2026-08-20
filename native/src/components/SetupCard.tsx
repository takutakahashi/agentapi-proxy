import { useState, type FormEvent } from "react";
import { installDaemon } from "../api";
import type { InstallRequest } from "../types";

export function SetupCard({ onInstalled, initialInstance = "default" }: { onInstalled: (instance: string) => Promise<void>; initialInstance?: string }) {
  const [request, setRequest] = useState<InstallRequest>({
    instance: initialInstance,
    upstream: "",
    listen: initialInstance === "default" ? ":8080" : ":8081",
    name: "",
    registrationToken: "",
    filesystemSandbox: true,
    setupNodejs: true,
    nodejsVersion: "lts",
    setupGithubCli: true,
    githubCliVersion: "latest",
  });
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      const result = await installDaemon(request);
      if (!result.ok) throw new Error(result.message || "Installation failed");
      setRequest((current) => ({ ...current, registrationToken: "" }));
      await onInstalled(request.instance);
    } catch (cause) {
      setError(String(cause));
    } finally {
      setSubmitting(false);
    }
  }

  function update<K extends keyof InstallRequest>(key: K, value: InstallRequest[K]) {
    setRequest((current) => ({ ...current, [key]: value }));
  }

  return (
    <section className="card setup">
      <header className="card__header"><h2>Add native ESM instance</h2></header>
      <p className="setup__intro">The agentapi-proxy CLI is bundled with this app. Register this Mac and install its per-user LaunchAgent to get started.</p>
      <form onSubmit={(event) => void submit(event)}>
        <label>Instance name<input required pattern="[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?" value={request.instance} onChange={(e) => update("instance", e.target.value)} /></label>
        <label>Parent AgentAPI URL<input type="url" required placeholder="https://agentapi.example.com" value={request.upstream} onChange={(e) => update("upstream", e.target.value)} /></label>
        <label>Listen address<input required placeholder=":8081" value={request.listen} onChange={(e) => update("listen", e.target.value)} /></label>
        <label>This Mac's name<input required placeholder="native-mac" value={request.name} onChange={(e) => update("name", e.target.value)} /></label>
        <label>Registration token<input type="password" required autoComplete="off" value={request.registrationToken} onChange={(e) => update("registrationToken", e.target.value)} /></label>
        <label className="setup__checkbox"><input type="checkbox" checked={request.filesystemSandbox} onChange={(e) => update("filesystemSandbox", e.target.checked)} /> Restrict session filesystem access</label>
        <fieldset className="setup__group">
          <legend>Agent command setup</legend>
          <label className="setup__checkbox"><input type="checkbox" checked={request.setupNodejs} onChange={(e) => update("setupNodejs", e.target.checked)} /> Set up Node.js with mise</label>
          {request.setupNodejs && (
            <label>Node.js version<input required placeholder="lts" value={request.nodejsVersion} onChange={(e) => update("nodejsVersion", e.target.value)} /></label>
          )}
          <label className="setup__checkbox"><input type="checkbox" checked={request.setupGithubCli} onChange={(e) => update("setupGithubCli", e.target.checked)} /> Set up GitHub CLI with mise</label>
          {request.setupGithubCli && (
            <label>GitHub CLI version<input required placeholder="latest" value={request.githubCliVersion} onChange={(e) => update("githubCliVersion", e.target.value)} /></label>
          )}
          <p className="setup__hint">Installs the selected tools globally with mise and adds their resolved bin directories to the native session PATH.</p>
        </fieldset>
        {error && <p className="setup__error">{error}</p>}
        <button className="btn" type="submit" disabled={submitting}>{submitting ? "Installing…" : "Install and connect"}</button>
      </form>
      <p className="setup__hint">The manager connects to the parent through an outbound control poll. The short-lived token is used once and is not saved by the app.</p>
    </section>
  );
}
