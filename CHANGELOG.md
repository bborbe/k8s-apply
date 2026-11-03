# Changelog

All notable changes to this project will be documented in this file.

Please choose versions by [Semantic Versioning](http://semver.org/).

## Unreleased

- feat: roll a component back when its post-apply health check fails. `make apply` exits 0 the moment the manifests land and never observes the rollout, so an apply that leaves the stage broken used to report success. The applier now records every Deployment's container images before the apply, waits up to `HEALTH_TIMEOUT` (default `5m`) for each rollout to reach `updatedReplicas == availableReplicas == replicas` with no unavailable replica, and on a failed check patches the recorded images back. The Failure entry on the result topic names the failed check (with the replica counters) and the images restored, or why nothing was restored (new Deployment, image unchanged)
- feat: read and patch Deployments through an in-cluster client scoped to the pod's own namespace (`NAMESPACE` overrides it); the existing namespaced Role already grants `apps/deployments` get + patch. Outside a cluster the service now fails at startup instead of skipping the check
- feat: refuse to apply a component whose manifest declares a Deployment with a templated name, since its pre-apply image cannot be captured

## v0.13.1

- chore: update github.com/bborbe/boltkv to v1.16.6, github.com/bborbe/kafka to v1.29.11, github.com/bborbe/kv to v1.21.28, github.com/bborbe/run to v1.11.10, github.com/bborbe/time to v1.27.25

## v0.13.0

- refactor: rename the service from `nuke-apply` to `k8s-apply` — repo, Go module, image, schema group, and the `NUKE_REPO_DIR` env var (now `REPO_DIR`). Nothing about the service was nuke-specific except its names: it takes its stage from `STAGE` and its checkout from a path, and the sun task already ran the same image against `bborbe/sun`. Renaming while dev is the only live consumer keeps it a one-cluster change; every later stage would otherwise add a schema migration and a topic drain. The schema id becomes `k8s-apply-v1`, so the topics become `develop-k8s-apply-v1-{request,event,result,history}` and the gateway path `/api/1.0/command/k8s-apply-v1/apply`.

## v0.12.1

- fix: strip make's recipe-line prefixes (`@`, `-`, `+`) before looking for a docker invocation, so a component whose `apply:` target writes `@docker manifest inspect` is refused like any other docker-running component instead of passing the check and reaching a shell with no daemon; the strip applies only to the first physical line of a recipe entry, because a leading `-` on a backslash-continued line is a shell flag and removing it would corrupt the command
- fix: say `invokes docker in its apply target` rather than `builds a container image`, since the refusal covers registry reads as well as builds

## v0.12.0

- feat: add the `prod` stage — make branch `master`, cqrs branch `prod` — because the branch handed to make and the branch whose topics this instance consumes are different facts, and both happen to yield the topic prefix `master`, so one shared variable would read correct and consume the wrong topic the day either changes
- feat: refuse a command on `prod` unless it carries exactly one recorded operator approval, verified against the attention store before the component is even resolved — so a refused command never reaches the filesystem, let alone a shell
- feat: verify the deploy-repo checkout's `HEAD` equals the commit the approval covers before running make, so the commit binding is a control rather than paperwork
- feat: bind the approval to the action with `sha256("apply\n" + stage + "\n" + component + "\n" + commit)`, compared against the attention item's `dedup_key` (machine-exact) rather than its `payload` (operator-facing display text), so an approval cannot be replayed onto another component, another commit, or another stage
- feat: bound the approval window by the store's own `answered_at` (5-minute skew) and cap the declared lifetime at 24 h, so `approved_at` and `expires_at` cannot be whatever the publisher typed
- feat: refuse two approvals by name rather than as a generic unmarshal error, name an empty list as no approval rather than as too many, and refuse an approval past its `expires_at`
- feat: fail closed when a stage that requires an approval is wired with the noop approver — a prod deployment that forgot to configure one refuses every apply instead of silently skipping the gate
- feat: source `STAGE` from `example.env` and read `ATTENTION_URL` / `ATTENTION_TOKEN`, so a rendered manifest is never empty and a prod pod fails at startup rather than skipping the gate
- fix: mark the attention token `display:"length"` so the startup config dump prints its length instead of the bearer token itself
- feat: refuse a plain-http `ATTENTION_URL` unless the host is an in-cluster Service, so the bearer token is never sent in clear text to a routable host

## v0.11.0

- feat: refuse `path: teamvault` by design. Its `apply:` target only runs `envsubst` and `kubectl apply` over its own manifests — the TeamVault reads live in its separate operator-only `secrets:` target — so applying it would redeploy the component that holds every secret in the cluster without touching a single TeamVault grant. Neither the docker refusal nor a TeamVault grant list closes that path; this refusal is the only thing that does.

## v0.10.0

- feat: generalise the apply service around a `Stage` so one instance deploys one named stage, deriving the make `BRANCH` argument, checkout guard and approval decision from it
- feat: add the `sun` stage, which passes no `BRANCH` to make and refuses to apply unless the deploy checkout's HEAD contains `origin/master`
- refactor: replace the `BRANCH` configuration and the `DevBranch` hard refusal with `STAGE` (`dev` or `sun`) and `ParseStage`, refusing an unknown stage instead of silently falling back
- test: cover every stage's branch, topic prefix, master-HEAD guard and approval predicate, plus the `application.Stage` tags
- refactor: derive the stage's branch-argument decision from ApplyBranch so a future stage passes BRANCH without editing the applier

## v0.9.0

- feat: consume an `apply` command (schema `nuke-apply-v1`) from the branch-scoped CQRS request topic and run the named component's own `make BRANCH=dev apply` target, publishing one result per command
- feat: refuse a component whose apply recipe invokes `docker`, and one with no apply target, with a Failure result naming the reason
- feat: add `pkg/apply` (command payload, component resolver, applier) and `pkg/command` (the executor), with counterfeiter fakes
- feat: build the runtime image on alpine instead of scratch so it carries `git`, `make`, `bash`, `kubectl` and `teamvault-cli`, and pin kubectl to the nuke cluster's server version
- feat: wire `BRANCH`, `NUKE_REPO_DIR` and the `TEAMVAULT_*` variables into the deployment and clone the deploy repo in an init container
- feat: add `k8s/nuke-apply-rbac.yaml` — the `nuke-apply` ServiceAccount with a namespaced Role plus a narrow ClusterRole for the cluster-scoped kinds some components ship, so the pod can start and its kubectl calls are not Forbidden
- security: pin the kubectl download to a recorded SHA-256 instead of trusting a checksum fetched from the same origin, and run the service as non-root with no privilege escalation
- security: pin github.com's SSH host keys from `api.github.com/meta` and clone with `StrictHostKeyChecking=yes`, so the repo whose Makefiles run with this pod's credentials is verified rather than trusted on first contact
- fix: reject a command path with leading or trailing whitespace instead of validating a trimmed copy and then resolving the original
- chore: drop the duplicate StatefulSet manifest — the Deployment is the only shape this service ships
- chore: bump golangci-lint to v2.14.0 — v2.13.1 cannot decode Go 1.27.3 export data and failed the lint gate in CI

## v0.8.0

- feat: add `pkg/textwrap` with dependency-free word wrapping (`Wrap`, `WrapString`), line indentation (`Indent`, `IndentLines`), and `Dedent` helpers
- docs: correct the README Go requirement from 1.25+ to 1.27+ — `go.mod` and the `Dockerfile` have been on `1.27.1` since the Go 1.27 bump, so the stated minimum was stale

## v0.6.0

- feat: serve `/healthz` as JSON (`{"status":"ok"}` with `Content-Type: application/json`) so consumers and monitoring tooling can parse the liveness response; route path and k8s liveness probe unchanged
- feat: opt new repos into `goUpdate.autoUpdate` in `.maintainer.yaml` so `github-update-go-watcher` may file Go-version update tasks for them
- fix: Reorder `format` target in `Makefile.precommit` so `gofmt -w` runs last (after goimports-reviser and golines), normalizing golines' wrapping so the gofmt lint check passes on the Go 1.27 bump
- chore: Pin golangci-lint to v2.13.1 (fixes staticcheck `buildir` panic on the Go 1.27 AST) and errcheck to v1.20.0 (fixes `package "context" without types`) in `tools.env`

## v0.5.1

- chore: e2e test of the release-check webhook (2026-08-22)

## v0.5.0

- feat: add Scaler interface with counterfeiter directive and ParseScalerDefault helper

## v0.4.13

- update Go to 1.26.6 and update dependencies

## v0.4.12

- update Go to 1.26.6 and update dependencies (fixes GO-2026-6179, GO-2026-6180, CVE-2026-56864, CVE-2026-56865)

## v0.4.11

- chore(dev): point `.envrc` `--teamvault-config` at `~/.config/teamvault-cli/config.json` (legacy `~/.teamvault.json` removed, so the prior path failed with an empty-URL error)

## v0.4.10

- chore(deps): update Go module dependencies to latest compatible versions via go get -u ./... + go mod tidy

## v0.4.9

- chore(deps): update github.com/getsentry/sentry-go from v0.47.0 to v0.48.0

## v0.4.8

- chore(dev): migrate `.envrc` to `teamvault-cli password` (from the dropped v4 `teamvault-password` binary); keep `--teamvault-config` so the personal-instance key stays instance-pinned

## v0.4.7

- chore: Update Go module dependencies to latest compatible versions via go get -u ./... + go mod tidy

## v0.4.6

- Bump Go 1.26.5, Alpine 3.24, and all bborbe/vendor deps
- Make -race opt-in via ENABLE_RACE to avoid CI flakes
- Surface real govulncheck errors, swallow known x/tools panic
- Exclude no-fix advisory GO-2026-5932 (x/crypto/openpgp)
- Remove unused Claude GitHub Action workflows

## v0.4.5

- chore: canary — verify maintainer spec 065 agent lenient unreleased-section detection. Uses lowercase `## unreleased` (typo'd heading) — the old strict-match agent would silently halt; the new lenient agent should detect this as the unreleased section (first non-version H2 wins), rewrite to `## v0.4.5`, commit, and tag.
- chore: retry — first dev e2e job hung in ai_review (MiniMax CLI stuck), this commit refreshes the SHA to spawn a fresh Job

## v0.4.4

- chore: canary — verify maintainer spec 061 prod rollout (prod release watcher now excludes nuke-apply; dev is sole releaser; expected: this SHA gets exactly one `Release bborbe-nuke-apply <sha>.md` write to the vault, zero new `_conflicts/tasks/` entries, and a `v0.4.x` tag from the dev releaser)
- chore: e2e test 059 flag=false path (header-rename only, no rewrite)
- docs: clarify Unreleased format in CHANGELOG header comment
- chore: re-fire (previous task dispatch lost due to vault readiness blip)
- chore: third attempt after executor failed to dispatch prior 2 tasks

## v0.4.3

- chore: bump golang.org/x/net v0.55.0 → v0.56.0
- chore: bump golang.org/x/sys v0.30.0 → v0.31.0
- chore: bump golang.org/x/text v0.20.0 → v0.21.0
- chore: bump bborbe/* libs (run, errors, http, kafka, kv, log, metrics, sentry, service, time, boltkv, argument, collection, math, parse, validation)
- chore: bump golang 1.26.3 → 1.26.4 (go.mod + Dockerfile)

## v0.4.2

- chore: trigger github-releaser ai_review failure-path smoke test
- bump bborbe/kafka v1.22.15 → v1.23.0; indirect deps refreshed via go mod tidy

## v0.4.1

- chore: trigger github-releaser pre-push-guard e2e on dev

## v0.4.0

- add `.maintainer.yaml` opting into github-releaser auto-release (release.autoRelease: true)
- chore: re-trigger github-releaser e2e after clone-url HTTPS fix
- chore: re-trigger github-releaser e2e after clone default-branch fix
- chore: trigger first github-releaser e2e (move master so the release watcher emits)

## v0.3.17

- bump github.com/bborbe/run v1.9.26 → v1.9.27
- bump golang.org/x/net v0.54.0 → v0.55.0
- add Makefile fix target for bulk dep updates

## v0.3.16

- Bump bborbe/* deps (boltkv, kv, service, time, kafka, http, run, etc.)
- Upgrade golang.org/x/* (crypto, net, sys, text)
- Upgrade IBM/sarama v1.47.0 → v1.48.1 and test libs (ginkgo, gomega)
- vulncheck: add -show verbose on unexpected findings
- trivy: support project-level .trivy-secret.yaml config

## v0.3.15

- chore: migrate to tools.env + Makefile @version pattern; remove tools.go and obsolete replace block; go.mod reduced from 467 to 67 lines
- chore: bump bborbe/* direct deps to latest (boltkv v1.12.6, errors v1.5.13, http v1.26.11, kafka v1.22.12, kv v1.19.8, log v1.6.13, metrics v0.5.3, run v1.9.24, sentry v1.9.17, service v1.9.11, time v1.25.11)
- chore: remove all CVE suppressions from .osv-scanner.toml (all entries unused after dep graph cleanup)
- fix: add GODEBUG=gotypesalias=1 to errcheck invocation to support generic type aliases used by kv v1.19.x
- chore: bump golang base image 1.26.2→1.26.3; add revive file-length-limit rule to golangci.yml

## v0.3.14

- Migrate `BuildInfoMetrics` from inlined `pkg/build-info-metrics.go` to shared `github.com/bborbe/metrics` v0.5.1; call site now passes `(version, commit, buildDate)` so `build_info{version, commit}` labels populate from the build args
- Remove `pkg/build-info-metrics.go`, its tests, and `mocks/build-info-metrics.go` (moved to shared package)
- Bump indirect dependencies (`go-git`, `golang.org/x/telemetry`, `golang.org/x/vuln`)

## v0.3.13

- test: add Ginkgo/Gomega tests for BuildInfoMetrics covering non-nil and nil buildDate branches
- update test suites with GinkgoConfiguration timeout and -buildvcs=false

## v0.3.12

- chore: update go.mod dependencies (bborbe/* libs, anthropic-sdk-go, golang.org/x/*, google APIs, containerd, otel, modernc)

## v0.3.11

- bump golang base image to 1.26.2
- update go.mod: boltkv, run, time, sentry-go, counterfeiter, validation, sys
- add vuln/CVE excludes to osv-scanner, trivyignore
- improve vulncheck to filter known ignored findings

## v0.3.10

- update bborbe/* deps (errors, http, kafka, log, sentry, service)
- update indirect deps (grpc, google APIs, genai, containerd)
- add replace directives for anthropic-sdk-go and other modules

## v0.3.9

- Update dependencies (boltkv, kv, run, go-git, anthropic-sdk-go, and others)
- Replace ginkgolinter/types replace directive with opencontainers/runtime-spec
- Enable dark-factory autoRelease

## v0.3.8

- Update go-git/go-git to v5.17.1 (fix security vulnerabilities)
- Clean up unused osv-scanner ignore entries
- Add .trivyignore for docker indirect dep CVEs
- Improve trivy Makefile target with .trivyignore support and vendor skip

## v0.3.7

- Update bborbe/* libraries (boltkv, errors, http, kafka, kv, log, run, sentry, service, time)
- Update golangci-lint to v2.11.4 and gosec to v2.25.0
- Update docker, containerd, moby dependencies
- Update golang.org/x/* and opentelemetry packages
- Remove unused exclude directives and clean up go.mod

## v0.3.6

- chore: validate project health — all tests, linting, and precommit checks pass

## v0.3.5

- go mod update

## v0.3.4

- go mod update

## v0.3.3

- remove accidentally committed docs/improvements.md

## v0.3.2

- go mod update

## v0.3.1

- Update GitHub workflows to v1 plugin system
- Simplify Claude Code action with inline conditions
- Add ready_for_review and reopened triggers

## v0.3.0

- optimize Docker build with BuildKit cache mounts for dependencies and build artifacts
- remove go mod vendor step from build process
- remove --no-cache flag to leverage Docker layer caching
- sanitize branch names in Docker tags (replace / with -)
- fix k8s Makefile include paths

## v0.2.14

- Update Go to 1.25.7
- Update github.com/bborbe/errors to v1.5.2
- Update github.com/bborbe/log to v1.6.2
- Update github.com/bborbe/sentry to v1.9.6
- Update github.com/bborbe/time to v1.22.0

## v0.2.13

- Update Go dependencies (sentry, time, ginkgo, gomega, etc.)
- Remove Gemini CI workflows
- Add doc comment to BuildInfoMetrics

## v0.2.12

- add build info Prometheus metrics with timestamp tracking
- change BuildDate field to use libtime.DateTime type
- add GoDoc comments for exported types and functions

## v0.2.11

- update golang to 1.25.6
- update alpine to 3.23
- update github.com/bborbe/* dependencies
- update getsentry/sentry-go to v0.41.0
- update IBM/sarama to v1.46.3

## v0.2.10
- Update Docker image name from bborbe/nuke-apply to bborbe/nuke-apply

## v0.2.9
- Fix module path URLs from nuke-apply to nuke-apply across all files
- Update all dependencies to latest versions

## v0.2.8
- Fix security vulnerabilities by updating Go version and dependencies
- Disable unparam linter for tests to resolve build failures

## v0.2.7

- Update Go version from 1.25.3 to 1.25.4
- Update containerd from 1.7.27 to 1.7.29 to fix security vulnerabilities
- Update opencontainers/selinux from 1.12.0 to 1.13.0 to fix security vulnerabilities
- Update cyphar/filepath-securejoin from 0.5.0 to 0.6.0
- Add gomodprepare Makefile target for consistent go.mod configuration
- Add additional k8s.io version excludes to go.mod

## v0.2.6

- Add depguard rule to block deprecated io/ioutil package
- Add depguard rule to block deprecated golang.org/x/lint/golint package

## v0.2.5

- Use relative path in gexec.Build test for improved portability

## v0.2.4

- Add OCI image labels to Dockerfile for better container metadata
- Enable race detection in test suite for improved concurrency safety testing
- Add table of contents to README for easier navigation
- Expand configuration documentation with runtime and build/deployment variables
- Clean up unused dependencies (move k8s.io/code-generator and gogen-avro to indirect)
- Remove unused tool imports from tools.go

## v0.2.3

- Remove codecov badge from README

## v0.2.2

- Add standard Go project badges (Go Reference, CI, Go Report Card, codecov)
- Add Installation, API Documentation, and License sections to README
- Add Ginkgo v2 to tools.go for consistent test framework dependency tracking
- Improve README structure with horizontal rules for better visual separation

## v0.2.1

- Add GoDoc comments for all exported handler and factory functions
- Improve error handling consistency by replacing errors.Wrapf with errors.Wrap
- Add pkg test suite setup with Ginkgo v2 and Gomega

## v0.2.0

- Add build metadata support (Git version, commit hash, and build timestamp)
- Docker container now exposes build information via environment variables
- Build metadata automatically injected during Docker build process

## v0.1.0

- Initial release
