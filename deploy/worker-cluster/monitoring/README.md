# Worker Cluster Monitoring

Lightweight monitoring for the worker cluster. This setup runs only Prometheus + node-exporter without Grafana - use the main cluster's Grafana to visualize metrics.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     Main Kubernetes Cluster                      │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │ Prometheus  │  │  Grafana    │  │  Alertmanager           │  │
│  │ (main)      │  │             │  │                         │  │
│  └─────────────┘  └──────┬──────┘  └─────────────────────────┘  │
│                          │                                       │
│                          │ Data Sources:                         │
│                          │ - Prometheus (main)                   │
│                          │ - Prometheus (workers) ◄────────┐     │
└────────────────────────────────────────────────────────────┼─────┘
                                                             │
                                              Network/VPN    │
                                                             │
┌────────────────────────────────────────────────────────────┼─────┐
│                       Worker Cluster                       │     │
│  ┌─────────────┐                                           │     │
│  │ Prometheus  │◄──────────────────────────────────────────┘     │
│  │ (workers)   │                                                 │
│  └─────────────┘                                                 │
│         ▲                                                        │
│         │ scrapes                                                │
│  ┌──────┴──────┐                                                 │
│  │             │                                                 │
│  ▼             ▼                                                 │
│ ┌─────┐ ┌─────┐ ┌─────┐      ┌───────────────────┐              │
│ │node │ │node │ │node │ ...  │ kube-state-metrics│              │
│ │exp. │ │exp. │ │exp. │      │   (1 instance)    │              │
│ └─────┘ └─────┘ └─────┘      └───────────────────┘              │
│ Worker  Worker  Worker                                           │
│ Node 1  Node 2  Node 3                                           │
└──────────────────────────────────────────────────────────────────┘
```

## Resource Usage

| Component | Instances | CPU Request | Memory Request |
|-----------|-----------|-------------|----------------|
| Prometheus | 1 | 100m | 256Mi |
| Prometheus Operator | 1 | 50m | 64Mi |
| kube-state-metrics | 1 | 10m | 32Mi |
| node-exporter | 1 per node | 10m | 16Mi |

**Total for 10 worker nodes:** ~200m CPU, ~500Mi RAM

## Installation

### 1. Add Helm Repository (if not already added)

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
```

### 2. Install on Worker Cluster

```bash
# Make sure kubectl is pointing to your worker cluster
kubectl config use-context <your-worker-cluster-context>

# Create namespace and install
helm install prometheus-stack prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --create-namespace \
  --values deploy/worker-cluster/monitoring/values.yaml
```

### 3. Verify Installation

```bash
kubectl get pods -n monitoring

# Expected: prometheus, prometheus-operator, node-exporter (per node), kube-state-metrics
```

## Exposing Prometheus to Main Cluster

The main cluster's Grafana needs network access to this Prometheus. Choose one method:

### Option A: NodePort (Simple)

Edit `values.yaml`:
```yaml
prometheus:
  service:
    type: NodePort
    nodePort: 30090
```

Then upgrade:
```bash
helm upgrade prometheus-stack prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --values deploy/worker-cluster/monitoring/values.yaml
```

Access via: `http://<any-worker-node-ip>:30090`

**Important:** Secure with firewall rules - only allow your main cluster's IP.

### Option B: LoadBalancer (Cloud Provider)

Edit `values.yaml`:
```yaml
prometheus:
  service:
    type: LoadBalancer
```

Access via the assigned external IP.

### Option C: VPN/Private Network

If clusters are connected via VPN or private network, use the ClusterIP service directly or set up an internal load balancer.

### Option D: Ingress with Authentication

```yaml
# deploy/worker-cluster/monitoring/ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: prometheus-ingress
  namespace: monitoring
  annotations:
    # Add basic auth or other authentication
    nginx.ingress.kubernetes.io/auth-type: basic
    nginx.ingress.kubernetes.io/auth-secret: prometheus-basic-auth
spec:
  rules:
    - host: prometheus-workers.yourdomain.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: prometheus-stack-prometheus
                port:
                  number: 9090
```

## Connect Main Cluster's Grafana

Once Prometheus is exposed, add it as a data source in your main cluster's Grafana:

1. Open Grafana (main cluster)
2. Go to **Configuration → Data Sources → Add data source**
3. Select **Prometheus**
4. Configure:
   - **Name:** `Prometheus-workers`
   - **URL:** `http://<worker-prometheus-endpoint>:9090`
   - **Access:** Server (default)
5. Click **Save & Test**

## Useful Queries

### Worker Node Metrics

```promql
# CPU usage per worker node
100 - (avg by(instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)

# Memory usage per worker node
(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100

# Disk usage per worker node
(1 - node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"}) * 100

# Network traffic per node
rate(node_network_receive_bytes_total[5m])
rate(node_network_transmit_bytes_total[5m])
```

### Worker Pod Metrics

```promql
# Worker pod status
kube_pod_status_phase{namespace="warmbly", pod=~"worker.*"}

# Worker pod restarts
increase(kube_pod_container_status_restarts_total{namespace="warmbly", pod=~"worker.*"}[1h])

# Worker pod CPU usage
rate(container_cpu_usage_seconds_total{namespace="warmbly", pod=~"worker.*"}[5m])

# Worker pod memory usage
container_memory_working_set_bytes{namespace="warmbly", pod=~"worker.*"}
```

## Upgrading

```bash
helm repo update
helm upgrade prometheus-stack prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --values deploy/worker-cluster/monitoring/values.yaml
```

## Uninstalling

```bash
helm uninstall prometheus-stack -n monitoring
kubectl delete namespace monitoring

# Remove PVCs if you want to delete all data
kubectl delete pvc -n monitoring --all
```
