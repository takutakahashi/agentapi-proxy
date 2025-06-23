# Kubernetes Manifests for AgentAPI Proxy

This directory contains Kubernetes manifests for deploying the AgentAPI Proxy application using StatefulSet.

## Prerequisites

- Kubernetes cluster (1.21+)
- kubectl configured to access your cluster
- (Optional) kustomize for manifest management
- (Optional) cert-manager for automatic TLS certificate management

## Structure

```
k8s/
└── base/
    ├── pvc.yaml           # Persistent storage for sessions
    ├── statefulset.yaml   # Main application StatefulSet
    ├── service.yaml       # Internal service
    ├── ingress.yaml       # External access configuration
    └── kustomization.yaml # Kustomize configuration
```

## Configuration

### 1. Environment Variables (Configure as needed)

The StatefulSet template includes environment variable placeholders. Configure these externally using:
- Kubernetes ConfigMaps
- Kubernetes Secrets
- Environment variable injection tools
- Container orchestration platforms

Common environment variables:
- `GITHUB_TOKEN`: GitHub Personal Access Token
- `GITHUB_APP_ID`, `GITHUB_APP_PEM_PATH`: GitHub App credentials
- `CLAUDE_ARGS`: Claude CLI arguments
- `API_KEYS`: Authentication API keys

### 2. Update Ingress (Required for external access)

Edit `k8s/base/ingress.yaml`:
- Replace `agentapi.example.com` with your actual domain
- Adjust annotations based on your ingress controller

### 3. Storage Class (Optional)

The StatefulSet uses `standard` storage class. Update if your cluster uses a different default:

```yaml
storageClassName: your-storage-class
```

## Deployment

### Using kubectl

```bash
# Deploy all resources
kubectl apply -f k8s/base/

# Or deploy individually
kubectl apply -f k8s/base/pvc.yaml
kubectl apply -f k8s/base/statefulset.yaml
kubectl apply -f k8s/base/service.yaml
kubectl apply -f k8s/base/ingress.yaml
```

### Using kustomize

```bash
# Preview changes
kubectl kustomize k8s/base/

# Apply
kubectl apply -k k8s/base/
```

## Verification

```bash
# Check StatefulSet status
kubectl get statefulset agentapi-proxy

# Check pod status
kubectl get pods -l app=agentapi-proxy

# Check logs
kubectl logs -l app=agentapi-proxy

# Check service
kubectl get service agentapi-proxy

# Check ingress
kubectl get ingress agentapi-proxy
```

## Access

### Internal Access (within cluster)
```
http://agentapi-proxy:8080
```

### External Access
```
https://agentapi.example.com  # Replace with your domain
```

## Scaling Considerations

The StatefulSet provides stable network identities and persistent storage:

1. **Session Persistence**: Each replica gets its own persistent volume
   - Sessions are isolated per pod
   - No shared state between replicas
   - Scale by increasing replica count

2. **Port Range**: Each spawned agentapi instance needs a unique port:
   - Use headless service for direct pod access
   - Pods have predictable names (agentapi-proxy-0, agentapi-proxy-1, etc.)
   - Consider using a service mesh for advanced routing

## Security Notes

1. **Environment Variables**: Configure sensitive data using Kubernetes Secrets
2. **RBAC**: Enable RBAC and network policies in production
3. **Image Updates**: Regular image updates for security patches
4. **Network Policies**: Restrict pod-to-pod communication as needed

## Troubleshooting

### StatefulSet not starting
```bash
kubectl describe statefulset agentapi-proxy
kubectl describe pod -l app=agentapi-proxy
kubectl logs -l app=agentapi-proxy
```

### Permission issues
- Ensure persistent volumes are properly bound
- Check security context matches image requirements

### Health check failures
- Verify `/health` endpoint is accessible
- Check resource limits aren't too restrictive

## Customization for Environments

Create overlay directories for different environments:

```
k8s/
├── base/
└── overlays/
    ├── dev/
    │   └── kustomization.yaml
    ├── staging/
    │   └── kustomization.yaml
    └── prod/
        └── kustomization.yaml
```

Example overlay (k8s/overlays/prod/kustomization.yaml):
```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

bases:
  - ../../base

patchesStrategicMerge:
  - statefulset-patch.yaml

patchesJson6902:
  - target:
      group: apps
      version: v1
      kind: StatefulSet
      name: agentapi-proxy
    patch: |-
      - op: add
        path: /spec/template/spec/containers/0/env/-
        value:
          name: CLAUDE_ARGS
          value: "--model claude-3-5-sonnet"
```