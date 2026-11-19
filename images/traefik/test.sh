#!/usr/bin/env bash
# Functional test of the traefik image: version, healthcheck, embedded
# dashboard, file provider, docker provider and a local Yaegi plugin. Runs
# under the harness's read-only profile; the writable state directories are
# tmpfs owned by the image's user.
set -euo pipefail

# shellcheck source-path=SCRIPTDIR source=../../lib/harness.sh
source "${NIMG_ROOT:?}/lib/harness.sh"

readonly WEB_PORT=80
readonly API_PORT=8080
readonly NONROOT_UID=65532
readonly TEST_DIR="${NIMG_ROOT}/images/traefik/test"
readonly DOCKER_SOCK=/var/run/docker.sock
readonly PLUGIN_HEADER=X-Nimg-Plugin
readonly PLUGIN_VALUE=nimg-plugin-ok
readonly WAIT_S=30

[[ -S "$DOCKER_SOCK" ]] || nimg_fail "docker provider test needs ${DOCKER_SOCK}"

# get <host> <url>: print "<status> <header value>" and the body on stdout.
get() {
  local headers body code
  headers=$(mktemp)
  body=$(mktemp)
  code=$(curl -s -o "$body" -D "$headers" -w '%{http_code}' --max-time 5 -H "Host: $1" "$2") || code=000
  printf '%s %s\n' "$code" "$(awk -v h="${PLUGIN_HEADER,,}:" 'tolower($1) == h { print $2 }' "$headers" | tr -d '\r')"
  cat "$body"
  rm -f "$headers" "$body"
}

# wait_route <host> <url>: wait until the route answers 200 (providers load asynchronously).
wait_route() {
  local i
  for ((i = 0; i < WAIT_S; i++)); do
    [[ "$(get "$1" "$2" | head -n1)" == 200\ * ]] && return 0
    sleep 1
  done
  nimg_fail "no 200 from ${2} (Host: ${1}) within ${WAIT_S}s"
}

echo "--- version"
got=$(nimg_once -- version | awk '$1 == "Version:" { print $2 }')
[[ "$got" == "$NIMG_VERSION" ]] || nimg_fail "traefik version printed '${got}', want '${NIMG_VERSION}'"

echo "--- start with file + docker providers and a local plugin"
state_mounts=()
for dir in /plugins-storage /letsencrypt; do
  state_mounts+=(--tmpfs "${dir}:uid=${NONROOT_UID},gid=${NONROOT_UID},mode=0700")
done
cid=$(nimg_run "${state_mounts[@]}" \
  -v "${TEST_DIR}/dynamic.yml:/etc/traefik/dynamic/dynamic.yml:ro" \
  -v "${TEST_DIR}/plugins-local:/plugins-local:ro" \
  -v "${DOCKER_SOCK}:${DOCKER_SOCK}:ro" --group-add "$(stat -c %g "$DOCKER_SOCK")" \
  --label traefik.enable=true \
  --label "traefik.http.routers.docker.rule=Host(\`docker.test\`)" \
  --label traefik.http.routers.docker.entrypoints=http \
  --label "traefik.http.services.docker.loadbalancer.server.port=${API_PORT}" \
  -p "127.0.0.1::${WEB_PORT}" -p "127.0.0.1::${API_PORT}" \
  -- --ping --api.insecure \
  --global.checkNewVersion=false --global.sendAnonymousUsage=false \
  --providers.file.directory=/etc/traefik/dynamic \
  --providers.docker --providers.docker.exposedByDefault=false \
  --experimental.localPlugins.addheader.moduleName=example.com/addheader)
web="http://127.0.0.1:$(nimg_port "$cid" "${WEB_PORT}/tcp")"
api="http://127.0.0.1:$(nimg_port "$cid" "${API_PORT}/tcp")"
# Traefik answers 404 until its internal routers are loaded, so wait for 200.
wait_route localhost "${api}/ping"
nimg_expect_http "${api}/ping" 200 "OK"

echo "--- healthcheck"
docker exec "$cid" /usr/local/bin/traefik healthcheck --ping >/dev/null ||
  nimg_fail "traefik healthcheck --ping failed"

echo "--- dashboard"
page=$(curl -sS --max-time 5 -w '\n%{http_code}' "${api}/dashboard/") || nimg_fail "GET ${api}/dashboard/ failed"
[[ "${page##*$'\n'}" == 200 ]] || nimg_fail "GET ${api}/dashboard/: status ${page##*$'\n'}, want 200"
grep -q '<title>Traefik Proxy</title>' <<<"$page" || nimg_fail "dashboard is not the Traefik web UI"
grep -q 'assets/' <<<"$page" || nimg_fail "dashboard HTML references no built assets"

echo "--- file provider route through the local plugin"
wait_route file.test "${web}/ping"
resp=$(get file.test "${web}/ping")
[[ "$(head -n1 <<<"$resp")" == "200 ${PLUGIN_VALUE}" ]] ||
  nimg_fail "file route: got '$(head -n1 <<<"$resp")', want status 200 and ${PLUGIN_HEADER}: ${PLUGIN_VALUE}"
[[ "$(tail -n +2 <<<"$resp")" == OK ]] || nimg_fail "file route: body '$(tail -n +2 <<<"$resp")', want 'OK'"

echo "--- docker provider route"
wait_route docker.test "${web}/ping"
curl -s --max-time 5 "${api}/api/http/routers" | grep -q '"docker@docker"' ||
  nimg_fail "router docker@docker missing from /api/http/routers"

echo "traefik ok"
