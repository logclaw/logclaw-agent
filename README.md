# LogClaw Agent

> A tiny Go robot that lives in your Kubernetes cluster, checks the health of your data stack every 30 seconds, and reports back to the LogClaw dashboard.

[![Go](https://img.shields.io/badge/Go-1.22-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Helm](https://img.shields.io/badge/Helm-3-0F1689?logo=helm&logoColor=white)](https://helm.sh)
[![Image: GHCR](https://img.shields.io/badge/Image-ghcr.io%2Flogclaw%2Fagent-blue?logo=github)](https://github.com/logclaw/logclaw-agent/pkgs/container/agent)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

---

## What is this?

Imagine sending a tiny robot into a building every 30 seconds to check if the electricity is on, the pipes aren't leaking, and all the security locks are working. The robot takes notes on everything it finds and sends a report back to headquarters. If anything is wrong, headquarters lights up a warning.

**The LogClaw Agent is exactly that robot — but for your software.**

It runs as a small Go program inside your Kubernetes cluster. Every 30 seconds it checks:

- **Kafka** — are your message queues backing up?
- **Flink** — are your data pipelines running or crashed?
- **OpenSearch** — is your search database healthy?
- **ExternalSecrets** — are your secrets syncing from the vault?

It packages up everything it found and sends it to the [LogClaw Platform](https://github.com/logclaw/logclaw-platform) (`app.logclaw.ai`), which stores it and shows it on a live dashboard for your team.

---

## How It Works

```
┌──────────────────────────────────────────────────────────────┐
│  Your Kubernetes Cluster                                     │
│                                                              │
│  logclaw-agent pod (:8080 health server)                     │
│    ├── watches Kafka CRDs     (consumer lag per topic)       │
│    ├── watches Flink CRDs     (job state + restart count)    │
│    ├── watches OpenSearch CRDs (cluster health)              │
│    └── watches ExternalSecret CRDs (secret sync status)      │
│                      │                                       │
│                      │  every 30 seconds                     │
│                      ↓                                       │
│           POST /api/metrics                                  │
│           Authorization: Bearer <JWT>                        │
└──────────────────────────────────────────────────────────────┘
                       ↓
┌──────────────────────────────────────────────────────────────┐
│  LogClaw Platform (app.logclaw.ai)                           │
│    stores metrics → shows on live dashboard                  │
└──────────────────────────────────────────────────────────────┘
```

The agent uses the **Kubernetes API** (via its ServiceAccount) to list and read custom resources. It never touches your application traffic, databases, or secrets directly — it only reads CRD status fields.

### Health Endpoints

The agent exposes two HTTP endpoints on `:8080` for Kubernetes probes:

| Endpoint | Purpose | Behavior |
|---|---|---|
| `GET /healthz` | Liveness probe | Always returns `200 OK` |
| `GET /readyz` | Readiness probe | Returns `503` until the first collection cycle completes, then `200 OK` |

---

## Prerequisites

| Tool | Version | Why |
|---|---|---|
| Kubernetes cluster | 1.25+ | Where the agent runs |
| [Helm](https://helm.sh/docs/intro/install/) | 3+ | Installing the agent |
| [Strimzi Kafka Operator](https://strimzi.io) | any | For Kafka metrics |
| [Flink Operator](https://nightlies.apache.org/flink/flink-kubernetes-operator-docs-stable/) | any | For Flink metrics |
| [OpenSearch Operator](https://github.com/opensearch-project/opensearch-k8s-operator) | any | For OpenSearch metrics |
| [External Secrets Operator](https://external-secrets.io) | 0.10.3+ | For ESO metrics (uses `v1` API) |
| LogClaw account | — | Get your tenant JWT from [app.logclaw.ai](https://app.logclaw.ai) |

> **Don't have all operators?** The agent is tolerant — if a CRD isn't installed, it skips that collector and reports what it can.

---

## Quick Start (Helm)

Deploy the agent into your cluster in under 5 minutes:

```bash
# 1. Get your tenant JWT from LogClaw dashboard → Settings

# 2. Create the JWT secret in your cluster
kubectl create secret generic logclaw-agent-jwt \
  --from-literal=jwt=<YOUR_JWT_TOKEN> \
  -n <YOUR_NAMESPACE>

# 3. Install the Helm chart
helm install logclaw-agent oci://ghcr.io/logclaw/charts/logclaw-agent \
  --namespace <YOUR_NAMESPACE> \
  --set global.tenantId=<YOUR_TENANT_SLUG> \
  --set global.namespace=<YOUR_NAMESPACE> \
  --set externalSecret.enabled=false

# 4. Verify it's running
kubectl logs -n <YOUR_NAMESPACE> deployment/logclaw-agent-logclaw-agent -f
```

You should see log lines like:
```
LogClaw agent starting: tenant=acme-corp namespace=logclaw-acme
Health server listening on :8080
Metrics pushed: kafka_topics=3 flink_jobs=2 eso_secrets=5
```

---

## Local Development

Run the agent locally against a `kind` cluster. Requires [Docker Desktop](https://www.docker.com/products/docker-desktop/) and [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation).

### 1 — Build the Docker image

```bash
git clone https://github.com/logclaw/logclaw-agent.git
cd logclaw-agent

# Generate go.sum (required for Docker build)
go mod tidy

# Build for your local architecture
docker build -t logclaw-agent:local .
```

### 2 — Create a kind cluster

```bash
kind create cluster --name logclaw-test
```

### 3 — Load the image into kind

```bash
kind load docker-image logclaw-agent:local --name logclaw-test
```

### 4 — Install the agent

```bash
# Create JWT secret (any string works for local dev)
kubectl create secret generic logclaw-agent-jwt \
  --from-literal=jwt=my-local-secret \
  -n default

# Install chart with local image
helm install logclaw-agent ./helm/logclaw-agent \
  --set image.repository=logclaw-agent \
  --set image.tag=local \
  --set image.pullPolicy=Never \
  --set global.tenantId=local-test \
  --set global.namespace=default \
  --set agent.saasUrl=http://host.docker.internal:3000/api/metrics \
  --set externalSecret.enabled=false
```

### 5 — Verify

```bash
# Check pod is Running with readiness probe passing
kubectl get pods -l app.kubernetes.io/name=logclaw-agent

# Check health endpoints
kubectl port-forward deployment/logclaw-agent-logclaw-agent 8080:8080 &
curl http://localhost:8080/healthz   # → ok
curl http://localhost:8080/readyz    # → ok (after first collection cycle)

# Watch agent logs
kubectl logs -f deployment/logclaw-agent-logclaw-agent
```

> **`host.docker.internal`** is the magic hostname that lets a container reach your Mac's localhost. It's available automatically in Docker Desktop and kind.

### Helm Lint & Template

The chart ships with CI values for testing without a real cluster:

```bash
# Lint
helm lint helm/logclaw-agent -f helm/logclaw-agent/ci/default-values.yaml

# Render templates (dry run)
helm template test-release helm/logclaw-agent \
  -f helm/logclaw-agent/ci/default-values.yaml
```

### Tear Down

```bash
helm uninstall logclaw-agent
kind delete cluster --name logclaw-test
```

---

## Configuration (Helm Values)

| Value | Default | Description |
|---|---|---|
| `global.tenantId` | `""` | **Required.** Your tenant slug from the LogClaw dashboard |
| `global.namespace` | `""` | Namespace where your LogClaw stack lives; defaults to the release namespace |
| `agent.saasUrl` | `https://app.logclaw.ai/api/metrics` | Platform endpoint to push metrics to |
| `agent.jwtSecretName` | `logclaw-agent-jwt` | Name of the Kubernetes Secret containing the JWT |
| `agent.jwtSecretKey` | `jwt` | Key inside the secret |
| `externalSecret.enabled` | `true` | Set to `false` to use a manually created Secret instead of ESO |
| `externalSecret.secretStoreName` | `logclaw-secret-store` | ESO ClusterSecretStore name |
| `externalSecret.remoteKey` | `""` | Auto-resolves to `logclaw/<tenantId>/agent/jwt` if empty |
| `image.repository` | `ghcr.io/logclaw/agent` | Container image repository |
| `image.tag` | `""` | Defaults to the chart's `appVersion` |
| `replicaCount` | `1` | Number of agent replicas (1 is sufficient) |
| `resources.requests.cpu` | `50m` | CPU request |
| `resources.requests.memory` | `64Mi` | Memory request |
| `resources.limits.cpu` | `200m` | CPU limit |
| `resources.limits.memory` | `128Mi` | Memory limit |

---

## Environment Variables

The agent reads these environment variables at runtime. The Helm chart sets them automatically.

| Variable | Required | Description |
|---|---|---|
| `LOGCLAW_TENANT_ID` | Yes | Your tenant slug (e.g. `acme-corp`) |
| `LOGCLAW_NAMESPACE` | No | Kubernetes namespace to watch for CRDs (defaults to `default`) |
| `LOGCLAW_SAAS_URL` | Yes | Platform endpoint — `https://app.logclaw.ai/api/metrics` |
| `LOGCLAW_AGENT_JWT` | Yes | Bearer token for authenticating with the platform |

---

## Metrics Collected

Every 30 seconds the agent collects:

| Collector | Kubernetes CRD | API Version | What it measures |
|---|---|---|---|
| **Kafka** | `Kafka.kafka.strimzi.io` | `v1beta2` | Per-topic consumer lag (messages behind) |
| **Flink** | `FlinkDeployment.flink.apache.org` | `v1beta1` | Job state (`RUNNING`, `FAILED`, etc.) |
| **OpenSearch** | `OpenSearchCluster.opensearch.opster.io` | `v1` | Cluster health (`green`/`yellow`/`red`), node counts |
| **ExternalSecrets** | `ExternalSecret.external-secrets.io` | `v1` | Sync status (`Ready`) + timestamp of last sync |

The collected data is sent as JSON to the platform:

```json
{
  "tenantId": "acme-corp",
  "collectedAt": "2025-01-01T00:00:00Z",
  "kafkaLag": { "payments.v1": 1234, "events.v1": 0 },
  "flinkJobs": [{ "name": "processor", "state": "RUNNING", "restarts": 0 }],
  "osHealth": { "status": "green", "numberOfNodes": 3, "numberOfDataNodes": 3 },
  "esoStatus": [{ "name": "kafka-secrets", "ready": true, "lastSync": "2025-01-01T00:00:00Z" }]
}
```

---

## Security

The agent is designed to run with minimal permissions:

- **Non-root**: Runs as UID 65534 (`nobody`) with `runAsNonRoot: true`
- **Read-only filesystem**: `readOnlyRootFilesystem: true` — no files can be written inside the container
- **No privilege escalation**: `allowPrivilegeEscalation: false`
- **Dropped capabilities**: All Linux capabilities dropped (`capabilities.drop: ["ALL"]`)
- **Health-only listener**: The agent listens on `:8080` exclusively for `/healthz` and `/readyz` Kubernetes probes — no other endpoints are exposed
- **Least-privilege RBAC**: The ServiceAccount can only `list` and `watch` the specific CRDs it needs. It has no write permissions and no access to Secrets, ConfigMaps, or other resources

---

## Docker Image

The image is built and pushed to GitHub Container Registry on every push to `main`:

```
ghcr.io/logclaw/agent:<version>
ghcr.io/logclaw/agent:latest
```

**Multi-arch**: Images are built for both `linux/amd64` and `linux/arm64` using Docker Buildx with QEMU emulation in CI.

**Multi-stage build**: Uses `golang:1.22-alpine` to compile a fully static binary, then copies it into `gcr.io/distroless/static:nonroot` — a minimal base image with no shell, no package manager, and no unnecessary files. The final image is under 10 MB.

To pull it manually:

```bash
docker pull ghcr.io/logclaw/agent:latest
```

---

## Project Structure

```
logclaw-agent/
├── main.go                    # Entry point: health server + collection loop + push
├── collectors/
│   ├── client.go              # Shared Kubernetes dynamic client (sync.Once)
│   ├── kafka.go               # Strimzi Kafka consumer lag collector
│   ├── flink.go               # Flink job state collector
│   ├── opensearch.go          # OpenSearch cluster health collector
│   └── eso.go                 # ExternalSecret sync status collector
├── Dockerfile                 # Multi-arch multi-stage build → distroless image
├── go.mod                     # Go module definition
├── go.sum                     # Go dependency checksums
├── helm/
│   └── logclaw-agent/
│       ├── Chart.yaml         # Chart metadata + version
│       ├── values.yaml        # Default configuration
│       ├── ci/
│       │   └── default-values.yaml  # CI/local-dev test values
│       └── templates/
│           ├── _helpers.tpl       # Template helpers
│           ├── deployment.yaml    # Agent pod spec (with health probes)
│           ├── serviceaccount.yaml
│           ├── clusterrole.yaml   # CRD read permissions
│           ├── clusterrolebinding.yaml
│           └── externalsecret.yaml  # ESO-managed JWT secret (v1 API)
└── .github/
    └── workflows/
        └── docker.yml         # Multi-arch build + push to GHCR
```

---

## Contributing

1. Fork the repo and create a branch: `git checkout -b feat/your-feature`
2. Make your changes to `main.go`, collectors, or the Helm chart
3. Run `helm lint helm/logclaw-agent -f helm/logclaw-agent/ci/default-values.yaml`
4. Build and test locally with kind (see [Local Development](#local-development))
5. Open a pull request

Please open an issue first for large changes so we can discuss the approach.

---

## License

MIT — see [LICENSE](LICENSE)
