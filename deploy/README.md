# Warmbly Deployment Infrastructure

This directory contains all deployment configurations for Warmbly services.

## Services

| Service | Language | Description |
|---------|----------|-------------|
| Backend | Go | API server |
| Consumer | Go | Kafka event consumer |
| Worker | Go | Distributed worker (1 per machine) |
| Tracking | Rust | Tracking pixel service |
| Realtime | Elixir | WebSocket server |

## Directory Structure

```
deploy/
├── docker/                          # Docker configurations
│   ├── backend.Dockerfile
│   ├── consumer.Dockerfile
│   ├── worker.Dockerfile
│   ├── realtime.Dockerfile
│   └── docker-compose.yml           # Local development stack
│
├── kubernetes/
│   ├── base/                        # Base Kustomize configs
│   │   ├── backend/
│   │   ├── consumer/
│   │   ├── worker/
│   │   ├── tracking/
│   │   ├── realtime/
│   │   ├── ingress/
│   │   └── config/
│   │
│   └── overlays/
│       ├── dev/                     # Dev environment
│       └── prod/                    # Production environment
│
└── config/
    └── env.example                  # Environment variable reference
```

## Local Development

Start the full development stack with Docker Compose:

```bash
cd deploy/docker

# Start infrastructure only
docker-compose up -d postgres redis kafka schema-registry

# Start all services
docker-compose up
```

### Service URLs (Local)

- Backend API: http://localhost:8080
- Tracking: http://localhost:3000
- Realtime: http://localhost:4000
- Kafka: localhost:9092
- Schema Registry: http://localhost:8081
- PostgreSQL: localhost:5432
- Redis: localhost:6379

## Building Docker Images

```bash
# From repository root
docker build -f deploy/docker/backend.Dockerfile -t warmbly/backend .
docker build -f deploy/docker/consumer.Dockerfile -t warmbly/consumer .
docker build -f deploy/docker/worker.Dockerfile -t warmbly/worker .
docker build -f deploy/docker/realtime.Dockerfile -t warmbly/realtime .
docker build -f tracking/Dockerfile -t warmbly/tracking tracking/
```

## Kubernetes Deployment

### Prerequisites

1. **External Secrets Operator** - For AWS Secrets Manager integration
   ```bash
   helm repo add external-secrets https://charts.external-secrets.io
   helm install external-secrets external-secrets/external-secrets -n external-secrets --create-namespace
   ```

2. **NGINX Ingress Controller**
   ```bash
   helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
   helm install ingress-nginx ingress-nginx/ingress-nginx -n ingress-nginx --create-namespace
   ```

3. **cert-manager** - For TLS certificates
   ```bash
   helm repo add jetstack https://charts.jetstack.io
   helm install cert-manager jetstack/cert-manager -n cert-manager --create-namespace --set installCRDs=true
   ```

### Deploy to Dev

```bash
kubectl apply -k deploy/kubernetes/overlays/dev
```

### Deploy to Production

```bash
kubectl apply -k deploy/kubernetes/overlays/prod
```

### Verify Deployment

```bash
# Check pods
kubectl get pods -n warmbly

# Check services
kubectl get svc -n warmbly

# Check ingress
kubectl get ingress -n warmbly
```

## Configuration

### Environment Variables

See `config/env.example` for a complete list of environment variables.

### Production Secrets

Production secrets are stored in AWS Secrets Manager and synced via External Secrets Operator:

| Secret Key | Description |
|------------|-------------|
| `postgres/primary` | Primary database connection string |
| `redis/primary` | Redis connection string |
| `kafka/sasl/username` | Kafka SASL username |
| `kafka/sasl/password` | Kafka SASL password |
| `auth/secret` | Auth signing secret |
| `auth/jwt_secret` | JWT secret |
| `stripe/secret_key` | Stripe secret key |
| `stripe/webhook_secret` | Stripe webhook secret |

### Non-Sensitive Config

Non-sensitive configuration is stored in the ConfigMap (`config/configmap.yaml`) and can be committed to git.

## Worker DaemonSet

The Worker service runs as a DaemonSet, meaning one pod per node. Nodes must be labeled:

```bash
kubectl label node <node-name> warmbly.com/role=worker
```

## Health Checks

All services expose health endpoints:

```bash
curl http://localhost:8080/health  # Backend
curl http://localhost:3000/health  # Tracking
curl http://localhost:4000/health  # Realtime
```

## Scaling

### Development
Default replicas are set in base manifests.

### Production
Production replica counts are configured in `overlays/prod/patches/replicas.yaml`:

- Backend: 3 replicas
- Consumer: 2 replicas
- Tracking: 3 replicas
- Realtime: 3 replicas
- Worker: 1 per labeled node (DaemonSet)
