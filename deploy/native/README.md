# Native External Session Manager

`agentapi-proxy native-session-manager` runs sessions as native process groups on a Linux or macOS host. It does not provide a sandbox; dedicate the host to one user or a mutually trusted team.

The daemon needs a parent proxy URL, the External Session Manager connection token registered in that user's settings, a parent-reachable public URL, and a private state directory. Register labels such as `os`, `arch`, and `pool` on the External Session Manager settings entry. A session tag such as `allocator.os=linux` is matched against those labels.

On Linux, copy `agentapi-native.service` to `/etc/systemd/system/`, create the `agentapi` user and `/etc/agentapi-native/environment` with mode `0600`, then enable the unit. On macOS, replace all `REPLACE_*` values in the plist, store it as `~/Library/LaunchAgents/com.agentapi.native.plist`, and load it with `launchctl bootstrap gui/$(id -u) ...`.

Each session is placed below `<state-dir>/sessions/<session-id>/`, with independent `home`, `workdir`, ports, logs, and persisted runtime state. Deleting the public session terminates the provisioner process group and removes that directory.
