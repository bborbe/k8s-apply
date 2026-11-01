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
ARG KUBECTL_VERSION=v1.36.4

RUN apk --no-cache add \
        bash \
        ca-certificates \
        git \
        make \
        openssh-client \
        tzdata \
        wget \
 && wget -q -O /usr/local/bin/kubectl \
        "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" \
 && chmod 0755 /usr/local/bin/kubectl \
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

COPY --from=build /main /main
COPY --from=build /usr/local/bin/teamvault-cli /usr/local/bin/teamvault-cli

# No teamvault config file is written into the image on purpose. teamvault-cli
# reads $TEAMVAULT_CONFIG (else ~/.config/teamvault-cli/config.json) and, when
# that file exists, its url/user/pass OVERWRITE the env vars — so shipping one
# would silently win over the per-stage credentials injected at runtime.

ENV BUILD_GIT_VERSION=${BUILD_GIT_VERSION}
ENV BUILD_GIT_COMMIT=${BUILD_GIT_COMMIT}
ENV BUILD_DATE=${BUILD_DATE}
ENTRYPOINT ["/main"]
