# agentapi-proxy Native Dashboard

A minimal, production-shaped **macOS menu-bar dashboard** for the
agentapi-proxy **native External Session Manager** (ESM). Built with
Tauri 2 + Vite + React + TypeScript.

The app bundles the `ccplant` Go binary as a Tauri sidecar. It uses that
binary for first-run registration and installation, then reads status,
sessions, doctor output, and restarts the daemon. The native daemon is owned by launchd and runs
independently of this dashboard — closing the window simply hides it.

## Layout

```
native/
├── package.json            # dev / build / check / tauri scripts
├── vite.config.ts          # Vite dev server on :1420 (Tauri convention)
├── tsconfig.json
├── index.html
├── src/                    # React + TypeScript frontend
│   ├── main.tsx
│   ├── App.tsx             # dashboard shell, loading/error/empty states
│   ├── api.ts              # invoke() wrappers + refresh-event listener
│   ├── types.ts            # mirrors of the Rust structs
│   ├── styles.css          # tasteful lightweight CSS (light + dark)
│   └── components/
│       ├── StatusCard.tsx
│       ├── SessionsCard.tsx
│       └── DoctorCard.tsx
└── src-tauri/              # Rust + Tauri 2 shell
    ├── Cargo.toml
    ├── tauri.conf.json
    ├── build.rs
    ├── capabilities/default.json
    ├── icons/              # placeholder icons (see below)
    └── src/
        ├── main.rs         # entry point
        ├── lib.rs          # tray menu, window hide-on-close, command registration
        ├── binary.rs       # execute bundled sidecar, with development fallbacks
        ├── commands.rs     # Tauri commands wrapping `native <sub> --json`
        └── types.rs        # serde structs matching the CLI JSON
```

## What it shows

- **Service** card: service state, health, manager ID, version, upstream,
  state directory, filesystem-sandbox flag, and labels.
- **Active session count** in the header.
- **Sessions** table: id, status, pid, start time, with an empty state.
- **Logs** viewer: live tail for the session manager or each session's combined
  provisioner and agent stdout/stderr.
- **Doctor** card: result of `native doctor --json` (healthy / issues).
- **Manager environment** card: add or edit PATH entries, restart the selected
  instance after saving, and verify `node`, `npx`, `mise`, `gh`, and `git` with
  their resolved locations and versions.
- **Refresh** and **Restart** actions; optional 15s auto-refresh while visible.
- **Loading / error / empty** states for every panel.
- **First-run setup** that registers the Mac and installs its per-user
  LaunchAgent without requiring a separately installed CLI.
- Optional **agent command setup with mise** that selects global Node.js and
  GitHub CLI versions and exposes their resolved binaries to every native agent session.
- Multiple named instances, with an instance selector and instance-scoped add,
  restart, diagnostics, session listing, and removal actions.

Installation requires a short-lived one-time enrollment credential:

```bash
agentapi-proxy native install \
  --upstream https://parent.example.com \
  --registration-token '<one-time-token>'
```

The manager establishes an authenticated outbound control poll to the parent,
so the Mac does not need a parent-reachable address or inbound firewall rule.

## Menu-bar tray

The app installs a system-tray icon with:

- **Show Dashboard** — reveals and focuses the window.
- **Refresh** — emits a `dashboard://refresh` event the frontend listens for.
- **Restart** — runs `native restart` on a background thread, then refreshes.
- **Update manager** — replaces the installed manager with the app-bundled binary and restarts it; disabled while sessions are active.
- **Quit** — exits the dashboard only.

A single left-click on the tray icon also reveals the dashboard.

### Window close = hide

The dashboard window intercepts `CloseRequested`, calls `api.prevent_close()`,
and hides itself. The window is created hidden on launch and revealed from the
tray. The native daemon (launchd `com.agentapi.native`) is independent and
keeps running regardless of the dashboard's lifecycle.

## How it talks to the proxy

The Rust side executes the bundled `agentapi-proxy` sidecar by default. An
`CCPLANT_BINARY_PATH` override remains available for development and accepts a
path to an executable file. The override applies to all commands, including
install, update, and reset. If
the sidecar is unavailable, the fallback lookup order is:

1. The macOS binary managed by `native install` under
   `~/Library/Application Support/agentapi-native/bin/`.
2. `agentapi-proxy` looked up on `PATH`.

The first-run form invokes `native install` with the bundled sidecar. Its
one-time registration token is passed only to that child process and is not written to GUI settings;
the CLI continues to store only the resulting connection credential.

When command setup is selected, the app locates an existing `mise` installation,
runs `mise use --global node@<version> github-cli@<version>`, and passes a PATH
containing the resolved `node` and `gh` binary directories plus the mise shim
and binary directories to `native install --manager-env`. The defaults are
Node.js `lts` and GitHub CLI `latest`. mise itself is not installed by the app.

Then it runs (JSON parsed into typed serde structs):

| Command                         | Tauri command    | Rust struct      |
|---------------------------------|------------------|------------------|
| `native list --json`            | `native_list`    | `Vec<NativeInstance>` |
| `native status --json`          | `native_status`  | `NativeStatus`   |
| `native sessions --json`        | `native_sessions`| `Vec<NativeSession>` |
| `native logs --tail 500 …`      | `native_logs`    | `String`        |
| `native doctor --json`          | `native_doctor`  | `DoctorResult`   |
| `native restart`                | `native_restart` | `CommandResult`  |
| `native update`                 | `native_update`  | `CommandResult`  |

`sessions` is the alias of `session-list` and emits a JSON array. `doctor`
returns an overall `ok` value plus structured checks so the dashboard can show
partial failures without parsing human-readable output.

The dashboard passes `--instance <name>` to instance-scoped commands. The
default instance deliberately omits that flag so existing installations keep
their historical paths and `com.agentapi.native` LaunchAgent label.

The struct fields mirror `nativeStatusOutput` / `nativeSessionListEntry` in
`backend/cmd/native.go` (`#[serde(rename_all = "snake_case")]` + `#[serde(default)]`
on optional fields) so partial CLI output stays forward-compatible.

## Prerequisites

- macOS 13+ (menu-bar tray + `filesystem-sandbox` is macOS-only upstream).
- Node.js 20+ and a package manager (`bun` is used in the scripts; `npm`/`pnpm`
  work too — adjust `beforeDevCommand` / `beforeBuildCommand` in
  `tauri.conf.json` if you switch).
- Rust toolchain (`rustup`) with the `aarch64-apple-darwin` and
  `x86_64-apple-darwin` targets for Tauri 2.
- Go 1.25+ when building the app, so the sidecar can be compiled from
  `../backend`. End users do not need Go or a separately installed CLI.

## Scripts

```bash
bun install

bun run dev       # Vite dev server on http://127.0.0.1:1420
bun run check     # tsc --noEmit (typecheck)
bun run build     # tsc --noEmit && vite build  -> dist/
bun run sidecar:build # build the Go sidecar for the current target
bun run sidecar:build:macos-arm64
bun run sidecar:build:macos-x64
bun run preview   # preview the built dist/
bun run tauri dev # build the Rust shell, launch the menu-bar app
bun run tauri build # produce a signed .app / .dmg bundle
```

> The `tauri` scripts require Rust/cargo. `dev`, `check`, `build`, and
> `preview` are pure-frontend and need only Node + the npm dependencies.

`tauri dev` and `tauri build` automatically run `sidecar:build` before the
frontend build. Tauri packages the generated target-triple-suffixed executable
through `bundle.externalBin`; generated binaries are intentionally ignored by
Git.

## Release

Native app releases are part of the monorepo's existing `v*` release flow.
Pushing a tag such as `v0.2.0` builds the backend binaries, container images,
Helm charts, and both macOS native app architectures into one GitHub Release.

```bash
git tag v0.2.0
git push origin v0.2.0
```

The release workflow builds and uploads Apple Silicon and Intel `.app`/`.dmg`
bundles to the draft created by GoReleaser. The release is published only after
every component succeeds.

By default, the macOS bundles are unsigned. Gatekeeper may block the first
launch, so users must explicitly allow the app from macOS Privacy & Security
settings. To publish signed and notarized bundles instead, configure all of
these repository secrets:

- `APPLE_CERTIFICATE`
- `APPLE_CERTIFICATE_PASSWORD`
- `APPLE_SIGNING_IDENTITY`
- `APPLE_ID`
- `APPLE_PASSWORD` (an app-specific password)
- `APPLE_TEAM_ID`

If none of the secrets are configured, the workflow publishes unsigned
bundles. If only some are configured, it fails before building to avoid a
partially configured release.

The app version is derived from the shared release tag at build time; do not
manually edit `tauri.conf.json` for each release.

## Icons

`src-tauri/icons/` contains small placeholder PNGs so the project is
self-contained. For a real bundle, drop a 1024×1024 `app-icon.png` into
`src-tauri/icons/` and run:

```bash
bun run tauri icon src-tauri/icons/app-icon.png
```

This regenerates the full set (`icon.icns`, `128x128.png`, etc.) referenced by
`tauri.conf.json`.

## Development notes

- The Vite dev server uses a **fixed port 1420** with `strictPort` so the Rust
  shell's `devUrl` always matches.
- `clearScreen: false` keeps Tauri's build logs readable.
- The frontend never touches the filesystem or the network directly — all
  proxy interaction is funnelled through Tauri commands, which run the CLI in
  the main process. This keeps the bundle sandbox-friendly.
- `Promise.allSettled` in `src/api.ts` means a single failing panel (e.g.
  `doctor` returning non-zero) does not blank the whole dashboard; warnings
  are surfaced in a banner and the remaining panels render normally.
- Capabilities in `capabilities/default.json` are scoped to the `dashboard`
  window and only allow the core, window, event, and opener permissions the
  UI actually needs.

## Caveats

- The dashboard supports first-run **Install**, **Restart**, and a confirmed
  local reset that removes the LaunchAgent, configuration, credentials, and
  session state while retaining the parent registration. Reset requires no API
  key. Token rotation and removing the parent registration remain CLI-only.
