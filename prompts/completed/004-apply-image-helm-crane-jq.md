---
status: completed
summary: Added helm and crane (digest-pinned COPY --from lines) and jq to the runtime stage of the apply image, with a pinning-rationale comment and a CHANGELOG bullet
execution_id: nuke-apply-exec-004-apply-image-helm-crane-jq
dark-factory-version: v0.198.17
created: "2026-11-04T08:01:35Z"
queued: "2026-11-04T08:01:35Z"
started: "2026-11-04T08:05:12Z"
completed: "2026-11-04T08:06:37Z"
---

# feat: ship helm, crane and jq in the apply image

<summary>
- The apply image gains helm, so a component's render and upgrade steps run in-cluster
- The apply image gains crane, so a component can copy an image with no Docker daemon
- The apply image gains jq, so a component's helm upgrade can build its arguments
- helm and crane are pinned by image digest, so a retagged upstream cannot change what builds
- jq comes from the alpine package set, like the rest of the apk list
- The digest pins are documented in the image the same way kubectl's pin already is
- Nothing in the service's Go code changes
- The image keeps its non-root user and its existing binaries
</summary>

<objective>
Give the in-cluster apply image the three command-line tools a component's apply prerequisites need — helm for rendering and upgrading, crane for registry copies with no daemon, and jq for the helm arguments — so those prerequisites run inside the pod instead of forcing a hand-run apply from a workstation.
</objective>

<context>
Read `docs/dod.md` (this project's definition of done) and `README.md` (service behaviour) for project conventions.

No spec governs this change; the requirements below are the sole definition of scope.

Read `Dockerfile` before writing — in particular the runtime stage, the `KUBECTL_VERSION` / `KUBECTL_SHA256` comment block that states why a version is pinned in the file rather than taken from a distro package, the `apk --no-cache add` list in the same `RUN`, and the existing `COPY --from=build` lines.

Both image references were verified on 2026-11-04. `docker manifest inspect` resolves `alpine/helm:3.22.1` and `gcr.io/go-containerregistry/crane:v0.22.1`; both are OCI indexes carrying a `linux/amd64` manifest, which `Makefile.docker` needs because it builds `--platform=linux/amd64`. `alpine/helm` installs the binary at `/usr/bin/helm` (its Dockerfile ends with `mv linux-${ARCH}/helm /usr/bin/helm`; `/bin/helm` does not exist) and that binary reports `v3.22.1`. The crane image's platform config carries `Entrypoint: ["/ko-app/crane"]` and its binary reports `v0.22.1`. A throwaway image built from `alpine:3.24` plus exactly the two `COPY --from` lines below built clean under BuildKit and both binaries executed inside it, world-executable, so uid 10001 can run them.

Nothing in the automated path builds this image — CI runs only `make precommit`, and the YOLO container has no docker socket — so `make build` on the operator side is the only check that the two `COPY --from` lines resolve. That build is the operator-executable rung of this change's verification; it is deliberately not in `<verification>`. Because no spec governs this change, that rung lives in the deploy of this component — `make buca` in the nuke repo runs the build and fails loudly if either reference does not resolve.

The consumer that motivates these three tools is the nuke `agent` component: its `apply` target is prerequisite-only and its prerequisites call `helm status` / `helm upgrade`, `jq` (its `upgrade` recipe refuses with `ERROR: 'jq' is not installed.` when jq is missing), and `docker pull` / `docker tag` / `docker push` in `mirror`. Replacing that `mirror` step with crane is a separate change in the nuke deploy repo and is out of scope here; this change only makes the tools present in the image.

`CHANGELOG.md` is the release-notes file; `prompts/completed/002-stage-branch-derivation.md` shows the house convention of adding an `## Unreleased` bullet. The sibling prompt `prerequisite-only-apply-target.md` also creates `## Unreleased`; dark-factory serializes executions per project, so the two run in sequence and neither bullet is lost.

`Makefile.docker` builds with `DOCKER_BUILDKIT=1`; that is what lets `COPY --from=<registry image>` pull its source.
</context>

<requirements>
1. Add helm to the runtime stage by copying it from `alpine/helm` at the exact digest below:
   `COPY --from=alpine/helm:3.22.1@sha256:8191701ad1f82ee3695c87211fcbfe6d7b1bb7df92ac1c8de9ccdb0995a6a2c1 /usr/bin/helm /usr/local/bin/helm`
   The binary is at `/usr/bin/helm` in that image. Pin by digest, not by a bare tag.

2. Add crane to the runtime stage from `gcr.io/go-containerregistry/crane` at the exact digest below:
   `COPY --from=gcr.io/go-containerregistry/crane:v0.22.1@sha256:1f968817b95790bed063f71175aa6b8ff879fa17064020415f3e18bb6e6a36e1 /ko-app/crane /usr/local/bin/crane`
   The binary is at `/ko-app/crane` — that image is ko-built and its entrypoint is `/ko-app/crane`. It is NOT at `/usr/bin/crane`. Pin by digest, not by a bare tag.

3. Add `jq` to the existing `apk --no-cache add` list in the runtime stage, alongside `bash`, `ca-certificates`, `git`, `make`, `openssh-client`, `tzdata` and `wget`. `wget` is deleted again at the end of the same `RUN` — leave that deletion alone.

4. Put the two `COPY --from` lines in the runtime stage next to the existing `COPY --from=build` lines, and add a comment above them in the same voice as the `KUBECTL_VERSION` block. The comment must:
   - name what each tool is for;
   - state that both third-party references are pinned by manifest digest, and why: a tag is mutable, so a retagged upstream would silently build different content, which is the same argument the kubectl checksum block already makes;
   - for crane, say what it is for: copying an image between registries from inside the pod, where there is no Docker daemon. Do not tie the comment to the pending nuke `mirror` rewrite — that comment goes stale the moment the rewrite lands.

5. Do not change the build stage, the non-root user, the `LABEL` block, or any existing `RUN` step other than the `apk add` list.

6. Add one bullet under `## Unreleased` in `CHANGELOG.md` (create the section at the top of the file if it is absent): `feat: ship helm, crane and jq in the apply image`.

7. Before finishing, re-run every command in the `<verification>` block and confirm each output matches; walk each requirement above against the change.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Both image references carry a `@sha256:` digest. A bare tag, a `latest` tag, or a partially-pinned tag such as `:3` or `:v0` is not acceptable.
- Do not restate either pinned reference inside the new comment.
- No change to Go code, tests, or manifests.
- The runtime image stays `alpine:3.24` and the container keeps running as uid 10001.
- Repo-relative paths only.
</constraints>

<verification>
Run `make precommit` -- must pass.

`grep -c 'alpine/helm:3.22.1@sha256:8191701ad1f82ee3695c87211fcbfe6d7b1bb7df92ac1c8de9ccdb0995a6a2c1' Dockerfile` prints 1.

`grep -c 'go-containerregistry/crane:v0.22.1@sha256:1f968817b95790bed063f71175aa6b8ff879fa17064020415f3e18bb6e6a36e1' Dockerfile` prints 1.

`sed -n '/apk --no-cache add/,/apk --no-cache del/p' Dockerfile | grep -cE '^[[:space:]]*jq[[:space:]]*\\?$'` prints 1 -- jq is an entry of the `apk --no-cache add` list, not merely mentioned in the new comment.

`grep -oE '(alpine/helm|crane):[^ @]+' Dockerfile | sort -u` prints exactly two lines -- `alpine/helm:3.22.1` and `crane:v0.22.1` -- so no other tag is present.

`grep -c 'feat: ship helm, crane and jq in the apply image' CHANGELOG.md` prints 1.
</verification>
