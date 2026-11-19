# nulimage

Distroless, rootless container images, rebuilt automatically when upstream releases a new version or a vulnerability fix lands.

## Images

| Image | What | Pull |
|---|---|---|
| [caddy](images/caddy) | Caddy web server with plugins (`full` and `lite`) | `ghcr.io/channinghe/caddy` |
| [tailscale-derp](images/tailscale-derp) | Tailscale DERP relay server (`derper`) | `ghcr.io/channinghe/tailscale-derp` |

## Tags

| Tag | Example | Points to |
|---|---|---|
| `latest` | `latest` | newest release |
| `X.Y.Z`, `X.Y`, `X` | `2.11.7`, `2.11`, `2` | that version line |
| `sha-<commit>` | `sha-1a2b3c4` | one build of this repository |
| `-debug` suffix | `latest-debug` | same image plus a busybox shell at `/busybox/sh` |

Images with variants add a suffix before `-debug`, for example `latest-lite` and `2.11.7-lite-debug`. Version tags are rebuilt when a dependency or the base image changes; pin a digest if you need a build that never changes.

## What every image guarantees

- Based on `gcr.io/distroless`: no shell and no package manager (except `-debug`).
- Runs as uid/gid `65532:65532`; tested with `--read-only` and `--cap-drop=ALL`.
- `linux/amd64` and `linux/arm64`.
- Tested and scanned (Grype, no fixable high or critical vulnerability) on both architectures before any tag moves.
- Published images are rescanned daily; findings are tracked as GitHub issues.
- SBOM and build provenance attached. Verify with:

  ```sh
  gh attestation verify oci://ghcr.io/channinghe/caddy:latest --owner channinghe
  ```

## Development

Needs Go and Docker with buildx.

```sh
go run ./tools/nimgctl build caddy   # build for the host architecture
go run ./tools/nimgctl test caddy    # image policy + images/caddy/test.sh
go run ./tools/nimgctl scan caddy    # vulnerability scan
```

To add an image, create `images/<name>/` with `nimg.yaml`, `Dockerfile` and `test.sh`; `images/tailscale-derp` is the smallest example.

## License

MIT for the files in this repository. Each image contains upstream software under its own license; see the image's README.
