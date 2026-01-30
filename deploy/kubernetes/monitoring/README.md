# Kubernetes Cluster Monitoring (Main Cluster)

This directory contains configuration for deploying the **kube-prometheus-stack** Helm chart on the main Kubernetes cluster.

## Multi-Cluster Architecture

Warmbly uses two separate clusters:

```
┌──────────────────────────────────────┐     ┌──────────────────────────────────┐
│       Main Kubernetes Cluster        │     │       k3s Worker Cluster         │
│                                      │     │       (separate provider)        │
│  Services:                           │     │                                  │
│  - Backend API                       │     │  Services:                       │
│  - Realtime (WebSocket)              │     │  - Worker DaemonSet              │
│  - Tracking                          │     │                                  │
│  - Consumer                          │     │  Monitoring:                     │
│                                      │     │  - Prometheus (lightweight)      │
│  Monitoring:                         │     │  - node-exporter                 │
│  - Prometheus (full)                 │     │  - kube-state-metrics            │
│  - Grafana ◄─────────────────────────┼─────┤                                  │
│  - Alertmanager                      │     │  See: deploy/k3s/monitoring/     │
│  - node-exporter                     │     │                                  │
│  - kube-state-metrics                │     │                                  │
└──────────────────────────────────────┘     └──────────────────────────────────┘
```

The main cluster's Grafana connects to both Prometheus instances to provide a unified view.

## Components Installed

- **Prometheus** - Metrics collection and storage
- **Grafana** - Dashboards and visualization
- **Alertmanager** - Alert routing and notifications
- **kube-state-metrics** - Kubernetes object metrics (pods, deployments, nodes)
- **node-exporter** - Host-level metrics (CPU, memory, disk per node)
- **Prometheus Operator** - Manages Prometheus configuration via CRDs

## Installation

### 1. Add Helm Repository

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
```

### 2. Create Monitoring Namespace

```bash
kubectl create namespace monitoring
```

### 3. Install the Stack

```bash
# Basic installation with generated password
helm install prometheus-stack prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --values deploy/kubernetes/monitoring/values.yaml

# Or with a specific Grafana admin password
helm install prometheus-stack prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --values deploy/kubernetes/monitoring/values.yaml \
  --set grafana.adminPassword='your-secure-password'
```

### 4. Verify Installation

```bash
# Check all pods are running
kubectl get pods -n monitoring

# Expected output: all pods should be Running/Ready
```

## Accessing Dashboards

### Grafana (Port Forward)

```bash
kubectl port-forward -n monitoring svc/prometheus-stack-grafana 3001:80
# Open http://localhost:3001
# Default credentials: admin / prom-operator (or your configured password)
```

### Grafana (Ingress)

1. Set your Grafana domain:
   ```bash
   export GRAFANA_DOMAIN=grafana.yourdomain.com
   ```

2. Apply the ingress:
   ```bash
   envsubst < deploy/kubernetes/monitoring/ingress.yaml | kubectl apply -f -
   ```

### Prometheus UI (Port Forward)

```bash
kubectl port-forward -n monitoring svc/prometheus-stack-prometheus 9090:9090
# Open http://localhost:9090
```

### Alertmanager UI (Port Forward)

```bash
kubectl port-forward -n monitoring svc/prometheus-stack-alertmanager 9093:9093
# Open http://localhost:9093
```

## Pre-built Dashboards

The stack includes these dashboards out of the box in Grafana:

| Dashboard | What it shows |
|-----------|---------------|
| Kubernetes / Compute Resources / Cluster | Cluster-wide CPU, memory, network |
| Kubernetes / Compute Resources / Node | Per-node resource usage |
| Kubernetes / Compute Resources / Namespace (Pods) | Pod resources by namespace |
| Node Exporter / Nodes | Detailed host metrics |
| Kubernetes / Kubelet | Kubelet health and pod operations |

## Custom Alerts

The following custom alerts are configured in `values.yaml`:

### Cluster Scaling Alerts
- **HighNodeCPU** - Node CPU > 80% for 10 minutes
- **CriticalNodeCPU** - Node CPU > 95% for 5 minutes
- **LowNodeMemory** - Node memory < 20% available
- **CriticalLowNodeMemory** - Node memory < 10% available
- **PodsPending** - Pods stuck pending for 15+ minutes
- **ContainerThrottling** - Container CPU throttling detected
- **ContainerMemoryNearLimit** - Container using > 90% memory limit

### Cluster Health Alerts
- **NodeNotReady** - Node in NotReady state
- **PodHighRestartCount** - Pod restarting frequently (>5/hour)
- **PodCrashLoopBackOff** - Pod in crash loop
- **DeploymentReplicasMismatch** - Deployment not at desired replicas
- **PersistentVolumeAlmostFull** - PVC > 85% full

### Warmbly Application Alerts
- **WarmblyBackendUnavailable** - Backend service has 0 available pods
- **WarmblyRealtimeUnavailable** - Realtime service has 0 available pods
- **WarmblyTrackingUnavailable** - Tracking service has 0 available pods

## Configuring Alert Notifications

Edit `values.yaml` to configure Slack, email, or webhook notifications:

### Slack Example

```yaml
alertmanager:
  config:
    receivers:
      - name: 'default'
        slack_configs:
          - api_url: 'https://hooks.slack.com/services/xxx/yyy/zzz'
            channel: '#alerts'
            send_resolved: true
            title: '{{ .Status | toUpper }}: {{ .CommonLabels.alertname }}'
            text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
```

### Email Example

```yaml
alertmanager:
  config:
    global:
      smtp_smarthost: 'smtp.gmail.com:587'
      smtp_from: 'alerts@yourdomain.com'
      smtp_auth_username: 'alerts@yourdomain.com'
      smtp_auth_password: 'your-password'
    receivers:
      - name: 'default'
        email_configs:
          - to: 'team@yourdomain.com'
            send_resolved: true
```

## Key Metrics for Scaling Decisions

### When to Add Nodes
- `node_cpu_seconds_total` - CPU usage per node (>80% sustained = need more nodes)
- `node_memory_MemAvailable_bytes` - Available memory (<20% = memory pressure)
- `kube_pod_status_phase{phase="Pending"}` - Pods stuck pending (scheduling issues)

### Throttling Detection
- `container_cpu_cfs_throttled_seconds_total` - CPU throttling
- `container_memory_working_set_bytes` vs limits - Memory pressure

### Cluster Health
- `kube_node_status_condition{condition="Ready",status="true"}` - Node readiness
- `kube_pod_container_status_restarts_total` - Pod restart count

## Upgrading

```bash
helm repo update
helm upgrade prometheus-stack prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --values deploy/kubernetes/monitoring/values.yaml
```

## Uninstalling

```bash
helm uninstall prometheus-stack -n monitoring
kubectl delete namespace monitoring
```

**Note:** PersistentVolumeClaims are not deleted automatically. To remove all data:

```bash
kubectl delete pvc -n monitoring --all
```

## Resource Usage

Estimated resource usage for a small cluster:

| Component | CPU Request | Memory Request | Storage |
|-----------|-------------|----------------|---------|
| Prometheus | 500m | 1Gi | 50Gi |
| Grafana | 100m | 256Mi | 10Gi |
| Alertmanager | 10m | 64Mi | 5Gi |
| Node Exporter | 10m x nodes | 32Mi x nodes | - |
| Prometheus Operator | 100m | 128Mi | - |
