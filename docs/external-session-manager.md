# External Session Manager

External Session Manager (ESM) lets a main agentapi-proxy instance route session
workloads to another agentapi-proxy instance. The main proxy is called **Proxy A**.
The external manager is called **Proxy B**.

Proxy B keeps an outbound polling connection to Proxy A and picks up allocation
requests. Proxy A does not need to send session creation requests to Proxy B.
This is useful for development and for environments where Proxy B should
register itself by token.

## Data Flow

```text
user -> Proxy A /start
        Proxy A queues an allocation for manager_id
        Proxy B polls Proxy A with SESSION_MANAGER_CONNECTION_TOKEN
        Proxy B creates/adopts a local session
        Proxy B reports remote_session_id and SESSION_MANAGER_PUBLIC_URL
user -> Proxy A /:sessionId/*
        Proxy A HMAC-signs and forwards traffic to Proxy B
```

Proxy A still needs a routable URL for Proxy B after allocation, because normal
session traffic such as `/status`, messages, and delete is proxied to the
remote session. That URL is `SESSION_MANAGER_PUBLIC_URL`.

## Proxy A: Register the Manager

Register an ESM without `url`. Proxy A generates a one-time `connection_token`
when `hmac_secret` is omitted.

```bash
PROXY_A_URL="https://proxy-a.example.com"
API_KEY="<proxy-a-api-key>"
USERNAME="<github-username>"

curl -X PUT "$PROXY_A_URL/settings/$USERNAME" \
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

## Proxy B: Required Environment

Proxy B runs the same `agentapi-proxy server`, with session manager mode and
Kubernetes session provisioning enabled.

```bash
export SESSION_MANAGER_ENABLED=true
export SESSION_MANAGER_UPSTREAM_URL="https://proxy-a.example.com"
export SESSION_MANAGER_CONNECTION_TOKEN="<generated-token>"
export SESSION_MANAGER_HMAC_SECRET="<generated-token>"
export SESSION_MANAGER_PUBLIC_URL="https://proxy-b.example.com"
export AGENTAPI_K8S_SESSION_PROVISIONER_PROXY_URL="https://proxy-b.example.com"
```

Important details:

- `SESSION_MANAGER_CONNECTION_TOKEN` authenticates Proxy B to Proxy A's
  allocator endpoint.
- `SESSION_MANAGER_HMAC_SECRET` must match the manager token stored in Proxy A.
  Proxy A uses that same secret to sign proxied requests to Proxy B.
- `SESSION_MANAGER_PUBLIC_URL` is the URL Proxy A stores in the session route
  after allocation. It must be reachable from Proxy A.
- `AGENTAPI_K8S_SESSION_PROVISIONER_PROXY_URL` should point at Proxy B so
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

- Proxy A release: `agentapi-proxy` in `agentapi-ui-dev`
- Proxy B deployment: `agentapi-proxy-esm-dev`
- Proxy B service: `agentapi-proxy-esm-dev`
- Manager ID: `dev-esm-allocator`
- Proxy A URL for Proxy B polling:
  `http://agentapi-proxy.agentapi-ui-dev.svc.cluster.local:8080`
- Proxy B public URL for routes:
  `http://agentapi-proxy-esm-dev.agentapi-ui-dev.svc.cluster.local:8080`
- Connection token stored in Secret:
  `agentapi-proxy-esm-dev-token`, key `connection_token`
- The same token used as `SESSION_MANAGER_CONNECTION_TOKEN` and
  `SESSION_MANAGER_HMAC_SECRET`

## Starting a Session Through an ESM

Specify the manager explicitly:

```bash
curl -X POST "$PROXY_A_URL/start" \
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

## Verification

After creating a session, verify the route and live status.

```bash
SESSION_ID="<session-id-from-start>"

curl -H "X-API-Key: $API_KEY" \
  "$PROXY_A_URL/$SESSION_ID/status"
```

Expected result is HTTP `200` with a normal status body such as:

```json
{
  "status": "stable",
  "agent_type": "custom",
  "transport": "pty"
}
```

In Kubernetes, the Proxy A route secret should include both `remote_session_id`
and `proxy_url`:

```bash
kubectl get secret \
  -n agentapi-ui-dev \
  "agentapi-session-route-$SESSION_ID" \
  -o jsonpath='{.data.route\.json}' | base64 -d | jq .
```

The `proxy_url` must be the Proxy B public URL. If it is empty, Proxy A cannot
route session traffic after allocation.

Delete should also work through Proxy A:

```bash
curl -X DELETE \
  -H "X-API-Key: $API_KEY" \
  "$PROXY_A_URL/sessions/$SESSION_ID"
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

- Proxy B logs should include:
  `Started outbound allocator polling upstream: <Proxy A URL>`.
- If `/status` returns `503 External session manager has not reported a
  routable session yet`, check `SESSION_MANAGER_PUBLIC_URL` on Proxy B.
- If `/status` returns `404 Session not found`, check that Proxy B is reporting
  the concrete local session ID in the allocation result.
- If delete returns `500 Failed to delete remote session` and Proxy B logs
  `invalid signature`, check that `SESSION_MANAGER_HMAC_SECRET` on Proxy B
  matches the connection token stored in Proxy A.
- If session pods call the wrong proxy for provision requests, check
  `AGENTAPI_K8S_SESSION_PROVISIONER_PROXY_URL`.
