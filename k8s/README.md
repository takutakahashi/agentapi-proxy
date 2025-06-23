# Kubernetes Manifests for AgentAPI Proxy

This directory contains Kubernetes manifests for deploying the AgentAPI Proxy application.

## Prerequisites

- Kubernetes cluster (1.21+)
- kubectl configured to access your cluster
- (Optional) kustomize for manifest management
- (Optional) cert-manager for automatic TLS certificate management

## Structure

```
k8s/
└── base/
    ├── configmap.yaml      # Application configuration
    ├── secret.yaml         # Sensitive data (GitHub tokens, API keys)
    ├── pvc.yaml           # Persistent storage for sessions
    ├── deployment.yaml     # Main application deployment
    ├── service.yaml        # Internal service
    ├── ingress.yaml       # External access configuration
    └── kustomization.yaml  # Kustomize configuration
```

## Configuration

### 1. Update Secret (Required)

Edit `k8s/base/secret.yaml` and configure authentication:

```yaml
stringData:
  # Option 1: GitHub Personal Access Token
  GITHUB_TOKEN: "your-actual-github-pat"
  
  # Option 2: GitHub App (uncomment and fill)
  # GITHUB_APP_ID: "your-app-id"
  # GITHUB_APP_PEM: |
  #   -----BEGIN RSA PRIVATE KEY-----
  #   your-private-key-here
  #   -----END RSA PRIVATE KEY-----
  
  # API Keys for authentication
  API_KEYS: "your-api-key-1,your-api-key-2"
```

### 2. Update ConfigMap (Optional)

Modify `k8s/base/configmap.yaml` to adjust:
- `start_port`: Starting port for spawned agentapi instances
- Authentication configuration
- Persistence settings

### 3. Update Ingress (Required for external access)

Edit `k8s/base/ingress.yaml`:
- Replace `agentapi.example.com` with your actual domain
- Adjust annotations based on your ingress controller

### 4. Storage Class (Optional)

The PVC uses `standard` storage class. Update if your cluster uses a different default:

```yaml
storageClassName: your-storage-class
```

## Deployment

### Using kubectl

```bash
# Deploy all resources
kubectl apply -f k8s/base/

# Or deploy individually
kubectl apply -f k8s/base/configmap.yaml
kubectl apply -f k8s/base/secret.yaml
kubectl apply -f k8s/base/pvc.yaml
kubectl apply -f k8s/base/deployment.yaml
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
# Check deployment status
kubectl get deployment agentapi-proxy

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

The current setup runs a single replica. For production:

1. **Session Persistence**: The file-based persistence doesn't support multiple replicas. Consider:
   - Using external storage (Redis, PostgreSQL)
   - Implementing session affinity
   - Using a shared filesystem (NFS, EFS)

2. **Port Range**: Each spawned agentapi instance needs a unique port. With multiple replicas:
   - Use a headless service for direct pod access
   - Implement port coordination across replicas
   - Consider using a service mesh

## Security Notes

1. **Never commit real secrets** to version control
2. Use sealed-secrets or external secret management (Vault, AWS Secrets Manager)
3. Enable RBAC and network policies in production
4. Regular image updates for security patches

## Troubleshooting

### Pod not starting
```bash
kubectl describe pod -l app=agentapi-proxy
kubectl logs -l app=agentapi-proxy
```

### Permission issues
- Ensure PVC is properly bound
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
  - deployment-patch.yaml

configMapGenerator:
  - name: agentapi-proxy-config
    behavior: merge
    literals:
      - CLAUDE_ARGS="--model claude-3-5-sonnet"
```