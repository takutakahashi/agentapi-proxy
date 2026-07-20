# External Session Manager

External Session Manager (ESM) lets a main agentapi-proxy instance route session
workloads to another agentapi-proxy instance. The main proxy is called **親プロキシ**.
The external manager remains **External Session Manager** or **ESM**.

ESM keeps an outbound polling connection to 親プロキシ and picks up allocation
requests. 親プロキシ does not need to send session creation requests to the ESM.
This is useful for development and for environments where the ESM should
register itself by token.

## Data Flow

```text
user -> 親プロキシ /start
        親プロキシ queues an allocation for manager_id
        ESM polls 親プロキシ with SESSION_MANAGER_CONNECTION_TOKEN
        ESM creates/adopts a local session
        ESM reports remote_session_id and SESSION_MANAGER_PUBLIC_URL
user -> 親プロキシ /:sessionId/*
        親プロキシ HMAC-signs and forwards traffic to the ESM
```

親プロキシ still needs a routable URL for the ESM after allocation, because normal
session traffic such as `/status`, messages, and delete is proxied to the
remote session. That URL is `SESSION_MANAGER_PUBLIC_URL`.

## 親プロキシ: Register the Manager

Register an ESM without `url`. 親プロキシ generates a one-time `connection_token`
when `hmac_secret` is omitted.

```bash
PARENT_PROXY_URL="https://parent-proxy.example.com"
API_KEY="<parent-proxy-api-key>"
USERNAME="<github-username>"

curl -X PUT "$PARENT_PROXY_URL/settings/$USERNAME" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "external_session_managers": [
      {
        "id": "dev-esm-allocator",
        "name": "Dev ESM Allocator",
        "default": false
      }
    ]
  }'
```

The response includes the token only at creation or rotation time:

```json
{
  "external_session_managers": [
    {
      "id": "dev-esm-allocator",
      "name": "Dev ESM Allocator",
      "has_connection_token": true,
      "connection_token": "<generated-token>",
      "default": false
    }
  ]
}
```

Store `<generated-token>` securely. It is not returned by later settings reads.

When updating settings, preserve any existing managers that should remain
registered. The `external_session_managers` array represents the desired list.

## External Session Manager: Required Environment

ESM runs the same `agentapi-proxy server`, with session manager mode and
Kubernetes session provisioning enabled.

```bash
export SESSION_MANAGER_ENABLED=true
export SESSION_MANAGER_UPSTREAM_URL="https://parent-proxy.example.com"
export SESSION_MANAGER_CONNECTION_TOKEN="<generated-token>"
export SESSION_MANAGER_HMAC_SECRET="<generated-token>"
export SESSION_MANAGER_PUBLIC_URL="https://esm.example.com"
export AGENTAPI_K8S_SESSION_PROVISIONER_PROXY_URL="https://esm.example.com"
```

Important details:

- `SESSION_MANAGER_CONNECTION_TOKEN` authenticates the ESM to 親プロキシ's
  allocator endpoint.
- `SESSION_MANAGER_HMAC_SECRET` must match the manager token stored in 親プロキシ.
  親プロキシ uses that same secret to sign proxied requests to the ESM.
- `SESSION_MANAGER_PUBLIC_URL` is the URL 親プロキシ stores in the session route
  after allocation. It must be reachable from 親プロキシ.
- `AGENTAPI_K8S_SESSION_PROVISIONER_PROXY_URL` should point at the ESM so
  provisioned session pods call back to the correct manager.

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
- ESM public URL for routes:
  `http://agentapi-proxy-esm-dev.agentapi-ui-dev.svc.cluster.local:8080`
- Connection token stored in Secret:
  `agentapi-proxy-esm-dev-token`, key `connection_token`
- The same token used as `SESSION_MANAGER_CONNECTION_TOKEN` and
  `SESSION_MANAGER_HMAC_SECRET`

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
  --public-url "https://native-mac.example.com" \
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
