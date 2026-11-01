# Nuke Apply

[![Go Reference](https://pkg.go.dev/badge/github.com/bborbe/nuke-apply.svg)](https://pkg.go.dev/github.com/bborbe/nuke-apply)
[![CI](https://github.com/bborbe/nuke-apply/actions/workflows/ci.yml/badge.svg)](https://github.com/bborbe/nuke-apply/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/bborbe/nuke-apply)](https://goreportcard.com/report/github.com/bborbe/nuke-apply)

**An in-cluster apply service for the `bborbe/nuke` deploy repo.** It consumes an `apply` command from Kafka (CQRS, published through the trading command gateway) and runs the named component's own `make apply` against the cluster it runs in — so nobody runs a deploy by hand.

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
go get github.com/bborbe/nuke-apply
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
| `BRANCH` | Branch whose apply commands this instance consumes | `dev` |
| `NUKE_REPO_DIR` | Path to the `bborbe/nuke` checkout the apply targets run in | `/repo` |
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

The `k8s/` directory contains the manifests for one Deployment:

- `nuke-apply-deploy.yaml` — the service, an init container that shallow-clones `bborbe/nuke` at the
  dev branch into an `emptyDir`, and the TeamVault credentials
- `nuke-apply-rbac.yaml` — the `nuke-apply` ServiceAccount, a namespaced Role for the namespaced
  kinds the apply targets touch, and a narrow ClusterRole for the cluster-scoped kinds some
  components ship. Without these the pod cannot start and every apply would be Forbidden.
- `nuke-apply-known-hosts.yaml` — github.com's published SSH host keys, so the clone verifies the
  host rather than trusting it on first contact
- Service, Ingress, Secret, and KafkaUser configurations included
- **To activate**: change replicas from 0 → 1
- Requires two Secrets provisioned out of band: `nuke-apply-git` (deploy key for the nuke repo) and
  `nuke-apply-teamvault` (keys `url`, `user`, `pass`)

### Docker

Multi-stage Docker build included. See `Dockerfile` and `Makefile.docker`.

## Architecture

A CQRS consumer with the standard bborbe service shell around it:

- `main.go` — entry point; wires the Kafka client provider, producer, BoltDB and both run funcs
- `pkg/factory/` — `CreateCommandConsumer` wires the schema, the topic prefix and the executors
- `pkg/command/` — the `apply` executor: unmarshals the command, resolves the component, runs it
- `pkg/apply/` — the command payload, the component resolver, the docker refusal and the applier
- `pkg/handler/` — HTTP handlers for health, metrics and log level

### The apply command

The service consumes schema `nuke-apply-v1` (group `nuke`, kind `apply`, version `v1`) on the topic
`develop-nuke-apply-v1-request` and publishes one result per command to `develop-nuke-apply-v1-result`.

```
POST /api/1.0/command/nuke-apply-v1/apply
{"path": "alert-controller"}
```

`path` is a component directory relative to the repo root. The service then runs exactly that
component's own target — `make -C <repo>/<path> BRANCH=dev apply` — and never rewrites it.

Two refusals are by design and both publish a **Failure** result naming the reason:

- a component whose `apply` recipe invokes `docker` (there is no docker daemon in the pod, and an
  image build belongs to the release pipeline)
- a component with no `apply` target at all

Only `BRANCH=dev` is accepted. The applier refuses any other branch, so a mis-set environment
fails loudly instead of deploying somewhere unexpected.

## API Documentation

For complete API documentation of the handlers and packages, visit [pkg.go.dev](https://pkg.go.dev/github.com/bborbe/nuke-apply).

---

## Testing

Uses Ginkgo BDD framework with Gomega matchers. Run tests with `make test` or `ginkgo run ./...`.

Example tests are included to demonstrate testing patterns - copy and adapt these when building your service.

## Usage as Template

1. Copy this repository as a starting point for your new service
2. Update service name throughout (replace "nuke-apply")
3. Modify handlers and add your business logic
4. Update configuration in `example.env` and K8s manifests
5. Implement your specific Kafka consumers/producers and database schema

---

## License

BSD-style license. See [LICENSE](LICENSE) file for details.
