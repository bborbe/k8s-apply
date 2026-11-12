FROM golang:1.27.3 AS build
WORKDIR /workspace
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=bind,target=. \
    GOCACHE=/root/.cache/go-build \
    GOMODCACHE=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w" \
    -installsuffix cgo \
    -o /main
# teamvault-cli ships in the image because every component's apply target renders
# its manifests through `teamvault-cli config parse` before piping them to
# kubectl. PINNED, NOT `@latest`: the same commit must produce the same image on
# every day it is built. The module path carries the Seibert org and the /v5
# suffix; CGO_ENABLED=0 is load-bearing because the runtime stage has no libc.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOBIN=/usr/local/bin go install github.com/Seibert-Data/teamvault-cli/v5@v5.10.1
CMD ["/bin/bash"]

# The runtime stage is alpine, not scratch: the service shells out to make, which
# shells out to git, bash, kubectl and teamvault-cli. An image that could not run
# a component's own apply target would fail at the first deploy rather than at
# startup.
FROM alpine:3.24

# kubectl is pinned to the nuke cluster's server version (measured 2026-11-01:
# nuke-dev runs v1.36.4+k3s1) rather than taken from a distro package, so the
# client the apply target invokes cannot drift with an apk index refresh.
#
# The checksum is pinned HERE, not fetched alongside the binary: a value read
# from the same origin as the artifact it verifies proves only that the two
# agree, and a compromised mirror or DNS path can supply both. The digest below
# was recorded by hashing the v1.36.4 linux/amd64 binary and comparing it to the
# published .sha256 on 2026-11-01.
ARG KUBECTL_VERSION=v1.36.4
ARG KUBECTL_SHA256=8b8f088da2dab964f853b38464033b1be15ede2839eca751482357c45abdd05a

RUN apk --no-cache add \
        bash \
        ca-certificates \
        curl \
        git \
        jq \
        make \
        openssh-client \
        tzdata \
        wget \
 && wget -q -O /tmp/kubectl \
        "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" \
 && echo "${KUBECTL_SHA256}  /tmp/kubectl" | sha256sum -c - \
 && install -m 0755 /tmp/kubectl /usr/local/bin/kubectl \
 && rm -f /tmp/kubectl \
 && apk --no-cache del wget

ARG BUILD_GIT_VERSION=dev
ARG BUILD_GIT_COMMIT=none
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.title="Nuke Apply"
LABEL org.opencontainers.image.description="In-cluster CQRS apply service for the bborbe/nuke deploy repo"
LABEL org.opencontainers.image.vendor="Benjamin Borbe"
LABEL org.opencontainers.image.licenses="BSD-2-Clause"
LABEL org.opencontainers.image.source="https://github.com/bborbe/nuke-apply"
LABEL org.opencontainers.image.documentation="https://github.com/bborbe/nuke-apply"
LABEL org.opencontainers.image.version="${BUILD_GIT_VERSION}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"
LABEL org.opencontainers.image.revision="${BUILD_GIT_COMMIT}"

# helm and crane ship in the image because a component's apply prerequisites
# render and upgrade a release through helm, and crane copies an image between
# registries from inside the pod, where there is no Docker daemon. jq, added to
# the apk list above, builds the arguments for that helm upgrade. Both
# third-party references are pinned by manifest digest, not by a tag: a tag is
# mutable, so a retagged upstream would silently build different content — the
# same argument the kubectl checksum block above already makes.
COPY --from=alpine/helm:3.22.1@sha256:8191701ad1f82ee3695c87211fcbfe6d7b1bb7df92ac1c8de9ccdb0995a6a2c1 /usr/bin/helm /usr/local/bin/helm
COPY --from=gcr.io/go-containerregistry/crane:v0.22.1@sha256:1f968817b95790bed063f71175aa6b8ff879fa17064020415f3e18bb6e6a36e1 /ko-app/crane /usr/local/bin/crane
# yq comes from upstream rather than Alpine's package. The package is older
# (v4.53.3 at the time of writing) and rejects a valid expression the operator's
# toolchain accepts — `if (.sandbox.domains // {}) | length > 0 then … end` in
# nuke's agent/Makefile fails there with `lexer: invalid input text`, while
# v4.54.1 evaluates it. A version skew between the image and the machine that
# authors the Makefiles turns a working component into a broken one, so the
# image pins the same release the operator runs.
COPY --from=mikefarah/yq:4.54.1@sha256:4b3d9475d65571d28cbb19544d3820ec2945e4c8b2f18279394282b8dc3a592e /usr/bin/yq /usr/local/bin/yq

# helmfile ships in the image for the same reason helm does, and its absence was
# a measured outage rather than a theoretical gap: ten of nuke's components
# declare `apply: helmfile -l stage=$(STAGE) apply`, and each one fails at the
# first deploy with `/bin/sh: helmfile: not found` (2026-11-12,
# github-update-go-watcher v0.9.1). The failure reads as a component defect, so
# an image gap looked like ten broken components for as long as nobody deployed
# one — the `agent` component applies through plain helm and kept working.
#
# Pinned to the release the operator runs (v1.8.3, 2026-11-12) for the reason the
# yq note gives above: a version skew between the image and the machine that
# authors the Makefiles turns a working component into a broken one. The tarball
# is verified against a checksum recorded HERE, not one fetched alongside it —
# the same argument the kubectl block makes.
ARG HELMFILE_VERSION=1.8.3
ARG HELMFILE_SHA256=7d79c6d2099a4819213873164eda23bdbd32f1404fec7a7104d934e5d102596f
RUN curl -fsSL -o /tmp/helmfile.tgz \
        "https://github.com/helmfile/helmfile/releases/download/v${HELMFILE_VERSION}/helmfile_${HELMFILE_VERSION}_linux_amd64.tar.gz" \
 && echo "${HELMFILE_SHA256}  /tmp/helmfile.tgz" | sha256sum -c - \
 && tar -xzf /tmp/helmfile.tgz -C /usr/local/bin helmfile \
 && chmod 0755 /usr/local/bin/helmfile \
 && rm -f /tmp/helmfile.tgz

COPY --from=build /main /main
COPY --from=build /usr/local/bin/teamvault-cli /usr/local/bin/teamvault-cli

# No teamvault config file is written into the image on purpose. teamvault-cli
# reads $TEAMVAULT_CONFIG (else ~/.config/teamvault-cli/config.json) and, when
# that file exists, its url/user/pass OVERWRITE the env vars — so shipping one
# would silently win over the per-stage credentials injected at runtime.

# Non-root: the service runs component Makefiles that hold the pod's service
# account token and the TEAMVAULT_* credentials, so a recipe that misbehaves
# should not also be root. The uid is fixed and matched by the pod's
# securityContext, and fsGroup makes the emptyDir volumes writable for it.
RUN addgroup -g 10001 -S app \
 && adduser -u 10001 -S -G app -h /home/app app \
 && mkdir -p /data /repo \
 && chown 10001:10001 /data /repo

# `helmfile apply` runs a diff before it syncs, and that diff is the helm-diff
# plugin rather than part of the helmfile binary — helmfile alone still fails
# every apply. Installed into a fixed path exported as HELM_PLUGINS so it does
# not depend on HOME being set for the runtime user, and pinned to the operator's
# version for the same skew reason as yq and helmfile.
ARG HELM_DIFF_VERSION=3.15.10
ENV HELM_PLUGINS=/home/app/.local/share/helm/plugins
RUN helm plugin install https://github.com/databus23/helm-diff --version "v${HELM_DIFF_VERSION}" \
 && chown -R 10001:10001 /home/app/.local/share/helm
USER 10001:10001

ENV BUILD_GIT_VERSION=${BUILD_GIT_VERSION}
ENV BUILD_GIT_COMMIT=${BUILD_GIT_COMMIT}
ENV BUILD_DATE=${BUILD_DATE}
ENTRYPOINT ["/main"]
