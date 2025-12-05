#!/bin/bash

# Deploy script for Kubernetes mode with mock agent
set -e

echo "================================================"
echo "Deploying AgentAPI Proxy with K8s + Mock Agent"
echo "================================================"

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "Error: kubectl is not installed or not in PATH"
    exit 1
fi

# Function to check Kubernetes connectivity
check_k8s_connection() {
    echo "Checking Kubernetes cluster connection..."
    if kubectl cluster-info &> /dev/null; then
        echo "✓ Connected to Kubernetes cluster"
        kubectl cluster-info | head -n 1
    else
        echo "✗ Cannot connect to Kubernetes cluster"
        echo "Please ensure kubectl is configured correctly"
        exit 1
    fi
}

# Function to build mock agent image (optional)
build_mock_agent() {
    echo ""
    echo "Building mock agent Docker image..."
    if command -v docker &> /dev/null; then
        docker build -f Dockerfile.mock-agent -t agentapi/mock-agent:latest .
        echo "✓ Mock agent image built successfully"
    else
        echo "⚠ Docker not found, skipping image build"
        echo "  You'll need to ensure the image exists or use a public one"
    fi
}

# Function to deploy to Kubernetes
deploy_to_k8s() {
    echo ""
    echo "Deploying to Kubernetes..."
    
    # Apply the deployment manifest
    kubectl apply -f k8s-mock-deployment.yaml
    
    echo "✓ Deployment applied successfully"
    echo ""
    echo "Waiting for pods to be ready..."
    
    # Wait for deployment to be ready
    kubectl wait --for=condition=available --timeout=300s \
        deployment/agentapi-proxy -n agentapi-proxy 2>/dev/null || true
    
    # Show pod status
    echo ""
    echo "Pod Status:"
    kubectl get pods -n agentapi-proxy
}

# Function to show access information
show_access_info() {
    echo ""
    echo "================================================"
    echo "Deployment Complete!"
    echo "================================================"
    echo ""
    echo "Access Information:"
    echo "-------------------"
    
    # Get service information
    echo "1. ClusterIP Service:"
    kubectl get svc agentapi-proxy -n agentapi-proxy
    
    echo ""
    echo "2. NodePort Service:"
    kubectl get svc agentapi-proxy-nodeport -n agentapi-proxy
    
    # Get NodePort
    NODE_PORT=$(kubectl get svc agentapi-proxy-nodeport -n agentapi-proxy -o jsonpath='{.spec.ports[0].nodePort}')
    echo ""
    echo "   Access URL: http://<NODE_IP>:${NODE_PORT}"
    
    # If running in minikube
    if command -v minikube &> /dev/null && minikube status &> /dev/null; then
        MINIKUBE_IP=$(minikube ip)
        echo "   Minikube URL: http://${MINIKUBE_IP}:${NODE_PORT}"
    fi
    
    echo ""
    echo "3. Port Forward (for local testing):"
    echo "   kubectl port-forward -n agentapi-proxy svc/agentapi-proxy 8080:8080"
    echo "   Then access: http://localhost:8080"
    
    echo ""
    echo "4. View logs:"
    echo "   kubectl logs -n agentapi-proxy -l app=agentapi-proxy -f"
    
    echo ""
    echo "5. Check metrics:"
    echo "   kubectl port-forward -n agentapi-proxy svc/agentapi-proxy 9090:9090"
    echo "   Then access: http://localhost:9090/metrics"
}

# Function to test the deployment
test_deployment() {
    echo ""
    echo "================================================"
    echo "Testing Deployment"
    echo "================================================"
    
    # Set up port-forward in background
    echo "Setting up port-forward for testing..."
    kubectl port-forward -n agentapi-proxy svc/agentapi-proxy 8080:8080 &
    PF_PID=$!
    sleep 3
    
    echo "Running basic tests..."
    
    # Test health endpoint
    echo -n "1. Testing health endpoint: "
    if curl -s http://localhost:8080/health > /dev/null 2>&1; then
        echo "✓ Success"
    else
        echo "✗ Failed"
    fi
    
    # Test readiness endpoint
    echo -n "2. Testing readiness endpoint: "
    if curl -s http://localhost:8080/ready > /dev/null 2>&1; then
        echo "✓ Success"
    else
        echo "✗ Failed"
    fi
    
    # Clean up port-forward
    kill $PF_PID 2>/dev/null || true
    
    echo ""
    echo "Testing complete!"
}

# Function to clean up deployment
cleanup() {
    echo ""
    echo "Cleaning up deployment..."
    kubectl delete -f k8s-mock-deployment.yaml 2>/dev/null || true
    echo "✓ Cleanup complete"
}

# Main execution
main() {
    case "${1:-deploy}" in
        deploy)
            check_k8s_connection
            build_mock_agent
            deploy_to_k8s
            show_access_info
            ;;
        test)
            test_deployment
            ;;
        cleanup|clean)
            cleanup
            ;;
        all)
            check_k8s_connection
            build_mock_agent
            deploy_to_k8s
            show_access_info
            test_deployment
            ;;
        *)
            echo "Usage: $0 [deploy|test|cleanup|all]"
            echo "  deploy  - Deploy to Kubernetes (default)"
            echo "  test    - Test the deployment"
            echo "  cleanup - Remove the deployment"
            echo "  all     - Deploy and test"
            exit 1
            ;;
    esac
}

# Run main function with arguments
main "$@"