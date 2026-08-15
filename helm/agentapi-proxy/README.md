# AgentAPI Proxy Helm Chart

A Helm chart for deploying AgentAPI Proxy - a reverse proxy and process manager for agentapi server instances on Kubernetes.

## Process separation

The chart keeps three independent workloads in one chart: `ccplant server` for
the API proxy, `ccplant worker` for background controllers, and `ccplant
session-manager` for allocation and Kubernetes session lifecycle. Each role has
its own image, ServiceAccount, credentials, persistence and Redis client values.
With libSQL persistence, API and worker do not mount Kubernetes credentials.
With Kubernetes KV persistence they receive a Secret/ConfigMap-only Role;
session workload RBAC remains exclusive to the session-manager.

The chart remains a single chart. Configure the independent workloads under
`api`, `worker` and `sessionManager`. Deprecated root proxy values are retained
only for upgrade compatibility.
Workers persist application records through the configured `kvStore` backend
and use Redis leases for leader election; they never create Kubernetes Lease
objects. Session creation, listing, messaging, deletion, and stock operations
are delegated to the backend control API. Enabling `worker` requires either
libSQL or Kubernetes KV persistence, bundled or external Redis, and a
worker-control Secret shared only with the API.

```yaml
worker:
  enabled: true
  controlApi:
    tokenSecretRef:
      name: worker-control
  kvStore:
    backend: libsql
    databaseUrlSecretRef:
      name: worker-libsql
  redis:
    addr: redis.example:6379
  schedule:
    enabled: true
  stockInventory:
    enabled: true

api:
  kvStore:
    backend: libsql
    databaseUrlSecretRef:
      name: api-libsql
  redis:
    addr: redis.example:6379
  encryption:
    keySecretRef:
      name: application-encryption
  workerControl:
    tokenSecretRef:
      name: worker-control
  sessionManager:
    url: http://agentapi-proxy-session-manager:8080
    tokenSecretRef:
      name: manager-internal

sessionManager:
  enabled: true
  internalApi:
    tokenSecretRef:
      name: manager-internal
  encryption:
    keySecretRef:
      name: application-encryption
  kvStore:
    backend: libsql
    databaseUrlSecretRef:
      name: manager-libsql
  redis:
    addr: redis.example:6379
  kubernetesSession:
    provisioner:
      tokenSecretRef:
        name: agentapi-provisioner-token

  # Optional: register this manager with an external parent.
  externalRegistration:
    enabled: true
    upstreamUrl: https://control.example.com
    connectionTokenSecretRef:
      name: session-manager-credentials
    hmacSecretRef:
      name: session-manager-credentials
```

## Prerequisites

- Kubernetes 1.21+
- Helm 3.0+

## Installing the Chart

### From Local Chart

To install the chart with the release name `my-agentapi-proxy`:

```bash
helm install my-agentapi-proxy ./helm/agentapi-proxy
```

The default `values.yaml` is a minimal single-replica API-only installation.
Session allocation, SCIA, asset serving, persistent session workspaces,
background workers, OpenTelemetry collection, and Redis are opt-in. More than
one API replica requires the bundled Redis or `api.redis.addr`.

### Migrating legacy values

Charts that ran the API, workers, and Kubernetes session lifecycle in one
process used root-level values. Convert those values before enabling the
separated workloads:

```bash
helm get values agentapi-proxy -n agentapi -o yaml > legacy-values.yaml

ccplant helm migrate-values \
  --input legacy-values.yaml \
  --output separated-values.yaml \
  --namespace agentapi \
  --release agentapi-proxy \
  --worker-control-secret agentapi-worker-control \
  --manager-internal-secret agentapi-session-manager-internal \
  --encryption-secret agentapi-application-encryption \
  --provisioner-secret agentapi-provisioner-token

helm upgrade agentapi-proxy oci://ghcr.io/ccplant/charts/agentapi-proxy \
  -n agentapi -f separated-values.yaml
```

The converter preserves all legacy keys and adds the independent `api`,
`worker`, and `sessionManager` sections. It does not create or copy Secrets;
the four referenced Secrets must exist before the upgrade. Existing separated
role sections are rejected unless `--force` is explicitly supplied. Verify
that the migrated root image contains the `worker` and `session-manager`
subcommands; older monolithic images cannot run the separated Deployments.
Legacy `env` and `envFrom` entries are copied to every role for compatibility;
remove API-only or worker-only entries after confirming the migration.
For legacy libSQL configurations without an explicit KV namespace, the
converter preserves `kubernetesSession.namespace` as the logical namespace so
existing schedules, profiles, memories, and other resources remain visible.

### From OCI Registry (Recommended)

Once published to ghcr.io, you can install directly from the OCI registry:

```bash
helm install my-agentapi-proxy oci://ghcr.io/takutakahashi/charts/agentapi-proxy --version 0.1.0
```

The command deploys AgentAPI Proxy on the Kubernetes cluster in the default configuration. The [Parameters](#parameters) section lists the parameters that can be configured during installation.

> **Note**: For OCI registry publishing instructions, see [OCI_REGISTRY.md](../OCI_REGISTRY.md)

## Uninstalling the Chart

To uninstall/delete the `my-agentapi-proxy` deployment:

```bash
helm delete my-agentapi-proxy
```

The command removes all the Kubernetes components associated with the chart and deletes the release.

## Parameters

### Global parameters

| Name                      | Description                               | Value |
| ------------------------- | ----------------------------------------- | ----- |
| `nameOverride`            | String to partially override names       | `""`  |
| `fullnameOverride`        | String to fully override names           | `""`  |

### Image parameters

| Name                | Description                       | Value                                      |
| ------------------- | --------------------------------- | ------------------------------------------ |
| `api.image.repository` | API image repository | `ghcr.io/ccplant/ccplant-api` |
| `worker.image.repository` | Worker image repository | `ghcr.io/ccplant/ccplant-api` |
| `sessionManager.image.repository` | Session-manager image repository | `ghcr.io/ccplant/ccplant-backend` |
| `kubernetesSession.image` | Legacy in-process session Pod image; empty uses the full root image | `""` |
| `sessionManager.kubernetesSession.image` | Dedicated manager session Pod image; empty uses the full session-manager image | `""` |

An empty role image tag uses the chart's `appVersion`.

The default uses the lightweight image for the API and background worker while
keeping the compatibility-sensitive session-manager and session runtime on the
full image:

```yaml
api:
  image:
    repository: ghcr.io/ccplant/ccplant-api

worker:
  image:
    repository: ghcr.io/ccplant/ccplant-api
sessionManager:
  image:
    repository: ghcr.io/ccplant/ccplant-backend
```

The API-only image does not contain agent CLIs, Docker/GitHub tooling, or the
session runtime. It supports the API and worker commands, but must not be used
for session-manager, provisioner, or direct/local session execution roles.

Override these independently from CI/CD environment variables by passing them
to Helm, for example:

```bash
helm upgrade --install backend ./backend/helm/agentapi-proxy \
  --set-string api.image.repository="${BACKEND_API_IMAGE_REPOSITORY}" \
  --set-string kubernetesSession.image="${SESSION_IMAGE}"
```

When running `ccplant` without Helm, `AGENTAPI_K8S_SESSION_IMAGE` selects the
session Pod image directly. A container cannot change its own Kubernetes image
from an environment variable; use `api.image.repository`/`api.image.tag` to
select the backend API Deployment image.

### Deployment parameters

| Name                    | Description                                      | Value |
| ----------------------- | ------------------------------------------------ | ----- |
| `replicaCount`          | Number of AgentAPI Proxy replicas to deploy     | `1`   |
| `podAnnotations`        | Annotations for AgentAPI Proxy pods             | `{}`  |
| `podLabels`             | Extra labels for AgentAPI Proxy pods            | `{}`  |
| `podSecurityContext`    | Set AgentAPI Proxy pod's Security Context       | `{}`  |
| `securityContext`       | Set AgentAPI Proxy container's Security Context | `{}`  |

### Service parameters

| Name                  | Description                               | Value       |
| --------------------- | ----------------------------------------- | ----------- |
| `service.type`        | AgentAPI Proxy service type              | `ClusterIP` |
| `service.port`        | AgentAPI Proxy service HTTP port         | `8080`      |

### Ingress parameters

| Name                       | Description                                        | Value                    |
| -------------------------- | -------------------------------------------------- | ------------------------ |
| `ingress.enabled`          | Enable ingress record generation                   | `false`                  |
| `ingress.className`        | IngressClass that will be used                     | `nginx`                  |
| `ingress.annotations`      | Additional annotations for the Ingress resource   | `{}`                     |
| `ingress.hosts`            | An array with hosts and paths                      | `[{"host": "agentapi.example.com", "paths": [{"path": "/", "pathType": "Prefix"}]}]` |
| `ingress.tls`              | TLS configuration for ingress                      | `[]`                     |

### scia parameters

| Name                                               | Description                                      | Value |
| -------------------------------------------------- | ------------------------------------------------ | ----- |
| `scia.enabled`                                     | Deploy scia OAuth broker and configure sessions | `false` |
| `scia.publicBaseUrl`                               | Browser-facing scia base URL                    | `""` |
| `scia.credential`                                  | Google credential ID injected into sessions; empty disables Google OAuth config | `""` |
| `scia.todoistCredential`                           | Todoist credential ID injected into sessions    | `default.todoist` |
| `scia.userNamespace`                               | Optional fixed scia user namespace; empty derives it from each agentapi user | `""` |
| `scia.dynamicUserSecretNamePrefix`                 | Prefix for scia dynamic user token Secrets      | `scia-oauth-` |
| `scia.oauth.google.scopes`                         | Google OAuth scope choices exposed to the frontend and rendered into scia integration metadata | Calendar/Tasks defaults |
| `scia.oauth.todoist.scopes`                        | Todoist OAuth scope choices exposed to the frontend and rendered into scia integration metadata | Todoist defaults |
| `scia.oauth.google.secret.create`                  | Create a Kubernetes Secret for Google OAuth     | `false` |
| `scia.oauth.google.secret.existingSecret`          | Existing Secret containing Google OAuth values  | `""` |
| `scia.oauth.google.secret.clientIdKey`             | Secret key for the Google OAuth client ID       | `client-id` |
| `scia.oauth.google.secret.clientSecretKey`         | Secret key for the Google OAuth client secret   | `client-secret` |
| `scia.oauth.todoist.enabled`                       | Enable Todoist OAuth in scia                    | `false` |
| `scia.oauth.todoist.omitRedirectUrl`               | Omit Todoist redirect_uri from OAuth requests   | `false` |
| `scia.oauth.todoist.secret.create`                 | Create a Kubernetes Secret for Todoist OAuth    | `true` |
| `scia.oauth.todoist.secret.existingSecret`         | Existing Secret containing Todoist OAuth values | `""` |

### Application Configuration

| Name                                    | Description                               | Value     |
| --------------------------------------- | ----------------------------------------- | --------- |
| `config.enableMultipleUsers`            | Enable multi-user mode                   | `false`   |
| `config.auth.enabled`                   | Enable authentication                    | `false`   |

### Environment variables

| Name      | Description                           | Value |
| --------- | ------------------------------------- | ----- |
| `env`     | Environment variables as array        | `[]`  |
| `envFrom` | Environment variables from ConfigMaps/Secrets | `[]`  |

### Resource limits

| Name                   | Description                                   | Value     |
| ---------------------- | --------------------------------------------- | --------- |
| `resources.requests`   | The requested resources for the container     | `{"memory": "512Mi", "cpu": "500m"}` |
| `resources.limits`     | The resources limits for the container        | `{"memory": "2Gi", "cpu": "2000m"}` |

### Service Account parameters

| Name                           | Description                                                | Value  |
| ------------------------------ | ---------------------------------------------------------- | ------ |
| `serviceAccount.create`        | Specifies whether a service account should be created     | `true` |
| `serviceAccount.automount`     | Automatically mount a ServiceAccount's API credentials    | `true` |
| `serviceAccount.annotations`   | Annotations to add to the service account                 | `{}`   |
| `serviceAccount.name`          | The name of the service account to use                    | `""`   |

### Other parameters

| Name           | Description                  | Value |
| -------------- | ---------------------------- | ----- |
| `nodeSelector` | Node labels for pod assignment | `{}`  |
| `tolerations`  | Tolerations for pod assignment | `[]`  |
| `affinity`     | Affinity for pod assignment    | `{}`  |

## Configuration Examples

### Basic Installation

```bash
# From local chart
helm install agentapi-proxy ./helm/agentapi-proxy

# From OCI registry
helm install agentapi-proxy oci://ghcr.io/takutakahashi/charts/agentapi-proxy --version 0.1.0
```

### With Custom Values

```bash
# From local chart
helm install agentapi-proxy ./helm/agentapi-proxy \
  --set replicaCount=2

# From OCI registry
helm install agentapi-proxy oci://ghcr.io/takutakahashi/charts/agentapi-proxy \
  --version 0.1.0 \
  --set replicaCount=2
```

### With Ingress Enabled

```yaml
# values.yaml
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: agentapi.yourdomain.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: agentapi-tls
      hosts:
        - agentapi.yourdomain.com
```

```bash
# From local chart
helm install agentapi-proxy ./helm/agentapi-proxy -f values.yaml

# From OCI registry
helm install agentapi-proxy oci://ghcr.io/takutakahashi/charts/agentapi-proxy --version 0.1.0 -f values.yaml
```

### With scia Google OAuth

```yaml
# values.yaml
config:
  hostname: agentapi.yourdomain.com

scia:
  enabled: true
  publicBaseUrl: https://agentapi.yourdomain.com
  credential: your-user.google
  oauth:
    google:
      scopes:
        - id: calendar-read
          value: https://www.googleapis.com/auth/calendar.readonly
          name: Google Calendar read-only
          enabled: true
        - id: tasks-read
          value: https://www.googleapis.com/auth/tasks.readonly
          name: Google Tasks read-only
          enabled: true
      secret:
        create: false
        existingSecret: scia-google-oauth
        clientIdKey: client-id
        clientSecretKey: client-secret
```

The `scia-google-oauth` Secret must contain the Google OAuth client ID and client secret. `scia.oauth.google.scopes` is rendered into scia integration metadata, and frontends can request a selected subset by posting those scope IDs to `/integrations/{id}/authorization-url`. When `ingress.enabled=true`, the chart routes `/oauth` and `/_scia` to the scia OAuth broker on the same host.

### With scia Todoist OAuth

```yaml
scia:
  enabled: true
  publicBaseUrl: https://agentapi.yourdomain.com
  todoistCredential: default.todoist
  oauth:
    todoist:
      enabled: true
      scope: data:read_write
      redirectUrl: https://agentapi.yourdomain.com/api/oauth/todoist/callback
      omitRedirectUrl: false
      secret:
        create: false
        existingSecret: scia-todoist-oauth
        clientIdKey: client-id
        clientSecretKey: client-secret
```

The `scia-todoist-oauth` Secret must contain the Todoist OAuth client ID and client secret. Register the same `redirectUrl` in the Todoist app console. The session sidecar injects the Todoist token for `api.todoist.com/api/v1/*` requests by default.

### With Environment Variables

```yaml
# values.yaml
env:
  - name: CLAUDE_ARGS
    value: "--dangerously-skip-permissions"

envFrom:
  - secretRef:
      name: agentapi-secrets
  - configMapRef:
      name: agentapi-config
```

### With Role-based Environment Variables

AgentAPI Proxy supports loading different environment variables based on the authenticated user's role. This allows for fine-grained configuration per user type.

```yaml
# values.yaml
config:
  roleEnvFiles:
    enabled: true
    path: "/etc/role-env-files"
    loadDefault: true

# Simple mapping: filename -> secret configuration
roleEnvFiles:
  enabled: true
  files:
    "default.env":
      secretName: "agentapi-env-default"
      key: "default.env"
    "admin.env":
      secretName: "agentapi-env-admin"
      key: "admin.env"
    "developer.env":
      secretName: "agentapi-env-developer"
      key: "developer.env"
    "user.env":
      secretName: "agentapi-env-user"
      key: "user.env"
    # You can also map files with different names:
    "database.env":
      secretName: "db-config-secret"
      key: "production.env"
```

Create secrets for each role:

```bash
# Default environment variables (applied to all roles)
kubectl create secret generic agentapi-env-default \
  --from-literal=default.env="LOG_LEVEL=info
DB_HOST=postgresql.default.svc.cluster.local
DB_PORT=5432"

# Admin-specific environment variables
kubectl create secret generic agentapi-env-admin \
  --from-literal=admin.env="LOG_LEVEL=debug
ADMIN_ACCESS=true
SECRET_KEY=admin-secret-123"

# Developer-specific environment variables
kubectl create secret generic agentapi-env-developer \
  --from-literal=developer.env="LOG_LEVEL=debug
DEV_ACCESS=true
FEATURE_FLAGS=dev,staging"

# User-specific environment variables
kubectl create secret generic agentapi-env-user \
  --from-literal=user.env="USER_ACCESS=true
FEATURE_FLAGS=production
API_RATE_LIMIT=100"
```

#### Flexible File Mapping

The new configuration format allows you to map any filename to any secret and key:

```yaml
roleEnvFiles:
  enabled: true
  files:
    # Standard role files
    "default.env":
      secretName: "common-config"
      key: "default.env"
    "admin.env":
      secretName: "admin-secrets"
      key: "admin-config"
    
    # Custom files from different secrets
    "database.env":
      secretName: "db-config"
      key: "production.env"
    "api-keys.env":
      secretName: "third-party-secrets"
      key: "api-credentials"
    "monitoring.env":
      secretName: "observability-config"
      key: "metrics.env"
```

This creates files in `/etc/role-env-files/`:
- `default.env` (from `common-config` secret, key `default.env`)
- `admin.env` (from `admin-secrets` secret, key `admin-config`)
- `database.env` (from `db-config` secret, key `production.env`)
- `api-keys.env` (from `third-party-secrets` secret, key `api-credentials`)
- `monitoring.env` (from `observability-config` secret, key `metrics.env`)

See [values-role-env-example.yaml](values-role-env-example.yaml) for a complete example with all secrets.

### GitHub App Repository Restriction

GitHub App installation tokens can be restricted to the repository being cloned or synced:

```yaml
github:
  app:
    repositoryRestriction: true
```

The default is `false`. When enabled, token generation requires a repository full name and sends the repository name in the GitHub installation token request body.

### Create Required Secrets

```bash
# Create secret for GitHub authentication
kubectl create secret generic agentapi-secrets \
  --from-literal=GITHUB_TOKEN=your-github-token \
  --from-literal=API_KEYS=key1,key2,key3

# For S3 with access keys (not recommended for production)
kubectl create secret generic agentapi-s3-credentials \
  --from-literal=access-key=your-access-key \
  --from-literal=secret-key=your-secret-key
```

## Health Checks

The API probes default to `/health`; session-manager probes use `/livez` and
`/readyz`. They can be customized in values.yaml.

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: http
  initialDelaySeconds: 30
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /health
    port: http
  initialDelaySeconds: 10
  periodSeconds: 5
```

## Scaling

The chart uses a Deployment. Single-replica operation does not require Redis.
For multiple API replicas, enable the bundled Redis or configure
`api.redis.addr`:

```bash
# Scale to 3 replicas (local chart)
helm upgrade agentapi-proxy ./helm/agentapi-proxy \
  --set api.replicaCount=3 \
  --set redis.enabled=true

# Scale to 3 replicas (OCI registry)
helm upgrade agentapi-proxy oci://ghcr.io/takutakahashi/charts/agentapi-proxy \
  --version 0.1.0 \
  --set api.replicaCount=3 \
  --set redis.enabled=true
```

To route runtime prompts, cancellation, and session events through outbound-only
session HTTPS long polling, enable session control together with Redis:

```bash
helm upgrade agentapi-proxy ./helm/agentapi-proxy \
  --set redis.enabled=true \
  --set sessionManager.sessionControl.enabled=true
```

Only backend pods connect to Redis. Session pods receive the backend control-plane
URL and provisioner token, but no Redis address or credentials. See
[`docs/session-control-long-poll.md`](../../docs/session-control-long-poll.md) for
the transport and retention model.

For ESM sessions, enable the direct Session Pod-to-parent runtime transport with
`--set sessionManager.sessionControl.directRuntimeEnabled=true`. This requires
`sessionManager.sessionControl.enabled=true` and Redis. See
[`docs/direct-session-runtime.md`](../../docs/direct-session-runtime.md).

## Troubleshooting

### Check Deployment status
```bash
kubectl get deployment
kubectl describe deployment agentapi-proxy
```

### Check pod logs
```bash
kubectl logs agentapi-proxy-0
kubectl logs -f agentapi-proxy-0  # Follow logs
```

### Check persistent volumes
```bash
kubectl get pvc
kubectl describe pvc data-agentapi-proxy-0
```

### Port forward for local access
```bash
kubectl port-forward agentapi-proxy-0 8080:8080
```

## Security Considerations

1. **Secrets Management**: Use Kubernetes Secrets for sensitive data like GitHub tokens
2. **RBAC**: The chart creates a ServiceAccount - configure RBAC as needed
3. **Network Policies**: Consider implementing network policies for production
4. **Image Security**: Use specific image tags and scan for vulnerabilities

## Upgrading

To upgrade an existing release:

```bash
# Upgrade from local chart
helm upgrade agentapi-proxy ./helm/agentapi-proxy

# Upgrade from OCI registry
helm upgrade agentapi-proxy oci://ghcr.io/takutakahashi/charts/agentapi-proxy --version 0.1.0
```

## Stable control-plane Service

The chart creates `control` as a release-independent endpoint for session
provisioners. It selects the API only in deprecated direct-session mode and
selects the dedicated session-manager whenever that role is enabled. The
Service is retained when its creating release is
uninstalled, allowing a blue/green deployment to move its selector to the new
proxy without recreating existing session workloads.

Install the shadow release without trying to create the shared Service or
fixed-name session RBAC:

```yaml
controlPlaneService:
  create: false

kubernetesSession:
  serviceAccountName: agentapi-proxy-session
  rbac:
    create: false
```

An explicit `kubernetesSession.provisioner.proxyUrl` takes precedence over the
stable Service. Set `controlPlaneService.enabled=false` to retain the previous
release-local Service behavior for new sessions.

## Values File Example

### Basic Setup
```yaml
replicaCount: 2

image:
  tag: "1.23.0"

service:
  type: LoadBalancer

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: agentapi.example.com
      paths:
        - path: /
          pathType: Prefix

env:
  - name: CLAUDE_ARGS
    value: "--dangerously-skip-permissions"

envFrom:
  - secretRef:
      name: agentapi-secrets

resources:
  requests:
    memory: 1Gi
    cpu: 1000m
  limits:
    memory: 4Gi
    cpu: 4000m
```
