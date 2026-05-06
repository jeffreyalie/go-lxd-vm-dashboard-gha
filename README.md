# go-lxd-vm-dashboard

A lightweight Go web application that displays the live status of LXD instances (VMs and containers) in your homelab. It authenticates with OpenBao/Vault via AppRole to securely retrieve LXD TLS credentials at runtime, then queries the LXD API and renders results in a clean HTML dashboard. Deployed on MicroK8s via Helm.

---

## Features

- **Live LXD instance table** — name, type (VM/container), status, and IPv4 addresses per interface
- **Vault/OpenBao AppRole auth** — no credentials baked into the image; secrets are fetched at request time
- **Kubernetes-native deployment** — Helm chart with configurable values, ingress, and secret reference
- **Single-binary Docker image** — multi-stage build, Alpine final layer

---

## Architecture

```
                         ┌────────────────────┐
                         │     index.html     │
                         └─────────┬──────────┘
                                   │
                                   ▼
                         ┌────────────────────┐
                         │      Browser       │
                         └─────────┬──────────┘
                                   │
                                   ▼
                         ┌────────────────────┐
                         │  Ingress (NGINX)   │
                         └─────────┬──────────┘
                                   │
                                   ▼
                         ┌────────────────────┐
                         │      Service       │
                         └─────────┬──────────┘
                                   │
                                   ▼
                         ┌────────────────────┐
                         │   Pod (MicroK8s)   │
                         └─────────┬──────────┘
                                   │
             ┌─────────────────────┴─────────────────────┐
             │                                           │
             ▼                                           ▼
┌──────────────────────────────┐         ┌──────────────────────────────┐
│ Fetch TLS cert/key           │         │ Query LXD API                │
│ from OpenBao (AppRole)       │         │ (LXD Server)                 │
└──────────────────────────────┘         └──────────────────────────────┘

Browser → Ingress (nginx) → Service → Pod
                                        ├── Fetches TLS cert/key from OpenBao (AppRole)
                                        └── Queries LXD API → renders index.html

```

Vault credentials (`VAULT_ADDR`, `VAULT_ROLE_ID`, `VAULT_SECRET_ID`) are injected via a Kubernetes Secret. The LXD address is set through `values.yaml`.

---

## Prerequisites

| Tool | Purpose |
|---|---|
| Docker | Build and push the image |
| MicroK8s | Local Kubernetes cluster |
| Helm | Deploy the application |
| OpenBao / Vault | Secrets backend (AppRole auth method) |
| LXD | Target API to query |

MicroK8s add-ons required: `dns`, `registry`, `ingress`, `helm3`

---

## Quick Start

### 1. Build and push the image

```bash
docker build -t localhost:32000/go-lxd-vm-dashboard:latest ./app
docker push localhost:32000/go-lxd-vm-dashboard:latest
```

### 2. Create the namespace and credentials secret

```bash
microk8s kubectl create namespace dev

microk8s kubectl create secret generic openbao-creds \
  --from-literal=VAULT_ADDR=https://<your-vault-addr>:8200 \
  --from-literal=VAULT_ROLE_ID=<role-id> \
  --from-literal=VAULT_SECRET_ID=<secret-id> \
  --namespace=dev
```

### 3. Configure values

Edit `helm-chart/values.yaml` to match your environment:

```yaml
lxd:
  address: "https://<your-lxd-ip>:8443"

ingress:
  host: "lxd-dashboard.local"
```

### 4. Deploy

```bash
microk8s helm upgrade --install go-lxd-vm-dashboard ./helm-chart --namespace dev
```

The dashboard will be available at `http://lxd-dashboard.local` (add this to your `/etc/hosts` pointing at the MicroK8s node IP).

---

## Vault / OpenBao Setup

The app reads the LXD client certificate and key from the KV path `homelab/data/lxd`.

Expected secret structure:

```json
{
  "client_cert": "<PEM string>",
  "client_key": "<PEM string>"
}
```

The AppRole must have a policy granting `read` on that path:

```hcl
path "homelab/data/lxd" {
  capabilities = ["read"]
}
```

---

## Project Structure

```
.
├── app/
│   ├── main.go          # HTTP server, LXD + Vault client logic
│   ├── index.html       # Go HTML template for the dashboard
│   ├── dockerfile       # Multi-stage Docker build
│   ├── go.mod
│   └── go.sum
├── helm-chart/
│   ├── Chart.yaml
│   ├── values.yaml
│   └── templates/
│       ├── deployment.yaml
│       ├── service.yaml
│       └── ingress.yaml.txt
├── .gitignore
└── README.md
```

---

## Helm Values Reference

| Key | Default | Description |
|---|---|---|
| `image.repository` | `localhost:32000/go-lxd-vm-dashboard` | Container image |
| `image.tag` | `latest` | Image tag |
| `image.pullPolicy` | `Always` | Pull policy |
| `service.type` | `ClusterIP` | Kubernetes service type |
| `service.port` | `80` | External service port |
| `service.targetPort` | `8080` | Container port |
| `lxd.address` | _(required)_ | LXD API endpoint |
| `existingSecretName` | `openbao-creds` | Name of the pre-created K8s secret |
| `ingress.enabled` | `true` | Toggle ingress resource |
| `ingress.className` | `public` | IngressClass name |
| `ingress.host` | `lxd-dashboard.local` | Hostname for the ingress rule |

---

## Updating the Deployment

After rebuilding and pushing a new image:

```bash
docker build -t localhost:32000/go-lxd-vm-dashboard:latest ./app
docker push localhost:32000/go-lxd-vm-dashboard:latest
microk8s kubectl rollout restart deployment go-lxd-vm-dashboard -n dev
```

---

## Roadmap

- [ ] Multi-project LXD support (`client.UseProject("production")`)
- [ ] Gitea Actions CI/CD pipeline (build → push → rolling restart)
- [ ] Add memory/CPU metrics from LXD state API
- [ ] Dark mode for the dashboard
