# Native External Session Manager

`agentapi-proxy native-session-manager` runs sessions as native process groups on a Linux or macOS host. It does not provide a sandbox; dedicate the host to one user or a mutually trusted team.

## One-command installation

Use the management command to register the machine with the parent proxy, install the daemon, start it, and verify both local health and the parent heartbeat:

```bash
sudo --preserve-env=AGENTAPI_KEY agentapi-proxy native install \
  --upstream https://cc-api.example.com \
  --public-url http://10.0.0.10:8080 \
  --name native-builder-01 \
  --label pool=native \
  --label machine=native-builder-01
```

On macOS omit `sudo`; the command installs a per-user LaunchAgent. `os`, `arch`, and `hostname` labels are detected automatically. The API key is used only for registration and is never written to daemon configuration. Use `--api-key-stdin` or `--api-key-file` when preserving an environment variable is undesirable.

The command is idempotent. It stores a stable instance ID, updates the existing registration and service definition on repeat runs, and preserves the connection token. Use these lifecycle commands after installation:

```bash
agentapi-proxy native status
agentapi-proxy native doctor
agentapi-proxy native restart
agentapi-proxy native rotate-token
agentapi-proxy native uninstall
```

Linux stores non-secret configuration in `/etc/agentapi-native/config.json`, the connection token in `/etc/agentapi-native/credentials.json`, state in `/var/lib/agentapi-native`, and the managed executable in `/usr/local/libexec/agentapi-proxy/`. macOS uses `~/Library/Application Support/agentapi-native/` and `~/Library/LaunchAgents/com.agentapi.native.plist`.

The daemon needs a parent proxy URL, the External Session Manager connection token registered in that user's settings, a parent-reachable public URL, and a private state directory. Register labels such as `os`, `arch`, and `pool` on the External Session Manager settings entry. A session tag such as `allocator.os=linux` is matched against those labels.

On Linux, copy `agentapi-native.service` to `/etc/systemd/system/`, create the `agentapi` user and `/etc/agentapi-native/environment` with mode `0600`, then enable the unit. On macOS, replace all `REPLACE_*` values in the plist, store it as `~/Library/LaunchAgents/com.agentapi.native.plist`, and load it with `launchctl bootstrap gui/$(id -u) ...`.

Each session is placed below `<state-dir>/sessions/<session-id>/`, with independent `home`, `workdir`, ports, logs, and persisted runtime state. Deleting the public session terminates the provisioner process group and removes that directory.
