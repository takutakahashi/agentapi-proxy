# Kubernetes Mode with Mock Agent Configuration

This configuration sets up AgentAPI Proxy in Kubernetes mode with a mock agent provider for testing and development.

## Files Created

1. **config.k8s-mock.yaml** - Complete YAML configuration file
2. **config.k8s-mock.env** - Environment variables configuration
3. **k8s-mock-deployment.yaml** - Kubernetes deployment manifest
4. **Dockerfile.mock-agent** - Mock agent container image
5. **deploy-k8s-mock.sh** - Deployment script

## Quick Start

### 1. Using the deployment script:
```bash
# Deploy to Kubernetes
./deploy-k8s-mock.sh deploy

# Test the deployment
./deploy-k8s-mock.sh test

# Deploy and test
./deploy-k8s-mock.sh all

# Clean up
./deploy-k8s-mock.sh cleanup
```

### 2. Manual deployment:
```bash
# Apply Kubernetes manifests
kubectl apply -f k8s-mock-deployment.yaml

# Check deployment status
kubectl get pods -n agentapi-proxy

# Access via port-forward
kubectl port-forward -n agentapi-proxy svc/agentapi-proxy 8080:8080
```

### 3. Using environment variables:
```bash
# Load environment configuration
source config.k8s-mock.env

# Run agentapi-proxy with environment config
agentapi-proxy
```

## Configuration Features

### Kubernetes Integration
- **Namespace**: `agentapi-proxy`
- **Session Storage**: ConfigMaps with prefix `agentapi-session-`
- **Service Account**: RBAC configured for pod/configmap management
- **Resource Limits**: Configured for both proxy and agent pods

### Mock Agent
- **Provider**: Mock agent for testing
- **Capabilities**: Simulates code execution, file operations, web search
- **Response Patterns**: Configurable pattern-based responses
- **Health Checks**: Built-in health endpoint on port 8000

### Services Exposed
- **Main API**: Port 8080 (HTTP)
- **Health Check**: Port 8081 (health/readiness probes)
- **Metrics**: Port 9090 (Prometheus metrics)
- **NodePort**: 30080 (external access)

### Observability
- **Logging**: JSON formatted, configurable level
- **Metrics**: Prometheus-compatible metrics endpoint
- **Health Probes**: Liveness and readiness checks
- **Annotations**: Prometheus scraping configured

## Testing

### Health Check:
```bash
curl http://localhost:8080/health
curl http://localhost:8080/ready
```

### Metrics:
```bash
curl http://localhost:9090/metrics
```

### Mock Agent Response:
```bash
# Test mock agent patterns
curl -X POST http://localhost:8080/agent/execute \
  -H "Content-Type: application/json" \
  -d '{"command": "hello"}'

curl -X POST http://localhost:8080/agent/execute \
  -H "Content-Type: application/json" \
  -d '{"command": "status"}'
```

## Customization

### Modify Mock Responses
Edit the `config.yaml` in the ConfigMap or update `config.k8s-mock.yaml`:
```yaml
mock:
  responses:
    - pattern: "your_pattern"
      response: "Your custom response"
```

### Scale Replicas
```bash
kubectl scale deployment agentapi-proxy -n agentapi-proxy --replicas=3
```

### Update Resource Limits
Edit the deployment manifest and adjust the resources section for your needs.

## Monitoring

### View Logs:
```bash
kubectl logs -n agentapi-proxy -l app=agentapi-proxy -f
```

### Watch Pods:
```bash
kubectl get pods -n agentapi-proxy -w
```

### Check Events:
```bash
kubectl get events -n agentapi-proxy --sort-by='.lastTimestamp'
```

## Troubleshooting

### Pod not starting:
```bash
kubectl describe pod -n agentapi-proxy <pod-name>
kubectl logs -n agentapi-proxy <pod-name> --previous
```

### Connection refused:
- Check service endpoints: `kubectl get endpoints -n agentapi-proxy`
- Verify port-forward: `netstat -tulpn | grep 8080`

### Permission errors:
- Check RBAC: `kubectl auth can-i --list --as=system:serviceaccount:agentapi-proxy:agentapi-proxy`

## Production Considerations

For production use:
1. Enable authentication in the config
2. Use real agent provider instead of mock
3. Configure proper resource limits
4. Set up persistent volume for session data
5. Configure TLS/SSL termination
6. Implement proper secrets management
7. Set up monitoring and alerting