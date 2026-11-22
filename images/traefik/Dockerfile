# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

# VERSION (upstream Traefik release) and FLAVOR are supplied by `nimgctl bake`.
# FLAVOR selects the runtime base (release = distroless, debug = distroless + busybox).
ARG VERSION
ARG FLAVOR=release

# Source: the Go module zip of the release, verified against sum.golang.org. It contains
# webui/ (package.json, yarn.lock, .yarnrc.yml) as well as the Go code.
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS src
SHELL ["/bin/ash", "-eo", "pipefail", "-c"]
ARG VERSION
ENV GOTOOLCHAIN=local
# The nested module pkg/config/dynamic/ext (a local replace in go.mod) is excluded from the module
# zip and absent from the proxy, so its two files are fetched from the release commit (Origin).
RUN --mount=type=cache,target=/go/pkg/mod \
    info="$(go mod download -json "github.com/traefik/traefik/v3@v${VERSION}")" \
 && field() { printf '%s\n' "$info" | grep "\"$1\":" | cut -d'"' -f4; } \
 && dir="$(field Dir)" hash="$(field Hash)" ref="$(field Ref)" \
 && [ -n "$dir" ] \
 && { printf '%s\n' "$hash" | grep -Eqx '[0-9a-f]{40}' || { echo "bad Origin.Hash '$hash'" >&2; exit 1; }; } \
 && { [ "$ref" = "refs/tags/v${VERSION}" ] || { echo "bad Origin.Ref '$ref'" >&2; exit 1; }; } \
 && cp -R "$dir" /src \
 && chmod -R u+w /src \
 && mkdir -p /src/pkg/config/dynamic/ext \
 && for f in go.mod ext.go; do \
      wget -q -O "/src/pkg/config/dynamic/ext/$f" \
        "https://raw.githubusercontent.com/traefik/traefik/${hash}/pkg/config/dynamic/ext/$f" || exit 1; \
    done

# Dashboard assets, built the way upstream does (webui/buildx.Dockerfile, Makefile generate-webui).
# Platform independent, so it runs once on the build platform. .yarnrc.yml keeps enableScripts: false.
FROM --platform=$BUILDPLATFORM node:26-alpine@sha256:6bd9ab43c0ec0190ca7b2d193906cb8b7edf6e7f4afa43ddeccd6452710bffb5 AS webui
ENV VITE_APP_BASE_URL="" \
    VITE_APP_BASE_API_URL="/api" \
    COREPACK_ENABLE_DOWNLOAD_PROMPT=0
WORKDIR /src/webui
COPY --from=src /src/webui/package.json /src/webui/yarn.lock /src/webui/.yarnrc.yml ./
RUN npm i -g corepack@0.36.0 --ignore-scripts
RUN yarn workspaces focus --all --production
COPY --from=src /src/webui/ ./
RUN yarn build:prod

FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
SHELL ["/bin/ash", "-eo", "pipefail", "-c"]
ARG VERSION
ARG TARGETOS
ARG TARGETARCH
# GOTOOLCHAIN=local: fail loudly if upstream needs a newer Go than this image.
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY --from=src /src /src
COPY --from=webui /src/webui/static /src/webui/static
WORKDIR /src
# Version as upstream's release builds set it (no "v"). Codename and BuildDate keep their
# source defaults so the build stays reproducible.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
      go build -trimpath \
        -ldflags="-s -w -X github.com/traefik/traefik/v3/pkg/version.Version=${VERSION}" \
        -o /out/bin/traefik ./cmd/traefik \
 && mkdir -p /out/link \
 && ln -s /usr/local/bin/traefik /out/link/traefik \
 && install -d -m 0755 /out/plugins-storage /out/letsencrypt

# Ship the license/notice files of every module linked into the binary (Apache-2.0/BSD/MIT/MPL terms).
# The module list comes from the binary's own buildinfo, so it always matches what was compiled; the
# build fails if a module has no license file. Replacements by a local path (=> ./...) point into
# Traefik's own source tree and are covered by its LICENSE.md.
RUN --mount=type=cache,target=/go/pkg/mod \
    install -D -m 0644 "$(go env GOROOT)/LICENSE" /out/licenses/go/LICENSE \
 && install -D -m 0644 /src/LICENSE.md /out/licenses/github.com/traefik/traefik/v3/LICENSE.md \
 && go version -m /out/bin/traefik \
  | awk 'function flush() { if (m != "") print m; m = "" } $1 == "dep" { flush(); m = $2 "@" $3 } $1 == "=>" { m = ($2 ~ /^[.\/]/) ? "" : $2 "@" $3 } END { flush() }' \
  | while read -r mod; do \
      dir="$(go mod download -json "$mod" | grep '"Dir"' | cut -d'"' -f4)"; \
      [ -n "$dir" ] || { echo "cannot locate module $mod" >&2; exit 1; }; \
      dest="/out/licenses/${mod%@*}"; \
      mkdir -p "$dest"; \
      find "$dir" -maxdepth 1 -type f \
        \( -iname 'LICEN[CS]E*' -o -iname 'COPYING*' -o -iname 'NOTICE*' -o -iname 'PATENTS*' \) \
        -exec cp {} "$dest/" \; ; \
      [ -n "$(ls -A "$dest")" ] || { echo "no license file found for $mod" >&2; exit 1; }; \
    done

# CI-only target (nimgctl scan): reachability-based vulnerability scan of the shipped command.
FROM build AS vulncheck
COPY vulncheck/go.mod vulncheck/go.sum /vulncheck/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go -C /vulncheck tool govulncheck -C /src ./cmd/traefik

FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5 AS base-release
FROM gcr.io/distroless/static-debian13:debug-nonroot@sha256:3e7f48d714a3298b4df1a69fbc8809dd6fadd16c306838862fffc2895b168158 AS base-debug

# Final stage: no RUN. Paths match the official images: binary at /usr/local/bin/traefik (alpine)
# and /traefik (scratch, a symlink here), cwd /, so plugins land in /plugins-storage and local
# plugins are read from /plugins-local.
FROM base-${FLAVOR}
COPY --from=build /out/bin/traefik /usr/local/bin/traefik
COPY --from=build /out/link/ /
COPY --from=build /out/licenses /usr/share/licenses/traefik
COPY --from=build --chown=65532:65532 /out/plugins-storage /plugins-storage
COPY --from=build --chown=65532:65532 /out/letsencrypt /letsencrypt
USER 65532:65532
WORKDIR /
EXPOSE 80
VOLUME ["/tmp"]
ENTRYPOINT ["/usr/local/bin/traefik"]
