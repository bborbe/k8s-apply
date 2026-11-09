---
status: draft
---

## Summary

- This repository's image can currently be produced on one machine only: the Mac.
- This work gives it a second build path. Naming a cluster instead of the local Docker daemon runs the same build as a Job inside that cluster and pushes the result to Docker Hub.
- The local path is the default and keeps its command and its tag exactly as they are today.
- Both paths compose their image reference from the same two variables, so the two cannot drift apart.
- The arm lives in the build file this repository already has, the one the parent goal's arm check greps.

## Problem

`k8s-apply` runs on nukedev, nukeprod and sun, and its deployment pulls `bborbe/k8s-apply` through nuke's hand-rolled `mirror:` target, which copies `docker.io/bborbe/k8s-apply:$(VERSION)` into the per-env registry. Both ends of that chain start with a build on this Mac: `make build` runs `docker build` against the local daemon, and nothing else can produce the image. Every rebuild, every release cut while the operator is away from this machine, and every unattended pipeline needs a human at the keyboard for this one repo.

Three properties of this repo shape the change, and only one of them is the usual trap:

- **The build file is `Makefile.docker`, included before `SERVICE` is assigned.** `Makefile:3` includes it, `Makefile:4` includes `example.env`, and `SERVICE = bborbe/k8s-apply` is assigned at `Makefile:6`. Recipes expand lazily, so a recipe reading `$(SERVICE)` is correct; a parse-time `$(shell …)` in the arm is not, because `SERVICE`, `IMAGE` and `DOCKER_REGISTRY` are all still empty when `Makefile.docker` is read.
- **`VERSION` does not exist here.** `Makefile.variables` defines only `BRANCH`, `HOSTNAME`, `ROOTDIR` and `TEAMVAULT`. There is no `VERSION` default to refuse and no `check-version-tag` guard to scope, so the traps the parent goal records for other repos do not apply. `hack/build-cluster.sh`'s own `require TAG` is the refusal.
- **`BRANCH` is pinned to `dev` by `example.env`.** So the local arm always tags `docker.io/bborbe/k8s-apply:dev`, regardless of the checked-out branch, and that is the reference the local-arm criterion must inspect.

## Goal

`make build` on this repo produces the image locally exactly as it does today, tagged `docker.io/bborbe/k8s-apply:dev`. `make build BUILD_TARGET=sun REF=<full sha> VERSION=<scratch-tag>` produces the same image inside the sun cluster as a Kubernetes Job and pushes `docker.io/bborbe/k8s-apply:<scratch-tag>`, the Docker Hub repository nuke's `mirror:` target copies from. Both arms work from the same checkout, the local arm is unchanged in behaviour, and the cluster arm needs no local *build* preparation — only a pushed `REF` and a configured `kubectl` for the target cluster.

## Non-goals

- Changing the local arm's command or its tag. Both stay as they are.
- Moving the local arm to `docker buildx` with timestamp rewriting. This repo's local arm keeps `docker build`; the cluster arm derives its build clock from the built commit.
- Pushing the private-registry copy from the cluster. The `buildkit-push` credential is a Docker Hub credential; the private copy stays nuke's `mirror:` target.
- Bumping `nuke`'s `VERSION := v0.19.0` pin to the tag this work releases, or running `make mirror`. Both are deploy-side edits in another repository.
- Adding a `check-version-tag` guard. This repo has none and this spec does not introduce one.
- Adding build knobs beyond `BUILD_TARGET` and `VERSION`. The repository URL and the push-secret name are make variables with fixed defaults, not configurable inputs.
- Carrying `ALLOW_DEFAULT_VERSION` or `ALLOW_UNTAGGED_BUILD` across from the copy sources. Neither has anything to opt out of here.
- Constraining `BUILD_TARGET` to a fixed list. `hack/cluster-alias.sh` already defines the accepted aliases and the runner rejects an unknown one, so the Makefile adds no validation of its own.
- Changing what `buca` composes. It stays `build upload clean apply`; under a cluster target `build` builds in the cluster, `upload` and `clean` become no-ops, and `apply` is unaffected — this repo's `apply` recurses into sub-Makefiles that do not exist, so it is a no-op either way.
- A committed end-to-end scenario. The cluster path needs cluster access and registry credentials, which CI does not have; the operator-executable rung below is the smoke check instead.
- Modifying the five `hack/` files. They are committed to this branch, copied verbatim from their named sources; see Constraints.

## Acceptance Criteria

- [ ] **The local arm's command and tag are unchanged** — with no `BUILD_TARGET` set, `make -n build` prints the existing `docker build` recipe — `--rm=true`, `--platform=linux/amd64`, all five `--build-arg` lines (`DOCKER_REGISTRY`, `BRANCH`, `BUILD_GIT_VERSION`, `BUILD_GIT_COMMIT`, `BUILD_DATE`), `-t docker.io/bborbe/k8s-apply:dev` and `-f Dockerfile .` — and contains no reference to `hack/build-cluster.sh`. Evidence: the full `make -n build` output. ⚠️ `-t docker.io/bborbe/k8s-apply:dev` is asserted with the literal `dev`, not `$(BRANCH)` — make syntax is not shell syntax and would substitute empty; and `dev` comes from `example.env`, not from `git`.
- [ ] **The cluster arm is present in `Makefile.docker`, anchored on the assignment line** — `grep -c '^BUILD_TARGET' Makefile.docker` returns ≥ 1 **and** `grep -cE '^[[:space:]]*hack/build-cluster\.sh' Makefile.docker` returns ≥ 1 on an invocation line rather than a comment. Evidence: both counts. ⚠️ The `^` anchor is load-bearing and it dictates the recipe's shape: `hack/build-cluster.sh` must start a line, so the dispatch uses the backslash-continued environment-prefix form (one `VAR='$(VAR)' \` per line, with the script last). A single-line `VAR=… hack/build-cluster.sh` prefix fails the anchor. ⚠️ The anchor also excludes a `#` comment naming the script — the risk here is a comment the implementer adds to `Makefile.docker`, not the copied `hack/` headers, which this grep never reads.
- [ ] **The cluster arm composes the reference the deployment consumes, and passes every value the runner requires** — `make -n build BUILD_TARGET=sun REF=<full 40-character sha> VERSION=probe` prints all of `BUILD_TARGET='sun'`, `NAMESPACE='build'`, `REPO_URL='https://github.com/bborbe/k8s-apply.git'`, `REF='<sha>'`, `TAG='probe'`, `IMAGE='docker.io/bborbe/k8s-apply'`, `PUSH='true'`, `PUSH_SECRET='buildkit-push'` and `hack/build-cluster.sh`. Evidence: those nine lines from the printed recipe. `REPO_URL` is what proves `$(SERVICE)` resolved — it is assigned *after* the include, so an arm that read it at parse time would print `https://github.com/.git`. The other five are what the runner `require`s: an arm that omits `PUSH_SECRET` or `NAMESPACE` passes a narrower check and fails only at the expensive cluster rung, so they are asserted here instead.
- [ ] **The cluster arm refuses a missing `VERSION`** — `make build BUILD_TARGET=sun REF=<full 40-character sha>` with no `VERSION` exits non-zero and its stderr contains `TAG is required`. Evidence: the exit code and the message. The refusal is in the runner's `require TAG`, which runs before any `kubectl` call, so this probe needs no cluster and creates no Job.
- [ ] **`hack/` holds the five files and the arm's shell parses** — at `HEAD`, `git ls-tree --name-only HEAD hack/` lists `build-cluster.sh`, `cluster-alias.sh`, `build-job.yaml`, `build-cluster-bootstrap.sh` and `dockerconfig.json.tpl`, and `bash -n` exits 0 for each of the three shell scripts. Evidence: the five names and the three exit codes.
- [ ] **`upload` and `clean` stay local, and both targets still exist** — `make -n upload` prints `docker push` and `make -n clean` prints `docker rmi`; `make -n upload BUILD_TARGET=sun VERSION=probe` contains no `docker push` and **exits 0**, and `make -n clean BUILD_TARGET=sun VERSION=probe` contains no `docker rmi`, no `docker builder prune`, and **exits 0**. Evidence: the four outputs and the two exit codes. ⚠️ The exit codes are part of the criterion: wrapping the whole rule in `ifeq` deletes the target, so `make -n upload BUILD_TARGET=sun` would fail with `No rule to make target` and break `buca`'s prerequisite list — a refusal that reads like a pass.
- [ ] **Post-Deploy (Rung-2): one real cluster build is recorded** — `make build BUILD_TARGET=sun REF=<full 40-character sha> VERSION=<scratch-tag>` exits 0, `kubectlsun -n build get job build-<scratch-tag> -o jsonpath='{.status.succeeded}'` returns `1`, the build pod's `spec.nodeName` is recorded, `crane digest docker.io/bborbe/k8s-apply:<scratch-tag>` resolves to a digest, and `docker image inspect` on that same reference fails on this Mac with `No such image`. Evidence: one row carrying the scratch tag, exit code, Job name, node, digest and the inspect failure text. ⚠️ Exit 0 plus a local inspect failure is satisfiable by an arm that builds nothing; the Job's `succeeded=1` and the resolved digest are what distinguish a cluster push from a no-op. ⚠️ The Job name is the *sanitized* tag, not the raw one — `hack/build-cluster.sh` lowercases it, replaces everything outside `[a-z0-9.-]`, truncates to 63 characters and strips trailing `-`/`.`. Keep the scratch tag lowercase alphanumeric plus hyphens so the two agree.
  - `deploy_check:` `git show HEAD:Makefile.docker | grep -c '^BUILD_TARGET'`
  - `deploy_target:` `1`
- [ ] **The local arm still works** — `docker rmi` the local reference first, then `make build` exits 0 and `docker image inspect docker.io/bborbe/k8s-apply:dev` exits 0 with a non-empty `.Id`. Evidence: the `rmi` result, the exit code and the image id. ⚠️ This is the regression gate for the arm split; a cluster-only change that leaves `make build` refusing or tagging something else fails here.
- [ ] **The documented build path matches reality** — `README.md` § *Docker* shows the cluster invocation `make build BUILD_TARGET=sun REF=<sha> VERSION=<tag>` inside a code span, states that the local arm is the default, and records that the cluster arm pushes Docker Hub while nuke's `mirror:` target copies it onward; `CHANGELOG.md` carries a `## Unreleased` section with a bullet for this change. Evidence: `sed -n '/^### Docker/,/^## /p' README.md | grep -c 'BUILD_TARGET=sun REF=<sha> VERSION=<tag>'` = 1 and `grep -c '^## Unreleased$' CHANGELOG.md` = 1. ⚠️ The invocation must sit inside backticks or a fenced block: bare, `<sha>` renders as an HTML tag and disappears from the rendered page.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

- `make test` — the unit and integration suite exits 0
- `make precommit` — lint, format, generate, test and license clean
- `bash -n hack/build-cluster.sh && bash -n hack/cluster-alias.sh && bash -n hack/build-cluster-bootstrap.sh` — the arm's shell parses (the Go lint does not cover it)
- `git ls-tree --name-only HEAD hack/` — lists the five expected files
- `grep -c '^BUILD_TARGET' Makefile.docker` — returns ≥ 1
- `grep -cE '^[[:space:]]*hack/build-cluster\.sh' Makefile.docker` — returns ≥ 1
- `make -n build` — the unchanged local `docker build` recipe, no `hack/build-cluster.sh`
- `make -n build BUILD_TARGET=sun REF=<40-char-sha> VERSION=probe` — prints all nine values listed in AC 3
- `make build BUILD_TARGET=sun REF=<40-char-sha>` (no `VERSION`) — exits non-zero, stderr contains `TAG is required`
- `make -n upload BUILD_TARGET=sun VERSION=probe` — contains no `docker push`, exits 0
- `make -n clean BUILD_TARGET=sun VERSION=probe` — contains no `docker rmi`, no `docker builder prune`, exits 0
- `grep -c '^## Unreleased$' CHANGELOG.md` — returns `1`

### Operator-executable (runs on the host after PR merge, spec verification ladder)

- `docker rmi docker.io/bborbe/k8s-apply:dev` then `make build` then `docker image inspect docker.io/bborbe/k8s-apply:dev` — the local arm still works and produced this image
- `make build BUILD_TARGET=sun REF=<full sha> VERSION=<scratch-tag>` — one real cluster build, Job name and node recorded
- `kubectlsun -n build get job build-<scratch-tag> -o jsonpath='{.status.succeeded}'` — returns `1`
- `crane digest docker.io/bborbe/k8s-apply:<scratch-tag>` — the push landed at the repository nuke mirrors from
- `docker image inspect docker.io/bborbe/k8s-apply:<scratch-tag>` — fails with `No such image`, proving the build did not run here

## Desired Behavior

1. **A build target selects the arm.** `BUILD_TARGET` defaults to `local`. With `local`, `make build` behaves exactly as it does today: it runs the existing `docker build` recipe and tags `$(DOCKER_REGISTRY)/$(SERVICE):$(BRANCH)`, which with the repo's defaults is `docker.io/bborbe/k8s-apply:dev`.
2. **Any other value routes to a cluster Job.** With `BUILD_TARGET` set to a cluster alias or kubectl context, `make build` renders and applies the Job through `hack/build-cluster.sh`, passing the repository URL, the Dockerfile name and the push-secret name as make variables with fixed defaults. It waits for the Job and reports success or failure with the Job's own log lines.
3. **The cluster arm needs a caller-supplied tag.** `VERSION` has no default in this repo, so the cluster arm cannot invent one. A cluster build with no `VERSION` is refused by the runner's `require TAG` before any Job is created, naming `TAG` as the missing input.
4. **Both arms publish to one repository.** The cluster arm uses `$(DOCKER_REGISTRY)/$(SERVICE)` — the same expression the local arm tags, so the two cannot drift. With the repo's defaults that is `docker.io/bborbe/k8s-apply`. ⚠️ Do not compose it from `$(IMAGE)`: `example.env` exports `IMAGE=bborbe/k8s-apply` with no registry prefix, so `$(IMAGE)` alone is not the reference either arm uses.
5. **`upload` and `clean` remain local-only.** A remote target pushes from inside the cluster and never loads the image here, so there is nothing to push or remove. Both keep their target defined and become no-ops for a cluster target, while `build` with the same target still runs.
6. **The arm sits in `Makefile.docker` and adds no parse-time command substitution.** `Makefile.docker` is included at `Makefile:3`, before `SERVICE` (`:6`) and `example.env` (`:4`), so the arm may reference `$(SERVICE)`, `$(DOCKER_REGISTRY)` and `$(IMAGE)` only inside recipes — never in a `$(shell …)` or a `:=`. The `BUILD_*` defaults are literal strings and are safe wherever they are written. `ifeq` guards are parse-time by design and are the one parse-time construct the arm uses.
7. **The documented build path matches reality.** The README's Docker section shows both arms, and the CHANGELOG records the change.

## Constraints

- The local arm's command, recipe and tag must not change: `--rm=true`, `--platform=linux/amd64`, `-f Dockerfile .`, the `DOCKER_BUILDKIT=1` prefix, all five `--build-arg` lines, and `-t $(DOCKER_REGISTRY)/$(SERVICE):$(BRANCH)`. Do not convert it to `docker buildx`.
- The arm goes in `Makefile.docker`, not `Makefile`. The parent goal's arm check greps `Makefile` and `Makefile.docker`; the existing `Makefile` carries no `build` target at all.
- `BUILD_TARGET ?= local` must be assigned **before** the first `ifeq` in `Makefile.docker`. Every guard in the file keys on it, so an assignment placed below them makes the local arm vanish for a plain `make build`.
- The `upload` and `clean` guards sit **inside** their recipes, leaving each target defined with an empty body for a cluster target:
  ```make
  upload:
  ifeq ($(BUILD_TARGET),local)
  	docker push $(DOCKER_REGISTRY)/$(SERVICE):$(BRANCH)
  endif
  ```
  Do not wrap the whole rule in `ifeq`: that deletes the target, so `make -n upload BUILD_TARGET=sun` fails with `No rule to make target`, `buca`'s prerequisite list breaks, and the AC's "contains no `docker push`" reads as a pass on a command that errored.
- The cluster dispatch uses the backslash-continued environment-prefix form, with `hack/build-cluster.sh` last on its own line — the anchored `^[[:space:]]*hack/build-cluster\.sh` grep depends on it, and it is the shape both already-migrated sibling repos use.
- Do **not** add a top-level `$(error …)` to the arm. `Makefile.docker` is included unconditionally, so an ungated error fires for every goal — including `make test` and `make precommit` — and would break the build before the arm is reachable.
- Do **not** introduce a `VERSION` default. `hack/build-cluster.sh` already dies on an empty `TAG`; any default would publish a tag the checked-out tree does not correspond to, and it would also give the local arm a `VERSION` it has never had.
- The five `hack/` files are committed at `hack/` on this branch, copied verbatim from `origin/master` of their named sources: `build-cluster.sh`, `cluster-alias.sh` and `build-job.yaml` from `bborbe/agent-task-executor` (its Job template carries `automountServiceAccountToken: false`, which `go-skeleton`'s does not); `build-cluster-bootstrap.sh` and `dockerconfig.json.tpl` from `bborbe/go-skeleton`. **Do not modify them.** If a change seems needed, it belongs in the source repository and a later copy.
- `hack/build-job.yaml` must not move under `k8s/`, so a yaml-sweeping apply target cannot adopt it as deploy state. It is a template, not deploy state.
- `.dark-factory.yaml` needs no change: at `master` it already carries `workflow: worktree`, `pr: true`, `autoRelease: false`, `autoGeneratePrompts: true` and `validationPrompt: docs/dod.md`. ⚠️ `.maintainer.yaml` separately carries `release.autoRelease: true` — that is the release pipeline's setting, not this file's, and the two are not a conflict to reconcile mid-run.
- `.gitignore` needs no change: it ignores `/specs/log`, `/prompts/log` and `/vendor`, so the spec and the `hack/` files are committable as they are.
- `README.md` § *Docker* is the in-repo home for the two-arm topology; the spec dies after implementation, the README does not.
- No new runtime dependency in the built image; the change is build tooling only.

## Assumptions

These must hold before the cluster criteria can pass; none is created by this spec.

- The `build` namespace and the `buildkit-push` dockerconfigjson secret exist on the target cluster, and that credential authenticates against **Docker Hub**. Measured on the pilot 2026-11-08: a private-mirror push returned `401 Unauthorized` after a successful build. The private-registry copy the deployment pulls comes from nuke's `mirror:` target, not from this build.
- The repository is public, so the Job's anonymous `git fetch` succeeds; the init container carries no clone credential.
- `crane` is installed on the host running the operator-rung probes.
- `REF` exists in the local repository **and on `origin`**: the runner derives `SOURCE_DATE_EPOCH` from the local repository, and the Job's init container fetches the ref from the remote. A commit that exists only locally fails at the clone.
- The build pod can reach the Go module proxy.
- `kubectl` is configured for the target cluster through the alias `hack/cluster-alias.sh` resolves, and that cluster is reachable.
- The nuke `VERSION` pin stays at `v0.19.0`; deploying the newly built tag is a separate deploy-side edit plus a `make mirror`.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| `BUILD_TARGET` names an unknown cluster or context | The runner fails before applying anything and lists the accepted values | Re-run with a listed target; no Job exists to clean up |
| `BUILD_TARGET` names a real cluster that has no `build` namespace or no `buildkit-push` secret | Every validation passes and the Job is rejected at apply or fails at the push | Provision the namespace and secret on that cluster (`hack/build-cluster-bootstrap.sh`), or build on a cluster that has them |
| `VERSION` omitted on the cluster arm | Refusal in `require TAG`, naming `TAG`, before any Job is created | Re-run passing `VERSION=<scratch-tag>` |
| `REF` is a short SHA, or is not in the local repository | The runner dies resolving `SOURCE_DATE_EPOCH` | `git fetch`, then re-run with the full 40-character SHA |
| `REF` exists locally but was never pushed | The clone init container fails: the ref does not exist on `origin` | Push the branch or commit, then re-run |
| The clone's commit epoch differs from the locally derived one | The build container refuses before `buildctl` runs and names both epochs | `git fetch`, then re-run |
| A Job with the derived name already exists in `build` | The runner refuses and names the existing Job rather than overwriting it | `kubectlsun -n build delete job build-<scratch-tag>`, then re-run |
| The runner is interrupted after `apply` | The Job keeps running in the cluster while the local invocation exits; the next run then hits the existing-Job refusal | `kubectlsun -n build get jobs`, read the pod log, delete the Job once finished |
| The build pod cannot reach the module proxy | The Job fails; the runner prints the last 100 log lines and the Job name | `kubectlsun -n build logs job/build-<scratch-tag> --all-containers`, then re-run |
| The build pod is OOM-killed or throttled | The Job reports `failed`; the pod status carries `OOMKilled` or a CPU-throttling event | Raise the template's limits in the source repository, or build locally for that one image |
| The registry rejects the push (401) | The build fails at the push step with the registry's own error | `kubectlsun -n build get secret buildkit-push -o jsonpath='{.metadata.name}'`, confirm it is the Docker Hub credential, then re-run |
| Docker Hub throttles the push or nuke's `mirror:` pull (429) | The build or the mirror fails with the registry's status code; the tag did not land, so nothing is overwritten | Re-run after the window; both the push and the mirror hit Docker Hub, so a throttle can surface on either |
| A caller-supplied `VERSION` names an already-released tag | No guard — the push replaces that tag irreversibly | Re-push the image from the released tag, then cut a new release |
| The local arm regresses | `make build` fails or tags something other than `:dev` | Revert the arm; the local path is the regression gate |

## Security / Abuse

- The runner substitutes caller-supplied values into a manifest and into embedded shell, so every value is checked against a character whitelist and rejected for embedded newlines before rendering. Do not add a value path that bypasses that check.
- The repository URL and the push-secret name are make variables with fixed defaults, not configurable inputs. A configurable clone source on a build that mounts a push credential would let a caller point the credential-bearing Job at another repository.
- The build Job mounts a push credential from a `build`-namespace secret and runs with `automountServiceAccountToken: false`. The credential is scoped to pushing an image; it grants nothing else.
- `hack/build-cluster-bootstrap.sh` and `hack/dockerconfig.json.tpl` are one-time bootstrap helpers: they create the `build` namespace and the `buildkit-push` secret. They are not on the build path — the runtime credential arrives through the Job's `items:` remap. The template renders TeamVault placeholders; no credential value is ever committed, and the template is never applied during a build.
- `VERSION` is caller-controlled and becomes an image tag and a Job name. The tag is whitelisted and the Job name is sanitized to a valid DNS name, truncated, and stripped of trailing separators.

## Suggested Decomposition

Prompts should be generated in this order — each row is a single prompt with a clear scope.

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | `Makefile.docker`: the `BUILD_TARGET` split, the cluster branch with its full environment pass-through, and the in-recipe `upload` / `clean` scoping | 1, 2, 3, 4, 5, 6 | 1, 2, 3, 4, 6 | — |
| 2 | Docs: the README Docker section and the CHANGELOG `## Unreleased` bullet | 7 | 9 | — |

ACs **5**, **7** and **8** are not prompt rows by design: AC 5 asserts the already-committed `hack/` set, and ACs 7 and 8 are the operator rung, which no container can run.

Rationale: the five `hack/` files ship on this branch and are not a prompt — prompt 1 wires them into the build file, prompt 2 documents the result. Two prompts rather than three: the arm is one file and one contract, and splitting the dispatch from its own defaults would let the two drift.

**Size exception, recorded deliberately:** 7 Desired Behaviors × 9 Acceptance Criteria is 63, above the 50-cell guidance. Most of the ACs are negative probes on one contract (refusals, absences, exit codes) rather than independent surface, and the two-prompt decomposition keeps each prompt independently verifiable. Splitting would duplicate the Constraints section across two specs and let the halves drift — the arm is genuinely one contract.

## Do-Nothing Option

Leaving this repo without a cluster arm keeps one image tied to the Mac. Every rebuild, every release cut while the operator is away from this machine, and every future unattended pipeline needs a human at the keyboard for this one repo — while its siblings build themselves. The cost is not the one build; it is that the automation has a hole shaped exactly like the repo nobody is watching.
