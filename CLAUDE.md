# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Architecture Overview

This is a **Go web application** that displays live LXD VM/container status via a dashboard, deployed through **MicroK8s + Helm** with **Gitea Actions CI/CD**. Security uses **OpenBao (Vault)** for secrets management with AppRole authentication.

### Core Components
- `app/` - Go HTTP server that queries LXD API and renders HTML dashboard
- `helm-chart/` - Helm charts for deploying across dev/staging/live environments  
- `.gitea/workflows/` - CI/CD pipelines (PR → dev/staging, push → live)
- `setup/` - One-time infrastructure setup scripts

### Data Flow (per HTTP request)
```
GET / → getSecrets() [OpenBao AppRole auth] 
       → fetch LXD TLS creds from homelab/data/lxd 
       → lxd.ConnectLXD() [skip verify] 
       → GetInstancesFull() 
       → renderTemplate(index.html)
```

## Commands Reference

### Development Commands (Mandatory after changes)

**After modifying Go source code:**
```bash
# 1. Format the code (required before commit/deploy)
go fmt ./...

# 2. Run vet to catch common issues
go vet ./...

# 3. Build locally to verify no compilation errors
go build -o /tmp/main ./app
```

### Running the App
```bash
# Run locally (need LXD + OpenBao creds in env)
go run app/main.go

# Or run built binary
/tmp/main
```

### Testing
```bash
# Run all tests
go test ./...

# Run a single test
go test -v ./app/ -run TestSpecificTestName
```

### Building & Deploying
```bash
# Build Docker image (push to private registry)
docker build -t 10.206.213.243:32000/go-lxd-vm-dashboard:latest ./app
docker push 10.206.213.243:32000/go-lxd-vm-dashboard:latest

# Verify deployed service is responding
curl http://<service-ip>:8080/ | grep -o "LXD VM"

# Deploy to environment via Helm
helm upgrade --install go-lxd-vm-dashboard \
  --namespace <dev|staging|live> \
  --values helm-chart/values-<env>.yaml \
  --set image.tag=<tag> \
  -f helm-chart/Chart.yaml \
  ./helm-chart
```

### Linting
```bash
# If installed: golangci-lint run
# Otherwise: go vet ./...
```

## Environment Variables (K8s)

Injected via `openbao-creds` secret in each namespace:
- `VAULT_ADDR` - OpenBao server URL
- `VAULT_ROLE_ID` - AppRole role ID (same for all workflows)
- `VAULT_SECRET_ID` - AppRole secret ID  
- `LXD_ADDR` - LXD API endpoint

Additional at runtime:
- `PORT` - HTTP listen port (default: 8080)

## Security Architecture

### Secrets Flow (Gitea → OpenBao → LXD)
```
┌───────────────────────┐
│ Gitea Org Secrets     │ ← VAULT_ADDR, ROLE_ID, SECRET_ID
└───────────┬───────────┘
            │ AppRole login
            ▼
┌───────────────────────┐
│ OpenBao AppRole auth  │ ← Validates credentials
└───────────┬───────────┘
            │ Returns LXD TLS creds
            ▼
┌───────────────────────┐     ┌──────────────────┐
│ homelab/data/lxd      │     │ K8s Secret       │
│ client_cert           │◄────│ openbao-creds    │
│ client_key            │     │ VAULT_*.         │
└───────────────────────┘     └──────────────────┘
            │                     │
            ▼                     ▼
        LXD API              K8s deployment.yaml
   (10.0.0.162:8443)      ← envFrom + values.yaml
                           ↓
                      Go App reads creds at runtime
```

### Three Environments
- **dev** - PR pipeline job 2 after build succeeds
- **staging** - PR pipeline job 3 (sequential, requires dev deployment first)
- **live** - Push pipeline job (requires merge to `main`)

## Common Tasks

### Add/modify LXD instance queries
1. Edit `app/main.go` → find `GetInstancesFull()` call
2. Modify filters or add new queries to LXD state API
3. Update HTML template `app/index.html` if rendering changes needed

### Environment promotion gates
- PR pipeline: build → deploy-dev → deploy-staging (sequential)
- Push pipeline: build → deploy-live (immediate on merge)

### Troubleshooting OpenBao auth failures
- Verify Gitea org secrets have matching ROLE_ID/SECRET_ID pair
- Check OpenBao policy allows `lxd-read` data path access
- Confirm kubeconfig fetch succeeds (`kubectl --kubeconfig=<path>`)

### Troubleshooting LXD connection
- LXD address defaults to `10.0.0.162:8443` per README
- Verify TLS cert/key from OpenBao are valid for that host
- `InsecureSkipVerify: true` is used in client config

## CI/CD Workflows

### `.gitea/workflows/pr-pipeline.yml`
Triggers on PR to `main`:
1. `build-pr` - Build image, tag as `pr-{run_number}`, push to registry
2. `deploy-dev-pr` - Helm deploy to dev namespace (depends on build success)
3. `deploy-staging-pr` - Helm deploy to staging namespace (depends on dev success)

### `.gitea/workflows/push-pipeline.yml`
Triggers on push to `main`:
1. `build` - Build image tagged `latest`, push to registry
2. `deploy-live` - Helm deploy to live namespace

All workflows use Gitea Actions secrets for VAULT_* credentials and inject them into pods at runtime.
