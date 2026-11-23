# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

# VERSION (upstream Tailscale release) and FLAVOR are supplied by `nimgctl bake`.
# FLAVOR selects the runtime base (release = distroless, debug = distroless + busybox).
ARG VERSION
ARG FLAVOR=release

# Build on the native platform and cross-compile: no QEMU, no RUN in the final stage.
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build

SHELL ["/bin/ash", "-eo", "pipefail", "-c"]

ARG VERSION
ARG TARGETOS
ARG TARGETARCH

# GOTOOLCHAIN=local: fail loudly if upstream needs a newer Go than this image.
ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=local \
    GOFLAGS=-trimpath

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
      go install \
        -ldflags="-s -w -X tailscale.com/version.longStamp=${VERSION} -X tailscale.com/version.shortStamp=${VERSION}" \
        "tailscale.com/cmd/derper@v${VERSION}" \
 # Cross-compiled binaries land in $GOPATH/bin/<os>_<arch>/, native ones in $GOPATH/bin/.
 && bin="/go/bin/${TARGETOS}_${TARGETARCH}/derper" \
 && { [ -f "$bin" ] || bin=/go/bin/derper; } \
 && install -D -m 0755 "$bin" /out/derper \
 && install -d -m 0700 /out/state

# Ship the license/notice files of every module linked into the binary (BSD/Apache redistribution terms).
# The module list comes from the binary's own buildinfo, so it always matches what was compiled.
RUN --mount=type=cache,target=/go/pkg/mod \
    install -D -m 0644 "$(go env GOROOT)/LICENSE" /out/licenses/go/LICENSE \
 && go version -m /out/derper | awk '$1 == "mod" || $1 == "dep" { print $2 "@" $3 }' \
  | while read -r mod; do \
      dir="$(go mod download -json "$mod" | grep '"Dir"' | cut -d'"' -f4)"; \
      dest="/out/licenses/${mod%@*}"; \
      mkdir -p "$dest"; \
      find "$dir" -maxdepth 1 -type f \
        \( -iname 'LICEN[CS]E*' -o -iname 'COPYING*' -o -iname 'NOTICE*' -o -iname 'PATENTS*' \) \
        -exec cp {} "$dest/" \; ; \
      [ -n "$(ls -A "$dest")" ] || { echo "no license file found for $mod" >&2; exit 1; }; \
    done

FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5 AS base-release
FROM gcr.io/distroless/static-debian13:debug-nonroot@sha256:3e7f48d714a3298b4df1a69fbc8809dd6fadd16c306838862fffc2895b168158 AS base-debug

# Final stage: no RUN.
FROM base-${FLAVOR}

COPY --from=build /out/derper /usr/local/bin/derper
COPY --from=build /out/licenses /usr/share/licenses/derper
COPY --from=build --chown=65532:65532 --chmod=0700 /out/state /var/lib/derper

USER 65532:65532
EXPOSE 31478/tcp 3478/udp

# Plain HTTP on :31478 (TLS is terminated by the reverse proxy).
# Overriding CMD replaces ALL flags: keep -a and -c when you do.
ENTRYPOINT ["/usr/local/bin/derper"]
CMD ["-a", ":31478", "-stun-port", "3478", "-c", "/var/lib/derper/derper.key"]
