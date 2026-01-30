# Warmbly Deployment Guide

Complete step-by-step guide to deploy Warmbly from scratch.

## Table of Contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [Step 1: Infrastructure Setup](#step-1-infrastructure-setup)
4. [Step 2: DNS Configuration](#step-2-dns-configuration)
5. [Step 3: GitHub Repository Setup](#step-3-github-repository-setup)
6. [Step 4: First Deployment](#step-4-first-deployment)
7. [Step 5: Verify Everything Works](#step-5-verify-everything-works)
8. [Ongoing Development](#ongoing-development)
9. [Production Releases](#production-releases)
10. [Troubleshooting](#troubleshooting)

---

## Overview

### Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           GitHub (warmbly repo)                              │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐                      │
│  │   ci.yml    │    │build-push.yml│   │ release.yml │                      │
│  │  (tests)    │    │ (build imgs) │   │(version tag)│                      │
│  └─────────────┘    └──────┬──────┘    └──────┬──────┘                      │
└─────────────────────────────┼─────────────────┼─────────────────────────────┘
                              │                 │
                              ▼                 ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                    GitHub Container Registry (GHCR)                          │
│         ghcr.io/warmbly/warmbly/{backend,consumer,worker,tracking,realtime} │
└─────────────────────────────────────────────────────────────────────────────┘
                              │
                              │ ArgoCD detects new images
                              ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Kubernetes Cluster                                   │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    ArgoCD Controller (namespace: gitops)             │    │
│  │  ┌──────────────┐  ┌──────────────┐  ┌────────────────────────┐    │    │
│  │  │   Web UI     │  │  controller  │  │   image-updater        │    │    │
│  │  └──────────────┘  └──────────────┘  └────────────────────────┘    │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                    │                                         │
│                    syncs manifests from git                                  │
│                                    ▼                                         │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │              warmbly-dev / warmbly (namespaces)                      │    │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐       │    │
│  │  │ backend │ │consumer │ │ worker  │ │tracking │ │realtime │       │    │
│  │  └─────────┘ └─────────┘ └─────────┘ └─────────┘ └─────────┘       │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────────────┘
```

### What Gets Deployed Where

| Component | Location | Managed By |
|-----------|----------|------------|
| Kubernetes cluster | Cloud Provider | Terraform |
| ArgoCD controller | K8s cluster | Terraform/Helm |
| Warmbly services | K8s (warmbly-dev, warmbly namespaces) | ArgoCD |
| Docker images | GitHub Container Registry | GitHub Actions |
| Kubernetes manifests | warmbly repo (deploy/kubernetes/) | Git |

---

## Prerequisites

### Required Tools

```bash
# Terraform (>= 1.4.0)
brew install terraform

# kubectl
brew install kubectl

# htpasswd (for generating passwords)
brew install httpd
```

### Required Accounts

- **Cloud Provider** - For Kubernetes cluster
- **GitHub** - Repository and Container Registry
- **AWS** - RDS, DynamoDB, SES, S3
- **GCP** - Cloud Tasks, Pub/Sub

---

## Step 1: Infrastructure Setup

### 1.1 Clone the Infrastructure Repository

```bash
cd ~/projects
git clone git@github.com:warmbly/warmbly-infrastructure.git
cd warmbly-infrastructure
```

### 1.2 Create terraform.tfvars

```bash
cp terraform.tfvars.example terraform.tfvars
```

Edit `terraform.tfvars` with your credentials (see infrastructure README for details).

### 1.3 Initialize and Apply Terraform

```bash
terraform init
terraform validate
terraform plan
terraform apply
```

### 1.4 Save Important Outputs

```bash
terraform output
export KUBECONFIG=$(pwd)/kubeconfig
kubectl get nodes
```

---

## Step 2: DNS Configuration

### 2.1 ArgoCD Controller DNS

Create a DNS A record pointing to the Kubernetes load balancer:

```
gitops.warmbly.com → <k8s_load_balancer_ip>
```

### 2.2 Application DNS

```
api.warmbly.com      → <k8s_load_balancer_ip>
t.warmbly.com        → <k8s_load_balancer_ip>
gateway.warmbly.com  → <k8s_load_balancer_ip>
```

---

## Step 3: GitHub Repository Setup

### 3.1 Verify Workflow Files Exist

```
warmbly/.github/workflows/
├── ci.yml           # Tests and linting
├── build-push.yml   # Build and push Docker images
└── release.yml      # Release versioned images
```

### 3.2 Configure Repository Settings

1. Go to GitHub repository → Settings
2. Actions → General: "Read and write permissions"

---

## Step 4: First Deployment

### 4.1 Trigger Initial Build

```bash
cd warmbly
git checkout main
git commit --allow-empty -m "Trigger initial CI/CD build"
git push origin main
```

### 4.2 Monitor GitHub Actions

1. Go to GitHub → Actions tab
2. Watch `Build and Push` workflow

### 4.3 Check ArgoCD Controller

Access the ArgoCD UI and verify applications are synced.

---

## Step 5: Verify Everything Works

### 5.1 Check Pods Are Running

```bash
kubectl -n warmbly-dev get pods
```

### 5.2 Check Service Endpoints

```bash
kubectl -n warmbly-dev get ingress
curl https://api.dev.warmbly.com/health
```

---

## Ongoing Development

### Daily Workflow

```
1. Create feature branch
2. Make changes
3. Create PR → CI runs tests
4. Merge to main → Build runs → ArgoCD deploys to dev
5. Verify in dev environment
```

### Automatic Dev Deployment

When you merge to `main`:

1. `build-push.yml` builds changed services
2. Images pushed to GHCR with `:dev` tag
3. ArgoCD detects new image (within 2 minutes)
4. Automatically deploys to `warmbly-dev` namespace

---

## Production Releases

### Release Process

```bash
# 1. Ensure all changes are tested in dev

# 2. Create a release tag
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0

# 3. ArgoCD deploys to production namespace
```

### Rollback Production

```bash
# Via ArgoCD UI or CLI - rollback to previous revision
```

---

## Troubleshooting

### Build Not Triggering

```bash
gh run list --workflow=build-push.yml
gh run view <run-id> --log
```

### Pods Not Starting

```bash
kubectl -n warmbly-dev describe pod <pod-name>
kubectl -n warmbly-dev get events --sort-by=.lastTimestamp
kubectl -n warmbly-dev logs <pod-name>
```

---

## Quick Reference

### Commands Cheat Sheet

```bash
# Infrastructure
cd warmbly-infrastructure
terraform plan
terraform apply
terraform output

# Kubernetes
export KUBECONFIG=~/projects/warmbly-infrastructure/kubeconfig
kubectl -n warmbly-dev get pods
kubectl -n warmbly-dev logs -l app=backend -f

# GitHub
gh run list
gh run view <run-id> --log
```
