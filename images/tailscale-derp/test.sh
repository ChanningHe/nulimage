#!/usr/bin/env bash
# Functional test of the derper image: reports the right version, serves plain
# HTTP, answers DERP upgrades. Runs under the harness's read-only profile, so
# derper gets a tmpfs key dir owned by the image's user.
set -euo pipefail

# shellcheck source-path=SCRIPTDIR source=../../lib/harness.sh
source "${NIMG_ROOT:?}/lib/harness.sh"

readonly CONTAINER_PORT=31478
readonly NONROOT_UID=65532
readonly KEY_DIR=/var/lib/derper

got=$(nimg_once -- -version)
[[ "$got" == "$NIMG_VERSION" ]] || nimg_fail "-version printed '$got', want '$NIMG_VERSION'"

cid=$(nimg_run \
  --tmpfs "${KEY_DIR}:uid=${NONROOT_UID},gid=${NONROOT_UID},mode=0700" \
  -p "127.0.0.1::${CONTAINER_PORT}" --)
port=$(nimg_port "$cid" "${CONTAINER_PORT}/tcp")
base="http://127.0.0.1:${port}"

nimg_wait_http "${base}/derp/probe"
nimg_expect_http "${base}/derp/probe" 200
nimg_expect_http "${base}/generate_204" 204

# A DERP upgrade over plain HTTP (what the reverse proxy forwards) must get 101.
# The connection stays open after the upgrade, so curl is expected to time out.
code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 2 \
  -H 'Upgrade: DERP' -H 'Connection: Upgrade' "${base}/derp" 2>/dev/null || true)
[[ "$code" == "101" ]] || nimg_fail "/derp upgrade returned '${code}', want 101"

nimg_logs_contain "$cid" "serving on :${CONTAINER_PORT}" ||
  nimg_fail "not serving plain HTTP on :${CONTAINER_PORT}"

echo "tailscale-derp ok"
