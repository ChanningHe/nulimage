#!/usr/bin/env bash
# Nightly rescan of the published images. For every image x variant x flavor
# the `latest<suffix>` tag (the exact version tag for pre-releases, which get no
# latest) is scanned with Grype, because that is what consumers pull.
# `latest<suffix>` present while the current version's tag is missing means a
# bump was merged but its publish never finished: that is an error, not a skip.
# Only "neither tag exists" (nothing published yet) is a SKIP.
#
# Per image and architecture, one GitHub issue "vuln: <image> (<arch>)" is
# created or updated when a scan finds fixable vulnerabilities, and closed when
# all scans of that architecture pass. The arch is part of the title because the
# workflow runs one job per architecture: each issue has exactly one owner, so
# jobs can neither duplicate issues nor close an issue the other arch still fails.
# Any failure other than "not found" (registry, scanner, access denied, nimgctl,
# jq) makes the script exit 1 and leaves the issues untouched.
#
# Env: REGISTRY     registry prefix, e.g. ghcr.io/channinghe
#      NIMGCTL      path of the nimgctl binary; run this script from the repo root
#      RESCAN_ARCH  architecture of this runner (amd64 | arm64), used in issue titles
#      GH_TOKEN, GH_REPO for gh; GITHUB_SERVER_URL/GITHUB_RUN_ID (optional) for the run link
#      GRYPE_REGISTRY_AUTH_{AUTHORITY,USERNAME,PASSWORD} (optional) are passed on to the scanner
set -euo pipefail

: "${REGISTRY:?}" "${NIMGCTL:?}" "${RESCAN_ARCH:?}"

readonly LABEL=vulnerability
readonly FENCE='````' # longer than any run of backticks a table could contain
# `tags` needs some sha for the sha-<git7> tag, which is never the one we use.
readonly PLACEHOLDER_SHA=0000000

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# published REF: 0 = exists, 1 = never published (reason in $missing), 2 = could not tell.
# Only a 404 (not found / manifest unknown / name unknown) means "never published".
# denied, 401 and 403 are errors on purpose: packages must be public, and a
# silently private package would turn the vulnerability alerts off.
missing=
published() {
  local err
  if err=$(docker buildx imagetools inspect "$1" 2>&1 >/dev/null); then
    return 0
  fi
  if grep -qiE 'not found|manifest unknown|name unknown' <<<"$err"; then
    missing=${err%%$'\n'*}
    return 1
  fi
  echo "cannot inspect $1: $err" >&2
  return 2
}

# resolve_target IMAGE VARIANT FLAVOR: sets $target to the ref to scan.
# Returns 0 = scan $target, 1 = nothing published yet (SKIP), 2 = error.
target=
resolve_target() {
  local out refs exact latest="" ref rc msg
  out=$("$NIMGCTL" tags "$1" "$2" "$3" --registry "$REGISTRY" --sha "$PLACEHOLDER_SHA") || {
    echo "cannot compute tags for $1 $2 $3" >&2
    return 2
  }
  mapfile -t refs <<<"$out"
  exact=${refs[0]:-}
  [[ -n $exact ]] || { echo "no tags for $1 $2 $3" >&2; return 2; }
  for ref in "${refs[@]}"; do
    if [[ ${ref##*:} == latest* ]]; then latest=$ref; fi
  done
  target=${latest:-$exact}

  rc=0
  published "$target" || rc=$?
  ((rc != 2)) || return 2
  if ((rc == 1)); then
    msg=$missing
    [[ -n $latest ]] || { echo "SKIP $1 $2 $3: not published ($msg)"; return 1; }
    # latest is missing; the version tag must be missing too.
    rc=0
    published "$exact" || rc=$?
    case $rc in
      0) echo "ERROR $1 $2 $3: $latest does not exist but $exact does" >&2; return 2 ;;
      1) echo "SKIP $1 $2 $3: not published ($msg)"; return 1 ;;
      *) return 2 ;;
    esac
  fi
  [[ -n $latest ]] || return 0
  rc=0
  published "$exact" || rc=$?
  case $rc in
    0) return 0 ;;
    1) echo "ERROR $1 $2 $3: $latest exists but $exact does not: the publish of this version never finished" >&2; return 2 ;;
    *) return 2 ;;
  esac
}

ensure_label() {
  local labels
  labels=$(gh label list --limit 200 --json name --jq '.[].name')
  grep -qxF "$LABEL" <<<"$labels" ||
    gh label create "$LABEL" --force --color B60205 --description "Fixable vulnerabilities in a published image"
}

# issue_title IMAGE: the one title this job owns for IMAGE.
issue_title() { printf 'vuln: %s (%s)' "$1" "$RESCAN_ARCH"; }

# open_issue IMAGE: prints the number of the open issue, nothing when there is
# none. A failing gh or jq is an error: it must not look like "no issue".
open_issue() {
  local issues
  issues=$(gh issue list --state open --label "$LABEL" --limit 200 --json number,title) || return 1
  jq -r --arg t "$(issue_title "$1")" '[.[] | select(.title == $t)][0].number // empty' <<<"$issues"
}

# add_findings BODY REF ERRFILE: append the grype table found in ERRFILE.
add_findings() {
  local body=$1 ref=$2 errf=$3 table
  table=$(awk '/^NAME +INSTALLED/ {p = 1} /^nimgctl:/ {p = 0} p' "$errf")
  {
    printf "### \`%s\`\n\n%s\n%s\n%s\n\n" "$ref" "$FENCE" "${table:-table unavailable, see the run log}" "$FENCE"
  } >>"$body"
}

# report_failure IMAGE BODY: create or update the image's issue.
report_failure() {
  local image=$1 body=$2 number text
  text="$work/issue.md"
  {
    printf "Fixable vulnerabilities (severity high or higher) in published tags of \`%s\` on %s.\n\n" "$image" "$RESCAN_ARCH"
    cat "$body"
    printf 'Fixes arrive through Renovate PRs; this issue closes itself once the nightly scan is clean.\n'
    if [[ -n ${GITHUB_RUN_ID:-} ]]; then
      printf 'Scan run: %s/%s/actions/runs/%s\n' "${GITHUB_SERVER_URL:-https://github.com}" "${GITHUB_REPOSITORY:-}" "$GITHUB_RUN_ID"
    fi
  } >"$text"
  number=$(open_issue "$image") || return 1
  if [[ -n $number ]]; then
    gh issue edit "$number" --body-file "$text" >/dev/null
    echo "updated issue #$number for $image"
  else
    gh issue create --title "$(issue_title "$image")" --label "$LABEL" --body-file "$text"
  fi
}

report_clean() {
  local number
  number=$(open_issue "$1") || return 1
  [[ -n $number ]] || return 0
  gh issue close "$number" --comment "Nightly scan of every published tag of \`$1\` on $RESCAN_ARCH is clean."
}

# scan_image IMAGE: returns 1 if any combo could not be scanned.
scan_image() {
  local image=$1 combos variant flavor ref rc errf failed=0 passed=0 broke=0
  local body="$work/body.md"
  : >"$body"
  combos=$(jq -r --arg i "$image" '.include[] | select(.image == $i) | "\(.variant) \(.flavor)"' <<<"$matrix") || {
    echo "cannot list the variants of $image" >&2
    return 1
  }
  [[ -n $combos ]] || { echo "no variant/flavor found for $image" >&2; return 1; }
  while read -r variant flavor; do
    rc=0
    resolve_target "$image" "$variant" "$flavor" || rc=$?
    case $rc in
      0) ref=$target ;;
      1) continue ;;
      *) broke=1; continue ;;
    esac
    errf="$work/scan.err"
    if "$NIMGCTL" scan "$image" --variant "$variant" --flavor "$flavor" --ref "$ref" 2>"$errf"; then
      passed=$((passed + 1))
    elif grep -q 'fixable vulnerabilities' "$errf"; then
      failed=$((failed + 1))
      add_findings "$body" "$ref" "$errf"
    else
      broke=1
    fi
    cat "$errf" >&2
  done <<<"$combos"

  if ((failed > 0)); then
    report_failure "$image" "$body" || broke=1
  elif ((passed > 0 && broke == 0)); then
    report_clean "$image" || broke=1
  fi
  ((broke == 0))
}

matrix=$("$NIMGCTL" changed --all)
[[ -n $matrix ]] || { echo "nimgctl changed --all printed nothing" >&2; exit 1; }
images=$("$NIMGCTL" list)
[[ -n $images ]] || { echo "nimgctl list printed nothing" >&2; exit 1; }
ensure_label
status=0
for image in $images; do
  scan_image "$image" || status=1
done
exit "$status"
