# kbox

A simple CLI for deploying applications to Kubernetes.

## What is kbox?

kbox simplifies Kubernetes deployments for developers. It handles building, deploying, debugging, and rollbacks - all from a single tool. Works with zero configuration (just a Dockerfile) or with a simple `kbox.yaml` for more control.

## Quick Start

### Install

```bash
go install github.com/bobbyrathoree/kbox/cmd/kbox@latest
```

### Zero-Config Deploy (just a Dockerfile)

```bash
cd my-app
kbox up
```

### With Configuration

```bash
kbox init        # Create kbox.yaml
kbox deploy      # Deploy to cluster
```

## Requirements

- Docker (for building images)
- kubectl configured with cluster access
- kind/minikube (for local development)

## Commands

| Command | Description |
|---------|-------------|
| `kbox up` | Build and deploy (zero-config) |
| `kbox deploy` | Deploy with kbox.yaml |
| `kbox dev` | Development loop (build/deploy/logs) |
| `kbox logs` | Stream logs with K8s events |
| `kbox status` | Show deployment status |
| `kbox shell` | Shell into running pod |
| `kbox pf` | Port-forward to pod |
| `kbox diff` | Preview changes |
| `kbox rollback` | Rollback to previous release |
| `kbox history` | Show release history |
| `kbox doctor` | Diagnose setup issues |
| `kbox render` | Show generated K8s YAML |
| `kbox init` | Create kbox.yaml |

## kbox.yaml Example

```yaml
apiVersion: kbox.dev/v1
kind: App
metadata:
  name: myapp
spec:
  image: myapp:latest
  port: 8080
  replicas: 2
  env:
    LOG_LEVEL: info
  healthCheck: /health
```

## Secrets

kbox supports secrets from `.env` files or SOPS-encrypted files:

```yaml
spec:
  secrets:
    fromEnvFile: .env.local
    fromSops:
      - secrets.enc.yaml
```

## License

MIT
