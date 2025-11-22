#!/bin/bash
# Deploy codec server to Kubernetes
set -euo pipefail

NAMESPACE="${NAMESPACE:-temporal}"
ENVIRONMENT="${ENVIRONMENT:-dev}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "🚀 Deploying Temporal Codec Server"
echo "   Namespace: ${NAMESPACE}"
echo "   Environment: ${ENVIRONMENT}"
echo ""

# Check prerequisites
if ! command -v kubectl &> /dev/null; then
    echo "❌ kubectl not found. Please install kubectl first."
    exit 1
fi

# Create namespace if it doesn't exist
if ! kubectl get namespace "${NAMESPACE}" &> /dev/null; then
    echo "📦 Creating namespace: ${NAMESPACE}"
    kubectl create namespace "${NAMESPACE}"
fi

# Deploy based on environment
case "${ENVIRONMENT}" in
    dev)
        echo "🔧 Deploying development configuration..."
        echo "   - No TLS"
        echo "   - No API key"
        echo "   - Single replica"

        # Apply base deployment without secrets
        kubectl apply -f "${PROJECT_ROOT}/k8s/codec-server-deployment.yaml" \
            --namespace="${NAMESPACE}"

        # Patch for dev environment
        kubectl patch deployment codec-server -n "${NAMESPACE}" --type='json' -p='[
            {"op": "replace", "path": "/spec/replicas", "value": 1},
            {"op": "replace", "path": "/spec/template/spec/containers/0/env", "value": [
                {"name": "S3_BUCKET", "value": "temporal-large-payloads-dev"},
                {"name": "S3_REGION", "value": "us-east-1"},
                {"name": "LARGE_PAYLOAD_THRESHOLD", "value": "2097152"},
                {"name": "GRPC_PORT", "value": "9090"},
                {"name": "HTTP_PORT", "value": "8080"}
            ]}
        ]'
        ;;

    staging)
        echo "⚙️  Deploying staging configuration..."
        echo "   - TLS enabled"
        echo "   - API key required"
        echo "   - 2 replicas"

        # Check if secrets exist
        if ! kubectl get secret codec-server-tls -n "${NAMESPACE}" &> /dev/null; then
            echo "❌ TLS secret not found. Please create codec-server-tls secret first."
            echo "   See k8s/codec-server-secrets.yaml for template."
            exit 1
        fi

        if ! kubectl get secret codec-server-api-key -n "${NAMESPACE}" &> /dev/null; then
            echo "❌ API key secret not found. Please create codec-server-api-key secret first."
            echo "   See k8s/codec-server-secrets.yaml for template."
            exit 1
        fi

        kubectl apply -f "${PROJECT_ROOT}/k8s/codec-server-deployment.yaml" \
            --namespace="${NAMESPACE}"
        kubectl apply -f "${PROJECT_ROOT}/k8s/codec-server-secrets.yaml" \
            --namespace="${NAMESPACE}"

        # Patch for staging
        kubectl patch deployment codec-server -n "${NAMESPACE}" --type='json' -p='[
            {"op": "replace", "path": "/spec/replicas", "value": 2}
        ]'
        ;;

    prod|production)
        echo "🏭 Deploying production configuration..."
        echo "   - TLS enabled (required)"
        echo "   - API key required"
        echo "   - 3 replicas"
        echo "   - HPA enabled"
        echo "   - PDB enabled"

        # Strict validation for production
        if ! kubectl get secret codec-server-tls -n "${NAMESPACE}" &> /dev/null; then
            echo "❌ TLS secret not found. Production deployment requires TLS."
            exit 1
        fi

        if ! kubectl get secret codec-server-api-key -n "${NAMESPACE}" &> /dev/null; then
            echo "❌ API key secret not found. Production deployment requires authentication."
            exit 1
        fi

        # Verify S3 bucket configuration
        read -p "Have you created the S3 bucket? (y/n) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            echo "❌ Please create S3 bucket first. See k8s/README.md for instructions."
            exit 1
        fi

        # Deploy all resources
        kubectl apply -f "${PROJECT_ROOT}/k8s/codec-server-deployment.yaml" \
            --namespace="${NAMESPACE}"
        kubectl apply -f "${PROJECT_ROOT}/k8s/codec-server-secrets.yaml" \
            --namespace="${NAMESPACE}"

        echo "✅ Production deployment initiated"
        ;;

    *)
        echo "❌ Unknown environment: ${ENVIRONMENT}"
        echo "   Valid options: dev, staging, prod"
        exit 1
        ;;
esac

# Wait for deployment to be ready
echo ""
echo "⏳ Waiting for codec-server deployment to be ready..."
kubectl rollout status deployment/codec-server -n "${NAMESPACE}" --timeout=5m

# Get deployment info
echo ""
echo "📊 Deployment Status:"
kubectl get deployment codec-server -n "${NAMESPACE}"
kubectl get pods -n "${NAMESPACE}" -l app=codec-server

# Show service endpoints
echo ""
echo "🔗 Service Endpoints:"
kubectl get svc -n "${NAMESPACE}" -l app=codec-server

# Test health endpoint
echo ""
echo "🏥 Testing health endpoint..."
POD_NAME=$(kubectl get pods -n "${NAMESPACE}" -l app=codec-server -o jsonpath='{.items[0].metadata.name}')
if kubectl exec -n "${NAMESPACE}" "${POD_NAME}" -- wget -q -O- http://localhost:8080/health; then
    echo ""
    echo "✅ Health check passed!"
else
    echo ""
    echo "⚠️  Health check failed. Check pod logs:"
    echo "   kubectl logs -n ${NAMESPACE} ${POD_NAME}"
fi

echo ""
echo "✅ Codec server deployment complete!"
echo ""
echo "Next steps:"
echo "1. Deploy cleanup worker: ./scripts/deploy-cleanup-worker.sh"
echo "2. Deploy monitoring: ./scripts/deploy-monitoring.sh"
echo "3. Run load tests: k6 run tests/load-test.js"
echo "4. Configure workers to use codec server"
