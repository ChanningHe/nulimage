#!/usr/bin/env bash
# Functional test of the caddy image (one variant): required plugin modules,
# version, plugin Caddyfile validates, default config serves. Runs under the
# harness's read-only profile, so /data and /config are tmpfs owned by the
# image's user.
set -euo pipefail

# shellcheck source-path=SCRIPTDIR source=../../lib/harness.sh
source "${NIMG_ROOT:?}/lib/harness.sh"

readonly CONTAINER_PORT=8080
readonly NONROOT_UID=65532
readonly TEST_DIR="${NIMG_ROOT}/images/caddy/test"
# Cloudflare provider rejects malformed tokens at provision time; 40 chars passes the shape check.
DUMMY_CF_TOKEN=$(printf 'x%.0s' {1..40})
readonly DUMMY_CF_TOKEN
variant=${NIMG_VARIANT:?}
readonly variant

[[ -f "${TEST_DIR}/${variant}.modules" ]] || nimg_fail "unknown variant '${variant}': no test/${variant}.modules"

state_mounts=()
for dir in /data /config; do
  state_mounts+=(--tmpfs "${dir}:uid=${NONROOT_UID},gid=${NONROOT_UID},mode=0700")
done
readonly state_mounts

echo "--- required modules"
actual=$(nimg_once -- list-modules --skip-standard | sed 's/^[[:space:]]*//' | sort -u)
missing=$(comm -23 <(sort -u "${TEST_DIR}/${variant}.modules") <(echo "$actual"))
[[ -z "$missing" ]] || nimg_fail "missing modules:"$'\n'"$missing"

echo "--- version"
got=$(nimg_once -- version | awk '{ print $1 }')
got=${got#v}
[[ "$got" == "$NIMG_VERSION" ]] || nimg_fail "caddy version printed '${got}', want '${NIMG_VERSION}'"

echo "--- validate plugin Caddyfile"
nimg_once "${state_mounts[@]}" -e "CF_API_TOKEN=${DUMMY_CF_TOKEN}" \
  -v "${TEST_DIR}/${variant}.Caddyfile:/etc/caddy/Caddyfile:ro" \
  -- validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1 ||
  nimg_fail "caddy validate rejected test/${variant}.Caddyfile"

echo "--- smoke test default config"
cid=$(nimg_run "${state_mounts[@]}" -p "127.0.0.1::${CONTAINER_PORT}" --)
port=$(nimg_port "$cid" "${CONTAINER_PORT}/tcp")
base="http://127.0.0.1:${port}"
nimg_wait_http "${base}/"
nimg_expect_http "${base}/" 200 "Caddy is running"

echo "caddy ${variant} ok"
