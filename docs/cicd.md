# CI/CD Pipeline

Technical reference for the Warmbly CI/CD pipeline.

## Overview

- **GitHub Actions** - CI (tests, linting) and image builds
- **ArgoCD** - ArgoCD-based Kubernetes deployments
- **ArgoCD Image Updater** - Automatic deployment when new images are pushed

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Pull Request                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│  ci.yml                                                                      │
│  ├── Change Detection (dorny/paths-filter)                                  │
│  ├── Go CI (golangci-lint, go test)                                         │
│  ├── Rust CI (cargo fmt, clippy, test)                                      │
│  ├── Elixir CI (mix format, credo, test)                                    │
│  └── Security Scan (Trivy)                                                  │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ PR Merged to main
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Build & Push (GitHub Actions)                        │
├─────────────────────────────────────────────────────────────────────────────┤
│  build-push.yml                                                              │
│  ├── Change Detection                                                        │
│  └── Build & Push changed services to GHCR                                  │
│      └── Tags: {sha}, dev                                                    │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ New image pushed to GHCR
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Image Updater (Kubernetes)                           │
├─────────────────────────────────────────────────────────────────────────────┤
│  1. Detects new image in GHCR (polls every 2 minutes)                       │
│  2. Updates deployment with new image                                        │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ Deployment updated
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         ArgoCD Controller (Kubernetes)                       │
├─────────────────────────────────────────────────────────────────────────────┤
│  1. Syncs manifests from git repository                                     │
│  2. Applies to Kubernetes cluster                                           │
│  3. Monitors health and rollout status                                      │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Workflow Files

| File | Trigger | Purpose |
|------|---------|---------|
| `ci.yml` | Push/PR to main | Tests, linting, security |
| `build-push.yml` | Push to main | Build Docker images → GHCR |
| `release.yml` | Tag `v*.*.*` | Build release images with semver tags |

## Deployment Environments

| Environment | Namespace | Update Strategy | Trigger |
|-------------|-----------|-----------------|---------|
| Development | `warmbly-dev` | Digest (latest `dev` tag) | Push to main |
| Production | `warmbly` | Semver (`v*.*.*` tags) | Git tag push |

## Development Deployment

When code is merged to main:

1. `build-push.yml` builds changed services and pushes to GHCR with `dev` tag
2. Image Updater detects the new digest for `dev` tag
3. ArgoCD controller deploys to `warmbly-dev` namespace

No manual intervention required.

## Production Deployment

When you're ready to release:

```bash
git tag -a v1.2.3 -m "Release v1.2.3"
git push origin v1.2.3
```

This triggers:

1. `release.yml` builds ALL services with version tags (`v1.2.3`, `v1.2`, `v1`)
2. Image Updater detects the new semver tag
3. ArgoCD controller deploys to `warmbly` (production) namespace

## Image Tagging

| Environment | Tags | Example |
|-------------|------|---------|
| Development | `{sha}`, `dev` | `abc1234`, `dev` |
| Production | `v{major}.{minor}.{patch}`, `v{major}.{minor}`, `v{major}` | `v1.2.3`, `v1.2`, `v1` |

## Monitoring Deployments

### Kubernetes

```bash
# Check deployment status
kubectl -n warmbly-dev get deployments
kubectl -n warmbly-dev get pods

# Check what image is running
kubectl -n warmbly-dev get deployment dev-backend -o jsonpath='{.spec.template.spec.containers[0].image}'

# View logs
kubectl -n warmbly-dev logs -l app=backend -f
```

## Rollback

### Via Git

```bash
# Revert the change
git revert <commit-sha>
git push origin main

# ArgoCD will automatically sync the reverted state
```

### Emergency: Direct kubectl

```bash
# Set specific image directly
kubectl -n warmbly set image deployment/backend backend=ghcr.io/warmbly/warmbly/backend:v1.2.2

# Note: ArgoCD will show "OutOfSync" until git state matches
```

## Troubleshooting

### Build Issues

```bash
gh run list --workflow=build-push.yml
gh run view <run-id> --log
```

### Pods Not Starting

```bash
kubectl -n warmbly-dev describe pod <pod-name>
kubectl -n warmbly-dev get events --sort-by=.lastTimestamp
```

## Security

1. **Public Repository**: No auth needed to read manifests
2. **GHCR Access**: Optional token for rate limits
3. **Secrets**: All credentials managed via Terraform and Kubernetes secrets
4. **No kubeconfig in CI**: ArgoCD handles deployments from within the cluster
