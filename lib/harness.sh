#!/usr/bin/env bash
# Shared functional-test harness, sourced by images/<name>/test.sh.
#
# Requires NIMG_REF (the image under test). Every container started through
# these helpers runs under a hardened profile, carries the label nimg.test=1
# and is removed when the sourcing script exits.
#
# Helpers that print a value (nimg_run, nimg_once, nimg_port) are meant to be
# called inside $(...); run the script with `set -e` so that a failure inside
# the substitution stops it. Call nimg_fail directly for assertions.
#
# A caller that installs its own EXIT trap must call nimg_cleanup from it.

NIMG_RUN_ID="$$-${RANDOM}${RANDOM}"
readonly NIMG_RUN_ID
readonly NIMG_HARDEN=(--read-only --cap-drop=ALL --security-opt=no-new-privileges
  --label nimg.test=1 --label "nimg.test.run=${NIMG_RUN_ID}")

# Containers of this run, found by label so that registration survives
# $(...) subshells.
_nimg_containers() {
  docker ps -a -q --filter "label=nimg.test.run=${NIMG_RUN_ID}"
}

nimg_cleanup() {
  local ids
  ids=$(_nimg_containers 2>/dev/null) || return 0
  [[ -z "$ids" ]] && return 0
  # shellcheck disable=SC2086 # ids is a whitespace separated list
  docker rm -f -v $ids >/dev/null 2>&1 || true
}
trap nimg_cleanup EXIT

# nimg_fail <msg>: print msg and the logs of every container, then exit 1.
nimg_fail() {
  local id
  echo "FAIL: $*" >&2
  while read -r id; do
    [[ -n "$id" ]] || continue
    echo "--- logs of container ${id} ($(docker inspect -f '{{.Config.Image}}' "$id" 2>/dev/null))" >&2
    docker logs "$id" >&2 2>&1 || true
  done < <(_nimg_containers)
  exit 1
}

_nimg_need_ref() {
  [[ -n "${NIMG_REF:-}" ]] || { echo "FAIL: NIMG_REF is not set" >&2; exit 1; }
}

# _nimg_split <args...>: split at the first "--" into _nimg_dargs and _nimg_cmd.
_nimg_split() {
  _nimg_dargs=()
  _nimg_cmd=()
  while (($#)); do
    if [[ "$1" == "--" ]]; then
      shift
      _nimg_cmd=("$@")
      return 0
    fi
    _nimg_dargs+=("$1")
    shift
  done
}

# nimg_run [docker-run-args...] -- [cmd...]: start detached, print the id.
nimg_run() {
  _nimg_need_ref
  _nimg_split "$@"
  docker run -d "${NIMG_HARDEN[@]}" "${_nimg_dargs[@]}" "$NIMG_REF" "${_nimg_cmd[@]}"
}

# nimg_once [docker-run-args...] -- [cmd...]: run to completion, print stdout.
nimg_once() {
  _nimg_need_ref
  _nimg_split "$@"
  docker run --rm "${NIMG_HARDEN[@]}" "${_nimg_dargs[@]}" "$NIMG_REF" "${_nimg_cmd[@]}"
}

# nimg_port <cid> <port/proto>: host port published on 127.0.0.1.
nimg_port() {
  local out
  out=$(docker port "$1" "$2" | head -n1) || nimg_fail "no port mapping for $2 on $1"
  [[ -n "$out" ]] || nimg_fail "no port mapping for $2 on $1"
  echo "${out##*:}"
}

# nimg_wait_http <url> [timeout_s=30]: wait until the URL answers at all.
nimg_wait_http() {
  local url=$1 timeout=${2:-30} i
  for ((i = 0; i < timeout; i++)); do
    curl -s -o /dev/null --max-time 2 "$url" && return 0
    sleep 1
  done
  nimg_fail "no HTTP response from ${url} within ${timeout}s"
}

# nimg_expect_http <url> <code> [body]: assert status and, if given, exact body.
nimg_expect_http() {
  local url=$1 want=$2 body_file code
  body_file=$(mktemp)
  code=$(curl -s -o "$body_file" -w '%{http_code}' --max-time 10 "$url") || code=000
  if [[ "$code" != "$want" ]]; then
    rm -f "$body_file"
    nimg_fail "GET ${url}: status ${code}, want ${want}"
  fi
  if (($# >= 3)) && ! printf '%s' "$3" | cmp -s - "$body_file"; then
    local got
    got=$(cat "$body_file")
    rm -f "$body_file"
    nimg_fail "GET ${url}: body '${got}', want '${3}'"
  fi
  rm -f "$body_file"
}

# nimg_logs_contain <cid> <grep-pattern>: status 0 if the logs match.
nimg_logs_contain() {
  local logs
  logs=$(docker logs "$1" 2>&1) || return 1
  grep -q -e "$2" <<<"$logs"
}
