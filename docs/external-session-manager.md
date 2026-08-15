# External Session Manager

> **Proposed architecture:** [`direct-session-runtime.md`](direct-session-runtime.md) defines the
> migration that removes ESM from steady-state session traffic. The behavior below describes the
> current implementation until that migration is complete.

External Session Manager (ESM) lets a main agentapi-proxy instance route session
workloads to another agentapi-proxy instance. The main proxy is called **親プロキシ**.
The external manager remains **External Session Manager** or **ESM**.

ESM keeps an outbound polling connection to 親プロキシ and picks up allocation
requests. 親プロキシ does not need to send session creation requests to the ESM.
This is useful for development and for environments where the ESM should
register itself by token.

Current ESMs also keep an outbound control poll open. Normal session HTTP and SSE
traffic is carried as short-lived Redis-backed RPC commands fetched over that poll;
the ESM posts response frames back to 親プロキシ. The ESM never connects to Redis.

## Data Flow

```text
user -> 親プロキシ /start
        親プロキシ queues an allocation for manager_id
        ESM polls 親プロキシ with SESSION_MANAGER_CONNECTION_TOKEN
        ESM creates/adopts a local session
        ESM reports remote_session_id
user -> 親プロキシ /:sessionId/*
        親プロキシ queues an authenticated manager-scoped RPC
        ESM polls, dispatches it locally, and posts response/SSE frames
```

`SESSION_MANAGER_PUBLIC_URL` is optional. It is retained only as a compatibility
fallback for older managers that do not maintain an outbound control lease. A current
ESM can run behind NAT or a firewall without accepting traffic from 親プロキシ.

## 親プロキシ: Register the Manager

Issue a short-lived registration token from the Web settings page or `POST
/external-session-managers/registration-tokens`, then give only that token to
the manager host. Direct registration through settings or a parent API key is
not supported.

The issuance response contains a registration token that expires after 15
minutes and can be exchanged once:

```json
{
  "manager_id": "dev-esm-allocator",
  "registration_token": "<one-time-token>",
  "expires_at": "2026-08-02T07:00:00Z"
}
```

The manager exchanges it through `/external-session-managers/enroll` and
stores the returned connection token. Neither token is returned by later
settings reads.

## External Session Manager: Required Environment

ESM runs the same `agentapi-proxy server`, with session manager mode and
Kubernetes session provisioning enabled.

```bash
export SESSION_MANAGER_ENABLED=true
export SESSION_MANAGER_UPSTREAM_URL="https://parent-proxy.example.com"
export SESSION_MANAGER_CONNECTION_TOKEN="<generated-token>"
export SESSION_MANAGER_HMAC_SECRET="<generated-token>"
export AGENTAPI_K8S_SESSION_PROVISIONER_PROXY_URL="http://control.<esm-namespace>.svc.cluster.local:8080"
```

Important details:

- `SESSION_MANAGER_CONNECTION_TOKEN` authenticates the ESM to 親プロキシ's
  allocator endpoint.
- `SESSION_MANAGER_HMAC_SECRET` must match the manager token stored in 親プロキシ.
  親プロキシ uses that same secret to sign proxied requests to the ESM.
- `SESSION_MANAGER_PUBLIC_URL` is optional and only enables fallback for older
  parent/manager combinations.
- `AGENTAPI_K8S_SESSION_PROVISIONER_PROXY_URL` should point at the ESM so
  provisioned session pods call back to the correct manager.

### Parent-owned session runtime profile

Kubernetes ESMs do not independently configure the NFA or SCIA session runtime.
Every external allocation carries a versioned `runtime_profile` generated from
the parent proxy's effective configuration. Before creating the local session,
the ESM applies that profile and idempotently ensures its inherited session
ServiceAccount, Role, and RoleBinding.

The allocation long-poll also carries a content-derived profile revision. On
startup, after a parent connection failure recovers, or when that revision
changes, the ESM fetches and applies the current snapshot immediately. A
revision mismatch makes the parent end the current long-poll without waiting,
so configuration changes deployed with a parent restart do not wait for a
fixed synchronization interval. The profile on each allocation remains the
final consistency check before session creation.

The inherited profile includes:

- the session ServiceAccount;
- the NFA image and resource requests/limits, including its init containers;
- whether the SCIA session sidecar is enabled;
- the SCIA and config-renderer images, proxy port, `NO_PROXY`, credential IDs,
  and integration host/path rules.

There are intentionally no per-field ESM-side override flags for these fields.
Kubernetes ESMs always apply the profile. Native ESMs ignore it by default and
can opt in to the whole profile at installation time with
`native install --inherit-runtime-profile`. An older parent that does not send
`runtime_profile` retains the ESM's existing local configuration for upgrade
compatibility.

## Kubernetes Example

This is the shape used in the `agentapi-ui-dev` development environment.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: agentapi-proxy-esm-dev-token
  namespace: agentapi-ui-dev
type: Opaque
stringData:
  connection_token: "<generated-token>"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agentapi-proxy-esm-dev
  namespace: agentapi-ui-dev
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: agentapi-proxy-esm-dev
  template:
    metadata:
      labels:
        app.kubernetes.io/name: agentapi-proxy-esm-dev
    spec:
      serviceAccountName: agentapi-proxy
      containers:
        - name: agentapi-proxy
          image: ghcr.io/takutakahashi/agentapi-proxy:dev-7d4f9bf
          args: ["agentapi-proxy", "server", "--port", "8080"]
          env:
            - name: SESSION_MANAGER_ENABLED
              value: "true"
            - name: SESSION_MANAGER_UPSTREAM_URL
              value: "http://agentapi-proxy.agentapi-ui-dev.svc.cluster.local:8080"
            - name: SESSION_MANAGER_CONNECTION_TOKEN
              valueFrom:
                secretKeyRef:
                  name: agentapi-proxy-esm-dev-token
                  key: connection_token
            - name: SESSION_MANAGER_HMAC_SECRET
              valueFrom:
                secretKeyRef:
                  name: agentapi-proxy-esm-dev-token
                  key: connection_token
            - name: SESSION_MANAGER_PUBLIC_URL
              value: "http://agentapi-proxy-esm-dev.agentapi-ui-dev.svc.cluster.local:8080"
            - name: AGENTAPI_K8S_SESSION_PROVISIONER_PROXY_URL
              value: "http://agentapi-proxy-esm-dev.agentapi-ui-dev.svc.cluster.local:8080"
            - name: AGENTAPI_K8S_SESSION_IMAGE
              value: "ghcr.io/takutakahashi/agentapi-proxy:dev-7d4f9bf"
---
apiVersion: v1
kind: Service
metadata:
  name: agentapi-proxy-esm-dev
  namespace: agentapi-ui-dev
spec:
  selector:
    app.kubernetes.io/name: agentapi-proxy-esm-dev
  ports:
    - name: http
      port: 8080
      targetPort: 8080
```

In dev, the working configuration was:

- 親プロキシ release: `agentapi-proxy` in `agentapi-ui-dev`
- ESM deployment: `agentapi-proxy-esm-dev`
- ESM service: `agentapi-proxy-esm-dev`
- Manager ID: `dev-esm-allocator`
- 親プロキシ URL for ESM polling:
  `http://agentapi-proxy.agentapi-ui-dev.svc.cluster.local:8080`
- Connection token stored in Secret:
  `agentapi-proxy-esm-dev-token`, key `connection_token`
- The same token used as `SESSION_MANAGER_CONNECTION_TOKEN` and
  `SESSION_MANAGER_HMAC_SECRET`

## Registering a Native Manager

Use `native install` on the machine that will run native sessions. The command
registers the manager with the parent proxy, stores the returned connection
token, installs the host service, starts it, and sends the first heartbeat.

Before registering, prepare:

- A one-time registration token issued by the parent proxy.
- An upstream URL for the parent proxy.

The native manager opens an authenticated outbound control poll. It does not
need a parent-reachable address, VPN route, reverse proxy, or inbound firewall
rule.

Multiple managers can run on one host. Give every additional manager a unique
instance name and listen address:

```bash
agentapi-proxy native install --instance build-a --listen :8081 \
  --upstream "$PARENT_PROXY_URL" \
  --name build-a --registration-token "<registration-token>"
agentapi-proxy native install --instance build-b --listen :8082 \
  --upstream "$PARENT_PROXY_URL" \
  --name build-b --registration-token "<registration-token>"

agentapi-proxy native list
agentapi-proxy native status --instance build-a
agentapi-proxy native doctor --instance build-b
```

Omitting `--instance` selects the backward-compatible `default` instance. The
CLI adds a protected `native_instance` allocator label automatically. Named
instances have separate configuration, credentials, state, logs, and host
services. On Linux they share the managed executable; uninstalling one instance
does not remove that executable while another instance still uses it.

For a user-scoped macOS manager with the filesystem sandbox enabled:

```bash
agentapi-proxy native install \
  --upstream "https://parent-proxy.example.com" \
  --name "ios-builder" \
  --registration-token "<registration-token>" \
  --label purpose=ios \
  --filesystem-sandbox
```

Use `--manager-env KEY=VALUE` to set an environment variable on the native
manager service and its child provisioner processes. This is separate from
session environment settings. The option is repeatable, and values are kept in
the native configuration across later installs unless the same key is replaced.
For example, a macOS manager can expose mise-managed Node.js and the installed
`agentapi-proxy` binary through `PATH`:

```bash
NODE_BIN="$(dirname "$(mise which node)")"
NATIVE_BIN="$HOME/Library/Application Support/agentapi-native/bin"

agentapi-proxy native install \
  --upstream "https://parent-proxy.example.com" \
  --manager-env "PATH=$NODE_BIN:$NATIVE_BIN:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
```

Do not pass secrets through `--manager-env`; service definitions may be
readable by other local users on some platforms.

Add `--default` to select this manager when a session does not specify a
manager. To register it for a team instead of the current user:

```bash
agentapi-proxy native install \
  --upstream "https://parent-proxy.example.com" \
  --name "team-ios-builder" \
  --scope team \
  --team-id "my-org/ios-team" \
  --label purpose=ios \
  --filesystem-sandbox
```

Issue a one-time registration token for the authenticated user (or include
`{"scope":"team","team_id":"my-org/ios-team"}` for a team):

```bash
curl -X POST "$PARENT_PROXY_URL/external-session-managers/registration-tokens" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{}'

agentapi-proxy native install \
  --upstream "$PARENT_PROXY_URL" \
  --registration-token "<registration_token>"
```

The token expires after 15 minutes, is stored only as a hash by the parent,
and can be exchanged exactly once. The manager automatically receives and
stores its long-lived connection token during the exchange.
When a team-bound service account issues a token without specifying `scope`,
the token defaults to that service account's team scope. An explicit `scope`
still takes precedence.

The registration flow is:

```text
authenticated user -> POST /external-session-managers/registration-tokens
native install --registration-token ...
  -> POST /external-session-managers/enroll
  -> receive manager ID and connection token (returned once)
  -> install and start the native host service
  -> GET the outbound allocation and control long polls
  -> POST the manager heartbeat and response frames
```

## Outbound control authentication

The allocation poll, control poll, response frames, and heartbeat all use the
manager-specific connection token. 親プロキシ resolves the token to exactly one
manager ID and rejects cross-manager access. Each RPC request is additionally bound
to that manager in Redis; response frames from another manager are rejected.

The control lease expires after 75 seconds. While it is live, 親プロキシ never probes
or connects to `public_url`. If the lease is absent, routes with a legacy public URL
fall back to direct HMAC-signed HTTP; routes without one return 503 until the manager
reconnects.

Commands and response frames expire from Redis after five minutes. User
Authorization, API-key, cookie, and manager-token headers are removed before a
command is stored. Redis remains an ephemeral relay and is accessed only by the
parent backend.

On macOS, configuration and credentials are stored under:

```text
~/Library/Application Support/agentapi-native/config.json
~/Library/Application Support/agentapi-native/credentials.json
```

Named instances use sibling directories such as
`~/Library/Application Support/agentapi-native-build-a/` and LaunchAgent labels
such as `com.agentapi.native.build-a`.

Check the installed service and end-to-end parent connectivity:

```bash
agentapi-proxy native status
agentapi-proxy native doctor
```

`native status` reports the manager ID, upstream URL, active
sessions, and whether the filesystem sandbox is enabled. `native doctor`
checks local configuration permissions, service health, and the parent
heartbeat.

List native sessions and inspect their provisioner logs directly from the host:

```bash
agentapi-proxy native session-list
agentapi-proxy native logs <session-id>
agentapi-proxy native logs --follow --tail 200 <session-id>
agentapi-proxy native logs --daemon --follow
```

Session logs live below `<state-dir>/sessions/<session-id>/runtime/provisioner.log`.
They are removed along with the session directory when the session is deleted.

## Starting a Session Through an ESM

Specify the manager explicitly:

```bash
curl -X POST "$PARENT_PROXY_URL/start" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "scope": "user",
    "params": {
      "manager_id": "dev-esm-allocator",
      "message": "Hello from ESM",
      "agent_type": "codex",
      "session_ttl": "30m"
    }
  }'
```

If the manager is registered with `"default": true`, omit `manager_id` to route
new sessions to that ESM by default.

## macOS Native Filesystem Sandbox

Native ESM installations on macOS can wrap every session provisioner and its
descendant processes with the built-in Seatbelt `sandbox-exec` utility. Enable
it when installing the manager:

```bash
agentapi-proxy native install \
  --upstream "https://parent-proxy.example.com" \
  --filesystem-sandbox
```

The generated daemon configuration contains a single switch:

```json
{
  "filesystem_sandbox": {
    "enabled": true
  }
}
```

When omitted or set to `false`, native sessions retain their existing
unsandboxed behavior. When enabled, the session can read and write its own
`home`, `workdir`, `build`, `tmp`, and `runtime` directories, while the rest of
the host user's home and sibling native sessions are inaccessible. macOS and
Xcode services remain available so `xcodebuild` and Simulator workflows can
run. Build output should be directed to `$AGENTAPI_BUILD_DIR`.

The option is fail-closed: the daemon refuses to start on non-macOS hosts or
when `/usr/bin/sandbox-exec` is unavailable, and a session is not launched if
its generated Seatbelt profile fails validation. Because `sandbox-exec` is a
deprecated macOS facility, this backend should be treated as best-effort host
protection rather than a VM-strength isolation boundary.

## Verification

After creating a session, verify the route and live status.

```bash
SESSION_ID="<session-id-from-start>"

curl -H "X-API-Key: $API_KEY" \
  "$PARENT_PROXY_URL/$SESSION_ID/status"
```

Expected result is HTTP `200` with a normal status body such as:

```json
{
  "status": "stable",
  "agent_type": "custom",
  "transport": "pty"
}
```

In Kubernetes, the 親プロキシ route secret should include both `remote_session_id`
and `proxy_url`:

```bash
kubectl get secret \
  -n agentapi-ui-dev \
  "agentapi-session-route-$SESSION_ID" \
  -o jsonpath='{.data.route\.json}' | base64 -d | jq .
```

The `proxy_url` must be the ESM public URL. If it is empty, 親プロキシ cannot
route session traffic after allocation.

Delete should also work through 親プロキシ:

```bash
curl -X DELETE \
  -H "X-API-Key: $API_KEY" \
  "$PARENT_PROXY_URL/sessions/$SESSION_ID"
```

Expected result:

```json
{
  "message": "Session terminated successfully",
  "session_id": "<session-id>",
  "status": "terminated"
}
```

## Troubleshooting

- ESM logs should include:
  `Started outbound allocator polling upstream: <親プロキシ URL>`.
- If `/status` returns `503 External session manager has not reported a
  routable session yet`, check `SESSION_MANAGER_PUBLIC_URL` on the ESM.
- If `/status` returns `404 Session not found`, check that the ESM is reporting
  the concrete local session ID in the allocation result.
- If delete returns `500 Failed to delete remote session` and ESM logs
  `invalid signature`, check that `SESSION_MANAGER_HMAC_SECRET` on the ESM
  matches the connection token stored in 親プロキシ.
- If session pods call the wrong proxy for provision requests, check
  `AGENTAPI_K8S_SESSION_PROVISIONER_PROXY_URL`.
