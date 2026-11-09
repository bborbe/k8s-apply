---
status: completed
spec: [002-cluster-build-arm]
summary: Added a cluster build arm to Makefile.docker keyed on BUILD_TARGET, leaving the local build, upload, and clean behavior unchanged, and recorded the change under CHANGELOG Unreleased.
execution_id: d38d074f-a3a3-5298-a67d-ea6660c0f711-exec-006-spec-002-makefile-docker-arm
dark-factory-version: dev
created: "2026-11-09T06:49:14Z"
queued: "2026-11-09T07:01:18Z"
started: "2026-11-09T07:01:19Z"
completed: "2026-11-09T07:09:55Z"
branch: dark-factory/cluster-build-arm
---

# feat: add a cluster build arm to Makefile.docker

<summary>
- The repository gains a second way to build its image: inside a cluster
- With no build target set, `make build` behaves exactly as before — same command, same `:dev` tag
- Naming a cluster instead runs the same build as a Kubernetes Job in that cluster
- The cluster build pushes to the same Docker Hub repository the local build tags
- Both paths derive the image repository from the same two values, so they cannot drift apart
- A cluster build without a version tag is refused before any Job is created
- Upload and cleanup keep working locally and become harmless no-ops for a cluster build
- The build target defaults to local, so existing callers need no change
</summary>

<objective>
Give this repository a second image-build path without changing the first: keep `make build` building locally exactly as it does today, and make `make build BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>` render and run the same build as a Kubernetes Job in that cluster, pushing to the same Docker Hub repository the local arm tags.
</objective>

<context>
Read `docs/dod.md` (this project's definition of done) for conventions.

Read these files before editing:

- `Makefile.docker` — the file being changed. The local `build`, `upload`, `clean`, `apply` and `buca` targets live here.
- `Makefile` — `Makefile.docker` is included at line 3, `example.env` at line 4, and `SERVICE = bborbe/k8s-apply` is assigned at line 6. Recipes expand lazily, so `$(SERVICE)` and `$(DOCKER_REGISTRY)` are correct inside a recipe but empty at parse time.
- `Makefile.variables` — defines `BRANCH`, `HOSTNAME`, `ROOTDIR`, `TEAMVAULT`. There is no `VERSION` default anywhere in this repo.
- `example.env` — exports `DOCKER_REGISTRY=docker.io`, `IMAGE=bborbe/k8s-apply` (no registry prefix) and `BRANCH=dev`.
- `hack/build-cluster.sh` — the runner the cluster arm invokes. Read its `require` list (lines 37–42) and its accepted environment variables in the header comment (lines 3–7). It defaults `NAMESPACE=build`, `PUSH=true`, `TIMEOUT=3600`, `DOCKERFILE=Dockerfile` and `DOCKER_REGISTRY=docker.io`.
- `hack/cluster-alias.sh` — sourced by the runner; resolves `BUILD_TARGET` to a kubeconfig and context. It only sets variables and defines `kc()`; it makes no kubectl call.
- `hack/build-job.yaml` — the Job template the runner renders. Its header lists the template tokens.

The five `hack/` files are already committed on this branch, copied verbatim from their named source repositories. Do NOT modify any of them. If one seems to need a change, that change belongs in the source repository and a later copy.

The coding plugin's `/home/node/.claude/plugins/marketplaces/coding/docs/go-dockerfile-guide.md` shows a different canonical `Makefile.docker` block (VERSION-based, `--pull --no-cache`, `bborbe/$(SERVICE)`). Do NOT adopt that block. This repo's local recipe is frozen by the spec and must stay byte-for-byte as it is.
</context>

<requirements>
1. Add the following variable defaults at the top of `Makefile.docker`, before the first `ifeq` in the file. The ordering of `BUILD_TARGET` is load-bearing: every guard in the file keys on it, so an assignment below the guards would make the local arm vanish for a plain `make build`.

```make
# BUILD_TARGET selects the build arm. Assign it before the first ifeq below:
# every guard in this file keys on it, so an assignment placed below them
# makes the local arm vanish for a plain `make build`.
BUILD_TARGET ?= local
# Fixed defaults for the cluster arm. These are not operator-facing knobs.
REPO_URL ?= https://github.com/bborbe/k8s-apply.git
PUSH_SECRET ?= buildkit-push
DOCKERFILE ?= Dockerfile
```

`BUILD_TARGET`, `REPO_URL`, `PUSH_SECRET` and `DOCKERFILE` are the only new variables. Do not add a `VERSION` default, a `TIMEOUT`, a `DOCKER_REGISTRY` override, an `ALLOW_*` flag, or any other knob. `NAMESPACE` and `PUSH` are stated as literals in the recipe below, not as variables.

2. Wrap the existing `build` target in an `ifeq ($(BUILD_TARGET),local)` / `else` / `endif`. The `local` branch keeps the existing recipe exactly as it is today — same lines, same order, same five `--build-arg` lines, same `-t $(DOCKER_REGISTRY)/$(SERVICE):$(BRANCH)`, same `-f Dockerfile .`. The `else` branch dispatches to the cluster runner.

The dispatch uses the backslash-continued environment-prefix form, one `VAR='$(VAR)' \` per line, with `hack/build-cluster.sh` last on its own line and no `@` prefix anywhere. The `^[[:space:]]*hack/build-cluster\.sh` anchor depends on this exact shape; a single-line `VAR=… hack/build-cluster.sh` prefix would fail it. In the block below, every recipe line starts with a TAB.

```make
.PHONY: build
ifeq ($(BUILD_TARGET),local)
build:
	DOCKER_BUILDKIT=1 \
	docker build \
	--rm=true \
	--platform=linux/amd64 \
	--build-arg DOCKER_REGISTRY=$(DOCKER_REGISTRY) \
	--build-arg BRANCH=$(BRANCH) \
	--build-arg BUILD_GIT_VERSION=$$(git describe --tags --always --dirty) \
	--build-arg BUILD_GIT_COMMIT=$$(git rev-parse --short HEAD) \
	--build-arg BUILD_DATE=$$(date -u +%Y-%m-%dT%H:%M:%SZ) \
	-t $(DOCKER_REGISTRY)/$(SERVICE):$(BRANCH) \
	-f Dockerfile .
else
build:
	BUILD_TARGET='$(BUILD_TARGET)' \
	NAMESPACE='build' \
	REPO_URL='$(REPO_URL)' \
	REF='$(REF)' \
	TAG='$(VERSION)' \
	DOCKERFILE='$(DOCKERFILE)' \
	IMAGE='$(DOCKER_REGISTRY)/$(SERVICE)' \
	PUSH='true' \
	PUSH_SECRET='$(PUSH_SECRET)' \
	hack/build-cluster.sh
endif
```

Notes on the values:

- `IMAGE` is `$(DOCKER_REGISTRY)/$(SERVICE)`, which with the repo defaults is `docker.io/bborbe/k8s-apply`. Do NOT compose it from `$(IMAGE)` — `example.env` exports `IMAGE=bborbe/k8s-apply` with no registry prefix.
- `TAG` is `$(VERSION)`, caller-supplied. There is no default: an empty `VERSION` reaches the runner's `require TAG` and is refused there.
- `REF` is caller-supplied. Do not give it a default.
- `NAMESPACE='build'` and `PUSH='true'` are the runner's own defaults stated explicitly, so the recipe does not inherit them silently. Keep them as literals.

3. Put the `ifeq ($(BUILD_TARGET),local)` guard INSIDE the `upload` and `clean` recipes, not around the whole rule. Each target must stay defined with an empty body for a cluster target, so `make -n upload BUILD_TARGET=sun` and `make -n clean BUILD_TARGET=sun` exit 0 instead of failing with `No rule to make target`. In the block below, every recipe line starts with a TAB.

```make
.PHONY: upload
upload:
ifeq ($(BUILD_TARGET),local)
	docker push $(DOCKER_REGISTRY)/$(SERVICE):$(BRANCH)
endif

.PHONY: clean
clean:
ifeq ($(BUILD_TARGET),local)
	docker rmi $(DOCKER_REGISTRY)/$(SERVICE):$(BRANCH) || true
	docker builder prune --max-used-space 21474836480 -f || true
endif
```

4. Leave the `apply` target and the `buca` target unchanged. `buca` stays `buca: build upload clean apply`; under a cluster target `build` runs in the cluster, `upload` and `clean` are no-ops, and `apply` recurses into sub-Makefiles that do not exist (a no-op either way).

5. Do not add a top-level `$(error …)` to the arm. `Makefile.docker` is included unconditionally, so an ungated error would fire for every goal, including `make test` and `make precommit`.

6. Do not add any parse-time `$(shell …)` or `:=` that reads `$(SERVICE)`, `$(DOCKER_REGISTRY)` or `$(IMAGE)`. Those values are empty when `Makefile.docker` is parsed; the arm may reference them only inside recipes. `ifeq` is the one parse-time construct the arm uses, and it keys only on the literal `BUILD_TARGET`.

7. The resulting `Makefile.docker` must be exactly the following. Every recipe line starts with a TAB, not spaces.

```make
# BUILD_TARGET selects the build arm. Assign it before the first ifeq below:
# every guard in this file keys on it, so an assignment placed below them
# makes the local arm vanish for a plain `make build`.
BUILD_TARGET ?= local
# Fixed defaults for the cluster arm. These are not operator-facing knobs.
REPO_URL ?= https://github.com/bborbe/k8s-apply.git
PUSH_SECRET ?= buildkit-push
DOCKERFILE ?= Dockerfile

DIRS += $(shell find */* -maxdepth 0 -name Makefile -exec dirname "{}" \;)

.PHONY: build
ifeq ($(BUILD_TARGET),local)
build:
	DOCKER_BUILDKIT=1 \
	docker build \
	--rm=true \
	--platform=linux/amd64 \
	--build-arg DOCKER_REGISTRY=$(DOCKER_REGISTRY) \
	--build-arg BRANCH=$(BRANCH) \
	--build-arg BUILD_GIT_VERSION=$$(git describe --tags --always --dirty) \
	--build-arg BUILD_GIT_COMMIT=$$(git rev-parse --short HEAD) \
	--build-arg BUILD_DATE=$$(date -u +%Y-%m-%dT%H:%M:%SZ) \
	-t $(DOCKER_REGISTRY)/$(SERVICE):$(BRANCH) \
	-f Dockerfile .
else
build:
	BUILD_TARGET='$(BUILD_TARGET)' \
	NAMESPACE='build' \
	REPO_URL='$(REPO_URL)' \
	REF='$(REF)' \
	TAG='$(VERSION)' \
	DOCKERFILE='$(DOCKERFILE)' \
	IMAGE='$(DOCKER_REGISTRY)/$(SERVICE)' \
	PUSH='true' \
	PUSH_SECRET='$(PUSH_SECRET)' \
	hack/build-cluster.sh
endif

.PHONY: upload
upload:
ifeq ($(BUILD_TARGET),local)
	docker push $(DOCKER_REGISTRY)/$(SERVICE):$(BRANCH)
endif

.PHONY: clean
clean:
ifeq ($(BUILD_TARGET),local)
	docker rmi $(DOCKER_REGISTRY)/$(SERVICE):$(BRANCH) || true
	docker builder prune --max-used-space 21474836480 -f || true
endif

.PHONY: apply
apply:
	@for i in $(DIRS); do \
		cd $$i; \
		echo "apply $${i}"; \
		make apply; \
		cd ..; \
	done

.PHONY: buca
buca: build upload clean apply
```

8. Do not modify the `hack/` files, `Makefile`, `Makefile.variables`, `example.env`, `Dockerfile`, or any Go code.

9. Before finishing, re-run every command in the `<verification>` block and confirm each output matches; walk each requirement above against the change.
</requirements>

<constraints>
- The local arm's command, recipe and tag must not change: `--rm=true`, `--platform=linux/amd64`, `-f Dockerfile .`, the `DOCKER_BUILDKIT=1` prefix, all five `--build-arg` lines, and `-t $(DOCKER_REGISTRY)/$(SERVICE):$(BRANCH)`. Do not convert it to `docker buildx`.
- The arm goes in `Makefile.docker`, not `Makefile`. The existing `Makefile` carries no `build` target at all.
- `BUILD_TARGET ?= local` must be assigned before the first `ifeq` in `Makefile.docker`.
- The `upload` and `clean` guards sit inside their recipes, leaving each target defined with an empty body for a cluster target. Do not wrap the whole rule in `ifeq`.
- The cluster dispatch uses the backslash-continued environment-prefix form, with `hack/build-cluster.sh` last on its own line, no `@` prefix.
- Do not add a top-level `$(error …)` to the arm.
- Do not introduce a `VERSION` default. `hack/build-cluster.sh` already dies on an empty `TAG`; any default would publish a tag the checked-out tree does not correspond to.
- Do not add build knobs beyond `BUILD_TARGET` and `VERSION`. The repository URL, the Dockerfile name and the push-secret name are make variables with fixed defaults, not configurable inputs.
- Do not carry `ALLOW_DEFAULT_VERSION` or `ALLOW_UNTAGGED_BUILD` across from the copy sources.
- Do not constrain `BUILD_TARGET` to a fixed list; `hack/cluster-alias.sh` already defines the accepted aliases and the runner rejects an unknown one.
- Do not modify the five `hack/` files.
- No new runtime dependency in the built image; the change is build tooling only.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- Repo-relative paths only.
</constraints>

<verification>
Run `make precommit` -- must pass.

Run `make test` -- must pass.

`bash -n hack/build-cluster.sh && bash -n hack/cluster-alias.sh && bash -n hack/build-cluster-bootstrap.sh` -- all three parse, exit 0.

`grep -c '^BUILD_TARGET' Makefile.docker` prints 1.

`grep -cE '^[[:space:]]*hack/build-cluster\.sh' Makefile.docker` prints 1.

The local arm is unchanged. Run:
`out="$(make -n build)"; for want in "--rm=true" "--platform=linux/amd64" "--build-arg DOCKER_REGISTRY=docker.io" "--build-arg BRANCH=dev" "--build-arg BUILD_GIT_VERSION=" "--build-arg BUILD_GIT_COMMIT=" "--build-arg BUILD_DATE=" "-t docker.io/bborbe/k8s-apply:dev" "-f Dockerfile ."; do printf '%s\n' "$out" | grep -qF -- "$want" || { echo "missing: $want"; exit 1; }; done && ! printf '%s\n' "$out" | grep -q 'hack/build-cluster.sh' && echo ok`
prints `ok`.

The cluster arm passes every value. Run:
`out="$(make -n build BUILD_TARGET=sun REF=0123456789abcdef0123456789abcdef01234567 VERSION=probe)"; for want in "BUILD_TARGET='sun'" "NAMESPACE='build'" "REPO_URL='https://github.com/bborbe/k8s-apply.git'" "REF='0123456789abcdef0123456789abcdef01234567'" "TAG='probe'" "IMAGE='docker.io/bborbe/k8s-apply'" "PUSH='true'" "PUSH_SECRET='buildkit-push'" "hack/build-cluster.sh"; do printf '%s\n' "$out" | grep -qF -- "$want" || { echo "missing: $want"; exit 1; }; done && echo ok`
prints `ok`.

The cluster arm refuses a missing `VERSION`. This is a refusal probe, not a build: the runner exits non-zero at its `require TAG` before any kubectl call, so it needs no cluster, no Docker socket and no `kubectl`. Run:
`if make build BUILD_TARGET=sun REF=0123456789abcdef0123456789abcdef01234567 >/dev/null 2>/tmp/build-err; then echo "expected non-zero exit"; exit 1; fi; grep -q 'TAG is required' /tmp/build-err && echo ok`
prints `ok`.

Upload and clean stay local and exit 0 for a cluster target. Run:
`! make -n upload BUILD_TARGET=sun VERSION=probe | grep -q 'docker push' && make -n upload BUILD_TARGET=sun VERSION=probe >/dev/null && echo ok`
prints `ok`.
`! make -n clean BUILD_TARGET=sun VERSION=probe | grep -qE 'docker rmi|docker builder prune' && make -n clean BUILD_TARGET=sun VERSION=probe >/dev/null && echo ok`
prints `ok`.
</verification>
