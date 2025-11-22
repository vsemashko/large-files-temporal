#!/bin/bash
# Deploy monitoring configuration to Kubernetes
set -euo pipefail

NAMESPACE="${NAMESPACE:-temporal}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "🔍 Deploying monitoring configuration to namespace: ${NAMESPACE}"

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "❌ kubectl not found. Please install kubectl first."
    exit 1
fi

# Check if namespace exists
if ! kubectl get namespace "${NAMESPACE}" &> /dev/null; then
    echo "⚠️  Namespace ${NAMESPACE} does not exist. Creating..."
    kubectl create namespace "${NAMESPACE}"
fi

# Deploy Prometheus rules
echo "📊 Deploying Prometheus alerting rules..."
kubectl apply -f "${PROJECT_ROOT}/monitoring/prometheus-rules.yaml"

# Check if Prometheus Operator is installed
if kubectl get crd prometheusrules.monitoring.coreos.com &> /dev/null; then
    echo "📊 Prometheus Operator detected. Creating PrometheusRule resource..."

    cat <<EOF | kubectl apply -f -
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: codec-server-alerts
  namespace: ${NAMESPACE}
  labels:
    prometheus: kube-prometheus
    role: alert-rules
spec:
  groups:
$(kubectl get configmap prometheus-codec-server-rules -n ${NAMESPACE} -o jsonpath='{.data.codec-server\.rules}' | sed 's/^/    /')
EOF
else
    echo "⚠️  Prometheus Operator not found. Skipping PrometheusRule creation."
    echo "   Alerts are available in ConfigMap: prometheus-codec-server-rules"
fi

echo "✅ Monitoring configuration deployed successfully!"
echo ""
echo "Next steps:"
echo "1. Import Grafana dashboard: ${PROJECT_ROOT}/monitoring/grafana-dashboard-codec-server.json"
echo "2. Configure AlertManager routing (see monitoring/README.md)"
echo "3. Verify alerts in Prometheus UI: http://prometheus:9090/rules"
