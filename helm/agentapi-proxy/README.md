# AgentAPI Proxy Helm Chart

A Helm chart for deploying AgentAPI Proxy - a reverse proxy and process manager for agentapi server instances on Kubernetes.

## Prerequisites

- Kubernetes 1.21+
- Helm 3.0+
- Persistent volume provisioner support in the underlying infrastructure (if persistence is enabled)

## Installing the Chart

### From Local Chart

To install the chart with the release name `my-agentapi-proxy`:

```bash
helm install my-agentapi-proxy ./helm/agentapi-proxy
```

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
| `image.repository`  | AgentAPI Proxy image repository   | `ghcr.io/takutakahashi/agentapi-proxy`     |
| `image.tag`         | AgentAPI Proxy image tag          | `0.18.0`                                   |
| `image.pullPolicy`  | AgentAPI Proxy image pull policy  | `IfNotPresent`                             |

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
| `service.agentapiPort`| AgentAPI instances starting port         | `9000`      |

### Ingress parameters

| Name                       | Description                                        | Value                    |
| -------------------------- | -------------------------------------------------- | ------------------------ |
| `ingress.enabled`          | Enable ingress record generation                   | `false`                  |
| `ingress.className`        | IngressClass that will be used                     | `nginx`                  |
| `ingress.annotations`      | Additional annotations for the Ingress resource   | `{}`                     |
| `ingress.hosts`            | An array with hosts and paths                      | `[{"host": "agentapi.example.com", "paths": [{"path": "/", "pathType": "Prefix"}]}]` |
| `ingress.tls`              | TLS configuration for ingress                      | `[]`                     |

### Persistence parameters

| Name                         | Description                                   | Value            |
| ---------------------------- | --------------------------------------------- | ---------------- |
| `persistence.enabled`        | Enable persistence using StatefulSet         | `true`           |
| `persistence.storageClassName` | Storage class name for the persistent volume | `standard`       |
| `persistence.accessMode`     | Access mode for the persistent volume        | `ReadWriteOnce`  |
| `persistence.size`           | Size of the persistent volume                 | `10Gi`           |

### Application Configuration

| Name                                    | Description                               | Value     |
| --------------------------------------- | ----------------------------------------- | --------- |
| `config.enableMultipleUsers`            | Enable multi-user mode                   | `false`   |
| `config.persistence.enabled`            | Enable session persistence               | `false`   |
| `config.persistence.backend`            | Persistence backend (file/memory/s3)     | `"file"`  |
| `config.persistence.filePath`           | File path for file backend               | `"./sessions.json"` |
| `config.persistence.syncIntervalSeconds`| Sync interval for file backend           | `30`      |
| `config.persistence.encryptSensitiveData`| Encrypt sensitive data                  | `true`    |
| `config.persistence.sessionRecoveryMaxAgeHours`| Max age for session recovery    | `24`      |
| `config.persistence.s3.bucket`          | S3 bucket name                           | `""`      |
| `config.persistence.s3.region`          | S3 region                                | `"us-east-1"` |
| `config.persistence.s3.prefix`          | S3 object prefix                         | `"sessions/"` |
| `config.persistence.s3.endpoint`        | Custom S3 endpoint (MinIO etc.)          | `""`      |
| `config.persistence.s3.accessKey`       | S3 access key (use IAM roles instead)    | `""`      |
| `config.persistence.s3.secretKey`       | S3 secret key (use IAM roles instead)    | `""`      |
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
  --set image.tag=latest \
  --set replicaCount=2 \
  --set persistence.size=20Gi

# From OCI registry
helm install agentapi-proxy oci://ghcr.io/takutakahashi/charts/agentapi-proxy \
  --version 0.1.0 \
  --set image.tag=latest \
  --set replicaCount=2 \
  --set persistence.size=20Gi
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

### With S3 Persistence

```yaml
# values.yaml
config:
  persistence:
    enabled: true
    backend: "s3"
    s3:
      bucket: "agentapi-sessions"
      region: "us-west-2"
      prefix: "sessions/"
      # For IAM Role authentication (recommended)
      # Leave accessKey and secretKey empty
      accessKey: ""
      secretKey: ""

# For EKS with IRSA (IAM Roles for Service Accounts)
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::ACCOUNT-ID:role/agentapi-s3-role
```

```bash
# Install with S3 persistence
helm install agentapi-proxy ./helm/agentapi-proxy -f values.yaml
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

The chart includes liveness and readiness probes that check the `/health` endpoint. These can be customized in values.yaml:

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

The chart uses StatefulSet for better session persistence. Each replica gets its own persistent volume:

```bash
# Scale to 3 replicas (local chart)
helm upgrade agentapi-proxy ./helm/agentapi-proxy --set replicaCount=3

# Scale to 3 replicas (OCI registry)
helm upgrade agentapi-proxy oci://ghcr.io/takutakahashi/charts/agentapi-proxy --version 0.1.0 --set replicaCount=3
```

## Troubleshooting

### Check StatefulSet status
```bash
kubectl get statefulset
kubectl describe statefulset agentapi-proxy
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

## Values File Example

### Basic Setup with File Persistence
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

persistence:
  enabled: true
  size: 20Gi

config:
  persistence:
    enabled: true
    backend: "file"

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

### Setup with S3 Persistence (EKS + IRSA)
```yaml
replicaCount: 1

image:
  tag: "1.23.0"

serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/agentapi-s3-role

config:
  persistence:
    enabled: true
    backend: "s3"
    s3:
      bucket: "my-agentapi-sessions"
      region: "us-west-2"
      prefix: "sessions/"
      # IAM role authentication - no keys needed
      accessKey: ""
      secretKey: ""

# No persistent volume needed for S3
persistence:
  enabled: false

env:
  - name: CLAUDE_ARGS
    value: "--dangerously-skip-permissions"

resources:
  requests:
    memory: 512Mi
    cpu: 500m
  limits:
    memory: 2Gi
    cpu: 2000m
```