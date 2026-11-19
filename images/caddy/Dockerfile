# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

# VARIANT selects the plugin set under variants/ (full | lite).
# FLAVOR selects the runtime base (release = distroless, debug = distroless + busybox).
# The Caddy version is the one in variants/<VARIANT>/go.mod; VERSION (nimg.yaml) only labels the image.
ARG VARIANT=full
ARG FLAVOR=release

FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
SHELL ["/bin/ash", "-eo", "pipefail", "-c"]
ARG VARIANT
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
WORKDIR /src
COPY variants/${VARIANT}/go.mod variants/${VARIANT}/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY variants/${VARIANT}/main.go ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags='-s -w' -o /out/usr/bin/caddy . \
 && mkdir -p /out/data /out/config

# Ship the license/notice files of every module linked into the binary (Apache-2.0/BSD/MIT redistribution terms).
# The module list comes from the binary's own buildinfo, so it always matches what was compiled; the build fails
# if a module has no license file, except the modules listed in `unlicensed`: upstream states its license only in
# the source headers (github.com/hslatman/ipstore: Apache-2.0, Copyright 2021 Herman Slatman). For those a NOTICE
# naming the license plus the Apache-2.0 text (Caddy's LICENSE) is shipped instead. A new entry needs a reviewed
# source header. The main module (this repo's variants/<name>/go.mod) is not third-party code.
RUN --mount=type=cache,target=/go/pkg/mod \
    unlicensed="github.com/hslatman/ipstore" \
 && install -D -m 0644 "$(go env GOROOT)/LICENSE" /out/licenses/go/LICENSE \
 && go version -m /out/usr/bin/caddy \
  | awk 'function flush() { if (m != "") print m; m = "" } $1 == "dep" { flush(); m = $2 "@" $3 } $1 == "=>" { m = $2 "@" $3 } END { flush() }' \
  | while read -r mod; do \
      dir="$(go mod download -json "$mod" | grep '"Dir"' | cut -d'"' -f4)"; \
      [ -n "$dir" ] || { echo "cannot locate module $mod" >&2; exit 1; }; \
      dest="/out/licenses/${mod%@*}"; \
      mkdir -p "$dest"; \
      find "$dir" -maxdepth 1 -type f \
        \( -iname 'LICEN[CS]E*' -o -iname 'COPYING*' -o -iname 'NOTICE*' -o -iname 'PATENTS*' \) \
        -exec cp {} "$dest/" \; ; \
      [ -z "$(ls -A "$dest")" ] || continue; \
      case " $unlicensed " in \
        *" ${mod%@*} "*) \
          echo "WARNING: $mod ships no license file; shipping a NOTICE with the Apache-2.0 text" >&2; \
          printf '%s\n' "$mod has no LICENSE file; its source files carry the Apache License 2.0 header" \
            "(Copyright 2021 Herman Slatman). The license text is in LICENSE next to this file." >"$dest/NOTICE"; \
          cp "$(go list -m -f '{{.Dir}}' github.com/caddyserver/caddy/v2)/LICENSE" "$dest/LICENSE" ;; \
        *) echo "no license file found for $mod" >&2; exit 1 ;; \
      esac; \
    done

# CI-only target (nimgctl scan): reachability-based vulnerability scan of the exact module graph.
FROM build AS vulncheck
COPY vulncheck/go.mod vulncheck/go.sum /vulncheck/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go -C /vulncheck tool govulncheck -C /src ./...

FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931 AS base-release
FROM gcr.io/distroless/static-debian13:debug-nonroot@sha256:7df9fac2f5d3927260696af33fc2391da26366825838a47337088ed78f854a0c AS base-debug

FROM base-${FLAVOR}
COPY --from=build /out/usr/bin/caddy /usr/bin/caddy
COPY --from=build /out/licenses /usr/share/licenses/caddy
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build --chown=65532:65532 /out/config /config
COPY Caddyfile /etc/caddy/Caddyfile
ENV XDG_CONFIG_HOME=/config \
    XDG_DATA_HOME=/data
USER 65532:65532
WORKDIR /srv
EXPOSE 8080 8443 8443/udp
VOLUME ["/data", "/config"]
ENTRYPOINT ["/usr/bin/caddy"]
CMD ["run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
