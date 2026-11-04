# Nuke Apply

[![Go Reference](https://pkg.go.dev/badge/github.com/bborbe/k8s-apply.svg)](https://pkg.go.dev/github.com/bborbe/k8s-apply)
[![CI](https://github.com/bborbe/k8s-apply/actions/workflows/ci.yml/badge.svg)](https://github.com/bborbe/k8s-apply/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/bborbe/k8s-apply)](https://goreportcard.com/report/github.com/bborbe/k8s-apply)

**An in-cluster apply service for a stage of a deploy repo.** It consumes an `apply` command from Kafka (CQRS, published through the trading command gateway) and runs the named component's own `make apply` against the cluster it runs in — so nobody runs a deploy by hand. One instance serves one named stage, `dev` (the `bborbe/nuke` dev cluster) or `sun` (the `bborbe/sun` cluster).

---

## Table of Contents

* [Requirements](#requirements)
* [Technologies Demonstrated](#technologies-demonstrated)
* [Installation](#installation)
* [Quick Start](#quick-start)
* [Standard Endpoints](#standard-endpoints)
* [Development Commands](#development-commands)
* [Configuration](#configuration)
* [Deployment](#deployment)
  * [Kubernetes](#kubernetes)
  * [Docker](#docker)
* [Architecture](#architecture)
* [API Documentation](#api-documentation)
* [Testing](#testing)
* [Usage as Template](#usage-as-template)
* [License](#license)

---

## Requirements

- **Go 1.27+** (tested with Go 1.27.1 or later)
- **Docker** (for containerized builds)
- **Make** (for development commands)
- **Kafka** (optional, for message broker features)

---

## Technologies Demonstrated

- **Go**: Modern Go service with proper project structure
- **Docker**: Containerization with multi-stage builds
- **Kubernetes**: Complete deployment manifests (Deployment/StatefulSet options)
- **Kafka**: Message broker integration with producers/consumers
- **BoltDB**: Local embedded database for persistent storage
- **HTTP Services**: RESTful API with standard endpoints
- **Monitoring**: Prometheus metrics and Sentry error tracking
- **Security**: Comprehensive vulnerability scanning and license management

---

## Installation

As a template project, you typically copy this repository rather than installing it as a dependency. However, if you need to reference it:

```bash
go get github.com/bborbe/k8s-apply
```

## Quick Start

```bash
# Install dependencies
make deps

# Run the service locally
make run

# Run full development workflow
make precommit
```

## Standard Endpoints

| Endpoint | Description |
|----------|-------------|
| `/healthz` | Health check |
| `/readiness` | Readiness check |
| `/metrics` | Prometheus metrics |
| `/resetdb` | Database reset |
| `/setloglevel/{level}` | Dynamic log level adjustment |
| `/testloglevel` | Test logging at all levels |
| `/sentryalert` | Test Sentry integration |

## Development Commands

```bash
make ensure    # Dependency management (go mod tidy, verify)
make format    # Code formatting (gofmt, goimports-reviser, golines)
make generate  # Code generation
make test      # Run tests with coverage
make check     # Security and quality checks (vet, errcheck, vulncheck, osv-scanner, trivy)
make precommit # Complete development workflow
```

## Configuration

Configuration is managed through environment variables. See `example.env` for development defaults.

### Runtime Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `KAFKA_BROKERS` | Kafka broker addresses | `localhost:9092` |
| `LISTEN` | HTTP server listen address | `:8080` |
| `SENTRY_DSN` | Sentry error tracking DSN | (required for production) |
| `SENTRY_PROXY` | Sentry proxy URL | (optional) |
| `DATADIR` | Database storage directory | (required) |
| `BATCH_SIZE` | Kafka batch consume size | `1` |
| `STAGE` | Stage whose apply commands this instance consumes (`dev` or `sun`) | `dev` |
| `REPO_DIR` | Path to the deploy repo checkout the apply targets run in | `/repo` |
| `SUN_GATEWAY_URL` | Sun command gateway base URL; required on `dev`, where a healthy apply promotes the component through it | (empty on `sun` and `prod`) |
| `SUN_GATEWAY_INITIATOR` | Initiator the sun command gateway attributes the promotion to | (empty on `sun` and `prod`) |
| `SUN_GATEWAY_USER` | User the sun command gateway authenticates the promotion as; required on `dev` | (empty on `sun` and `prod`) |
| `SUN_GATEWAY_PASSWORD` | Password for that user; required on `dev` | (empty on `sun` and `prod`) |
| `TEAMVAULT_URL` | TeamVault base URL, passed through to `teamvault-cli` | (from Secret) |
| `TEAMVAULT_USER` | TeamVault user for this stage | (from Secret) |
| `TEAMVAULT_PASS` | TeamVault password for this stage | (from Secret) |

`teamvault-cli` takes `TEAMVAULT_URL` / `TEAMVAULT_USER` / `TEAMVAULT_PASS` as the defaults for its
`--teamvault-*` flags, so the component Makefiles stay untouched. The image deliberately ships **no**
`~/.config/teamvault-cli/config.json`: when that file exists its values overwrite the environment,
which would silently defeat per-stage credentials.

### Build/Deployment Configuration

| Variable | Description |
|----------|-------------|
| `DOCKER_REGISTRY` | Docker registry for image publishing |
| `IMAGE` | Docker image name |
| `CLUSTER_CONTEXT` | Kubernetes cluster context |
| `BUILD_GIT_VERSION` | Build version (auto-set in CI/Docker) |
| `BUILD_GIT_COMMIT` | Build commit hash (auto-set in CI/Docker) |
| `BUILD_DATE` | Build timestamp (auto-set in CI/Docker) |

## Deployment

### Kubernetes

**This repo is not deployed from here.** Its manifests live in the deploy repo, as a component:

```
bborbe/nuke/k8s-apply/
```

Deploy with that component's own target, which is how every other service in the fleet deploys:

```bash
cd ~/Documents/workspaces/nuke-dev/k8s-apply && BRANCH=dev make apply
```

The component renders every manifest through `teamvault-cli config parse` and applies it with the
pod's own ServiceAccount. It ships the Deployment, the `k8s-apply` ServiceAccount with its Role,
github.com's pinned SSH host keys, and both Secrets — rendered from TeamVault, never hand-created.

A `k8s/` directory used to live here. It was skeleton leftover: its `Makefile` included a
`../Makefile.env` that did not exist, so `make apply` there failed immediately, and it rendered
through the retired `teamvault-config-parser` with a `--context` flag the cluster wrappers forbid.
It was removed rather than repaired — a second, broken deploy path beside the real one is not
neutral, it reads as live.

### Docker

Multi-stage Docker build included. See `Dockerfile` and `Makefile.docker`.

## Architecture

A CQRS consumer with the standard bborbe service shell around it:

- `main.go` — entry point; wires the Kafka client provider, producer, BoltDB and both run funcs
- `pkg/factory/` — `CreateCommandConsumer` wires the schema, the topic prefix and the executors
- `pkg/command/` — the `apply` executor: unmarshals the command, resolves the component, runs it
- `pkg/apply/` — the command payload, the component resolver with its three refusals, and the applier
- `pkg/handler/` — HTTP handlers for health, metrics and log level

### The apply command

The service consumes schema `k8s-apply-v1` (group `nuke`, kind `apply`, version `v1`) on the topic
`develop-k8s-apply-v1-request` and publishes one result per command to `develop-k8s-apply-v1-result`.

```
POST /api/1.0/command/k8s-apply-v1/apply
{"path": "alert-controller"}
```

`path` is a component directory relative to the repo root. The service then runs exactly that
component's own target and never rewrites it. The stage decides the arguments: the `dev` stage runs
`make -C <repo>/<path> BRANCH=dev apply`, while the `sun` stage passes no `BRANCH` at all
(`make -C <repo>/<path> apply`), because sun's Makefiles select their environment unconditionally.

Three refusals are by design and all publish a **Failure** result naming the reason:

- a component whose `apply` recipe invokes `docker` (there is no docker daemon in the pod, and an
  image build belongs to the release pipeline). The refusal reads the apply target's own recipe
  lines only, so a prerequisite that invokes docker is not refused — it fails at apply time instead.
- a component with no `apply` target at all
- a component excluded by name. `teamvault` is the one entry: its `apply` target only runs `envsubst`
  and `kubectl apply` over its own manifests, and the TeamVault reads live in its separate,
  operator-only `secrets` target — so applying it would redeploy the component that holds every
  secret in the cluster without touching a single TeamVault grant. The exclusion is checked before
  the component is looked for on disk, so a checkout that dropped the directory cannot silently
  un-exclude it.

Only the stages `dev` and `sun` are accepted; any other `STAGE` value is refused, so a mis-set
environment fails loudly instead of deploying somewhere unexpected. Two behaviours follow from the
stage: `dev` passes `BRANCH=dev` to make, while `sun` passes no branch argument and instead refuses
to apply unless the checkout's HEAD contains `origin/master`.

## API Documentation

For complete API documentation of the handlers and packages, visit [pkg.go.dev](https://pkg.go.dev/github.com/bborbe/k8s-apply).

---

## Testing

Uses Ginkgo BDD framework with Gomega matchers. Run tests with `make test` or `ginkgo run ./...`.

Example tests are included to demonstrate testing patterns - copy and adapt these when building your service.

## Usage as Template

1. Copy this repository as a starting point for your new service
2. Update service name throughout (replace "k8s-apply")
3. Modify handlers and add your business logic
4. Update configuration in `example.env` and K8s manifests
5. Implement your specific Kafka consumers/producers and database schema

---

## License

BSD-style license. See [LICENSE](LICENSE) file for details.
