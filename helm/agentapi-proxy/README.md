# AgentAPI Proxy Helm Chart

A Helm chart for deploying AgentAPI Proxy - a reverse proxy and process manager for agentapi server instances on Kubernetes.

## Prerequisites

- Kubernetes 1.21+
- Helm 3.0+
- Persistent volume provisioner support in the underlying infrastructure (if persistence is enabled)

## Installing the Chart

To install the chart with the release name `my-agentapi-proxy`:

```bash
helm install my-agentapi-proxy ./helm/agentapi-proxy
```

The command deploys AgentAPI Proxy on the Kubernetes cluster in the default configuration. The [Parameters](#parameters) section lists the parameters that can be configured during installation.

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
helm install agentapi-proxy ./helm/agentapi-proxy
```

### With Custom Values

```bash
helm install agentapi-proxy ./helm/agentapi-proxy \
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
helm install agentapi-proxy ./helm/agentapi-proxy -f values.yaml
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

### Create Required Secrets

```bash
# Create secret for GitHub authentication
kubectl create secret generic agentapi-secrets \
  --from-literal=GITHUB_TOKEN=your-github-token \
  --from-literal=API_KEYS=key1,key2,key3
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
# Scale to 3 replicas
helm upgrade agentapi-proxy ./helm/agentapi-proxy --set replicaCount=3
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
helm upgrade agentapi-proxy ./helm/agentapi-proxy
```

## Values File Example

```yaml
replicaCount: 2

image:
  tag: "0.18.0"

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