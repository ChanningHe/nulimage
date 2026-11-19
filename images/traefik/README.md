# traefik

[Traefik](https://traefik.io/traefik/) reverse proxy, published to `ghcr.io/channinghe/traefik`.

- Built from source: the release's Go module (checked against `sum.golang.org`) compiled with this repository's Go toolchain, dashboard included
- Distroless (`gcr.io/distroless/static-debian13:nonroot`), runs as uid/gid `65532:65532`
- `linux/amd64` + `linux/arm64`
- Same paths as the official `traefik` image, so existing compose files keep working (see [Migrating](#migrating-from-the-official-image))

## Tags

| Release | Debug (busybox shell) |
|---|---|
| `latest` `3.7.19` `3.7` `3` `sha-abc1234` | `latest-debug` `3.7.19-debug` … |

Version tags follow the Traefik release (`version` in `nimg.yaml`) and are re-pushed when the base image or Go toolchain changes; use `sha-*` or a digest for immutability.

## Usage

Docker provider, ACME and a healthcheck:

```yaml
services:
  traefik:
    image: ghcr.io/channinghe/traefik:latest
    command:
      - --providers.docker.exposedByDefault=false
      - --entrypoints.web.address=:80
      - --entrypoints.websecure.address=:443
      - --certificatesresolvers.le.acme.email=you@example.com
      - --certificatesresolvers.le.acme.storage=/letsencrypt/acme.json
      - --certificatesresolvers.le.acme.tlschallenge=true
      - --ping
    ports: ["80:80", "443:443"]
    group_add: ["${DOCKER_GID}"]   # stat -c %g /var/run/docker.sock
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - letsencrypt:/letsencrypt
    healthcheck:
      test: ["CMD", "traefik", "healthcheck", "--ping"]
volumes:
  letsencrypt:
```

- **Docker socket**: the process is not root, so it needs the socket's group (`group_add`). Socket access is effectively root on the host; a socket proxy is the safer choice.
- **ACME**: `/letsencrypt` belongs to `65532:65532`, so a named volume mounted there is writable. Traefik creates `acme.json` with mode `0600` itself.
- **File provider / static config**: mount files under `/etc/traefik/` (Traefik reads `/etc/traefik/traefik.yml` by default).
- **Healthcheck**: `traefik healthcheck` reads the static configuration on its own. With a config file it finds `/etc/traefik/traefik.yml`; with flags, pass `--ping` (and the ping entry point, if you moved it) to the healthcheck too. There is no shell or `wget` in the release image.
- **Dashboard**: enable it with `--api.dashboard=true` and a router, or `--api.insecure=true` on `:8080` for local use.

### Plugins

- Plugins from the catalog (`experimental.plugins`) are downloaded to `/plugins-storage`, owned by `65532:65532`. With `read_only: true`, mount a volume or tmpfs there.
- Local plugins (`experimental.localPlugins`) are read from `/plugins-local/src/<moduleName>/`; mount them read-only.

### Read-only root filesystem

`read_only: true` works. Give Traefik writable `/plugins-storage` (only if you use catalog plugins) and `/letsencrypt` (only if you use ACME). `/tmp` is a `VOLUME`, the same as in the official scratch image.

## Migrating from the official image

Paths are the same: binary at `/usr/local/bin/traefik` (also `/traefik`), working directory `/`, config in `/etc/traefik/`, plugins in `/plugins-storage` and `/plugins-local`. What differs:

- **Not root.** Bind-mounted host directories (for example `./letsencrypt`) must be writable by uid `65532`: `sudo chown -R 65532:65532 ./letsencrypt`. An existing `acme.json` must stay `0600` and be owned by `65532`.
- **`command:` holds only flags.** The entrypoint is the binary, as in the official scratch image. If you used the alpine image with `command: ["traefik", "--providers.docker"]`, drop the leading `traefik`.
- **No shell** in the release image, so `CMD-SHELL` healthchecks and `wget` checks do not work; use `traefik healthcheck --ping` (above) or a `-debug` tag.
- **Ports 80/443** bind without extra capabilities under Docker's default bridge network: Docker sets `net.ipv4.ip_unprivileged_port_start=0` in the container's network namespace. With `network_mode: host`, or on Kubernetes, either listen on ports ≥ 1024 or set that sysctl (Kubernetes: `securityContext.sysctls` on the pod).
- `traefik version` shows the upstream source defaults for codename (`cheddar`) and build date; the version number is correct.

## License

MIT for the files in this repository (see the root `LICENSE`). The image contains Traefik (MIT). The license and notice files of every Go module linked into the binary, and Go's own `LICENSE`, are in `/usr/share/licenses/traefik/<module>/` inside the image. They are collected from the binary's build info, and the build fails if a module has none.

The embedded dashboard is a JavaScript bundle built from the release's `webui/` (React, Chart.js and other npm packages, mostly MIT). Their license texts are not collected into the image; the full list is in `webui/package.json` and `webui/yarn.lock` of the Traefik release.
