# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A lightweight Go web application that displays live status of LXD instances (VMs and containers) via a dashboard. Deployed on MicroK8s via Helm across three isolated environments: dev, staging, and live, using a Gitea Actions CI/CD pipeline with sequential environment promotion.

## Architecture Summary

```
┌─────────────────────────────────────────────────┐
│              Go App (main.go)                    │
│   - Authenticates to OpenBao via AppRole        │
│   - Retrieves LXD TLS credentials at runtime     │
│   - Queries LXD API for instance status          │
│   - Renders HTML dashboard via template engine  │
└─────────────────────────┬───────────────────────┘
                          │
                          ▼
              ┌──────────────────────┐
              │  Helm Chart          │
              │  - deployment.yaml   │
              │  - values*.yaml      │
              │  - service/ingress   │
              └──────────────────────┘
                         / | \
                        /  |  \
                       /   |   \
              ┌───────┼────┼────┼────────┐
              │       │    │    │        │
          ┌───▼───┐ ┌──▼──┐ ┌──▼──┐      │
          │ dev   │ │staging│ │ live│     │
          └───────┘ └──────┘ └──────┘      │
               ─────────────────────────────
```

## Key Commands

### Build & Deploy

**Build the Go application:**
```bash
cd app
docker build -t 10.206.213.243:32000/go-lxd-vm-dashboard:latest .
docker push 10.206.213.243:32000/go-lxd-vm-dashboard:latest
```

**Deploy to environment:**
```bash
# Example dev deployment
helm upgrade --install go-lxd-vm-dashboard \
  --namespace dev \
  --values helm-chart/values-dev.yaml \
  --set image.tag=latest \
  --set image.pullPolicy=Always \
  10.206.213.243:32000/go-lxd-vm-dashboard
```

**Full deployment with local registry push:**
```bash
# Build and push
docker build -t 10.206.213.243:32000/go-lxd-vm-dashboard:$TAG ./app
docker push 10.206.213.243:32000/go-lxd-vm-dashboard:$TAG

# Deploy to target environment
KUBECONFIG=/path/to/kubeconfig \
helm upgrade --install go-lxd-vm-dashboard \
  --namespace $NS \
  --values helm-chart/values-$NS.yaml \
  --set image.tag=$TAG \
  -f /tmp/kubeconfig
```

### Development Workflow

**Start local development server:**
```bash
cd app
go run main.go
# Visit http://localhost:8080
```

**Lint the Go code:**
```bash
golangci-lint run ./...
# or simply
go vet ./...
```

**Test individual components:**
```bash
# Run go test with verbose output
cd app
go test -v -run TestYourFunctionName ./...

# Run specific test file
go test -v -run ^TestStatusHandler$ ./
```

### Environment Setup

**MicroK8s namespaces and secrets setup:**
```bash
microk8s kubectl create namespace dev
microk8s kubectl create namespace staging
microk8s kubectl create namespace live

# Create OpenBao credentials secret for each namespace
for NS in dev staging live; do
  microk8s kubectl create secret generic openbao-creds \
    --from-literal=VAULT_ADDR=https://<openbao-ip>:8200 \
    --from-literal=VAULT_ROLE_ID=<role-id> \
    --from-literal=VAULT_SECRET_ID=<secret-id> \
    --namespace=$NS \
    --dry-run=client -o yaml | microk8s kubectl apply -f -
done
```

**Add ingress hosts:**
```bash
echo "<microk8s-node-ip> dev.local staging.local live.local" | sudo tee -a /etc/hosts
```

### GHA Workflow Secrets

These must be set at the **Gitea org level** (Infra org):

- `VAULT_ADDR` — OpenBao server address
- `VAULT_ROLE_ID` — AppRole Role ID (same for all workflows)
- `VAULT_SECRET_ID` — AppRole Secret ID

Workflow files use `.github/workflows/` directory inside the app subdirectory.

## Code Architecture Details

### Go Application (`app/main.go`)

**Entry point:** The application uses a simple HTTP server that handles each request to `/`.

**Request flow on each HTTP request to `/`:**

1. **`getSecrets()`** — Authenticates to OpenBao using AppRole credentials from environment variables:
   - Reads `VAULT_ADDR`, `VAULT_ROLE_ID`, `VAULT_SECRET_ID`
   - Performs AppRole login at `auth/approle/login`
   - Reads LXD TLS credentials from secret path `homelab/data/lxd`

2. **LXD Connection:** Uses the retrieved TLS cert/key to connect:
   - `lxd.ConnectLXD()` with `InsecureSkipVerify: true`
   - Configured to use project `default`

3. **Query LXD API:** Calls `client.GetInstancesFull()` to get all instances.

4. **Render Template:** Passes instance data to `index.html` and renders HTML.

**Environment variables used:**

| Variable | Source | Default | Purpose |
|----------|--------|---------|---------|
| `VAULT_ADDR` | K8s Secret | required | OpenBao server URL |
| `VAULT_ROLE_ID` | K8s Secret | required | AppRole Role ID |
| `VAULT_SECRET_ID` | K8s Secret | required | AppRole Secret ID |
| `VAULT_SKIP_VERIFY` | deployment.yaml | false | Skip TLS verification for OpenBao |
| `LXD_ADDR` | values.yaml | required | LXD API endpoint |
| `PORT` | optional | 8080 | HTTP listen port |

**Security considerations:**

- Credentials are **never baked into the image** — retrieved at runtime from K8s secrets
- Uses OpenBao AppRole authentication for short-lived credentials
- LXD TLS credentials are fetched per-request, not cached
- `VAULT_SKIP_VERIFY=true` option exists for development convenience

### Helm Chart Structure (`helm-chart/`)

**Files and their purposes:**

- **values.yaml** — Base defaults: image spec, service port (80 → 8080), LXD address
- **values-dev.yaml** — Dev environment overrides (ingress host: `dev.local`)
- **values-staging.yaml** — Staging overrides (`staging.local`)
- **values-live.yaml** — Live overrides (`live.local`)
- **deployment.yaml** — Pod spec with env injected from secrets and values
- **service.yaml** — ClusterIP service exposing port 80 → 8080
- **ingress.yaml** — NGINX ingress with public class

**Helm install pattern:**
```bash
helm upgrade --install go-lxd-vm-dashboard \
  --namespace $NS \
  --values helm-chart/values-$NS.yaml \
  --set image.tag=$TAG
```

### CI/CD Pipelines (`.gitea/workflows/`)

**PR Pipeline (pr-pipeline.yml):**
1. **build-pr** — Builds and pushes image tagged as `pr-<run_number>`
2. **deploy-dev-pr** — Fetches kubeconfig, deploys to dev namespace
3. **deploy-staging-pr** — Deploys to staging after dev approval

**Push Pipeline (push-pipeline.yml):**
1. **build** — Builds and pushes image tagged as `latest`
2. **deploy-live** — Deploys to live namespace

Both pipelines use OpenBao AppRole for authentication and fetch credentials at runtime, similar to the app itself.

## Common Development Tasks

### Debugging LXD Connection Issues

1. Check OpenBao role permissions:
```bash
# Verify you can login to OpenBao
curl -X POST https://<vault-addr>/v1/auth/approle/login \
  -d "role_id=<role_id>" \
  -d "secret_id=<secret_id>"
```

2. Check if secret path exists:
```bash
# List secrets in homelab namespace
curl https://<vault-addr>/v1/secret/list homelab/data --header "Authorization: Bearer <token>"
```

3. Verify LXD API is accessible from GHA runner:
```bash
curl -k https://<lxd-address>/1.0/instances?recursive=true \
  --cert <client-cert> \
  --key <client-key>
```

### Debugging Helm Deployments

**Check deployment status:**
```bash
microk8s kubectl get pods -n dev
microk8s kubectl describe pod <pod-name> -n dev

# Check events for failure reasons
microk8s kubectl logs <pod-name> -n dev
```

### Updating OpenBao Secrets Path

If you change the LXD secrets path in OpenBao:

1. Update secret location in app (`app/main.go` line 106):
```go
secret, err := client.Logical().Read("homelab/data/lxd")
```

2. Regenerate and redeploy:
```bash
helm upgrade --install ... --set image.tag=new-tag
```

### Template Rendering

The app loads `index.html` as a Go template. To modify the dashboard UI, edit `app/index.html`. The template receives data via `InstanceData` struct containing instances or errors.

## Important Notes

1. **Version Compatibility:**
   - Terraform ≥ 1.5 required (for related infrastructure)
   - LXD installed via snap on Ubuntu 24.04 host

2. **Secrets Management:**
   - All secrets are configured at Gitea org level (Infra org)
   - K8s secrets are created manually in each namespace via `openbao-creds`

3. **Deployment Flow:**
   - PR to main triggers: build → dev → staging promotion
   - Merge to main triggers: build → live deployment

4. **Workflow Requirements:**
   - All workflows run from root directory of repository
   - Workflows use Gitea Actions with predefined environment variables from secrets

5. **State Management (Related Infrastructure):**
   - See `terraform/` directory for LXD VM provisioning
   - State stored in MinIO per environment per VM
   - VM naming convention: `<environment>-<os_type>-<vm_name>`
