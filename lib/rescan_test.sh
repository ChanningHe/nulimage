#!/usr/bin/env bash
# Stub tests for lib/rescan.sh: docker, gh and nimgctl are replaced by scripts
# that read their behaviour from environment variables and log their calls.
# Run from the repo root: lib/rescan_test.sh
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir "$tmp/bin"

# docker buildx imagetools inspect REF: succeeds when REF is listed in
# STUB_PRESENT (space separated), else prints STUB_MISSING_ERR and fails.
cat >"$tmp/bin/docker" <<'EOF'
#!/usr/bin/env bash
ref=${*: -1}
for p in $STUB_PRESENT; do
  [[ $p == "$ref" ]] && exit 0
done
echo "${STUB_MISSING_ERR:-ERROR: $ref: not found}" >&2
exit 1
EOF

# gh: label list / issue list answer from the environment, writes are logged.
cat >"$tmp/bin/gh" <<'EOF'
#!/usr/bin/env bash
echo "gh $*" >>"$STUB_LOG"
case "$1 $2" in
  "label list") echo "${STUB_LABELS-vulnerability}" ;;
  "issue list") [[ -z ${STUB_GH_LIST_FAILS:-} ]] || exit 1; echo "${STUB_ISSUES:-[]}" ;;
  *) ;;
esac
EOF

# nimgctl: list / changed / tags / scan. tags prints STUB_TAGS (one per line).
cat >"$tmp/bin/nimgctl" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  list) [[ -z ${STUB_LIST_FAILS:-} ]] || exit 1; printf '%s' "${STUB_LIST-img}" ;;
  changed) echo '{"include":[{"image":"img","variant":"default","flavor":"release"}]}' ;;
  tags) printf '%s\n' $STUB_TAGS ;;
  scan)
    echo "scan $*" >>"$STUB_LOG"
    case "${STUB_SCAN:-pass}" in
      pass) ;;
      vuln)
        printf 'NAME INSTALLED FIXED-IN TYPE VULNERABILITY SEVERITY\nlibfoo 1 2 deb CVE-1 High\nnimgctl: fixable vulnerabilities\n' >&2
        exit 1 ;;
      *) echo "nimgctl: boom" >&2; exit 1 ;;
    esac ;;
esac
EOF
chmod +x "$tmp/bin/"*

reg=r.example/o
full_tags="$reg/img:1.2.3 $reg/img:1.2 $reg/img:1 $reg/img:latest $reg/img:sha-0000000"
pre_tags="$reg/img:1.2.3-rc1 $reg/img:sha-0000000"

fails=0
# case NAME EXPECTED_RC [ENV=VALUE...]: runs rescan.sh with the stubs; the
# case's own checks follow with `logged PATTERN` / `not_logged PATTERN`.
case_run() {
  local name=$1 want=$2 rc=0
  shift 2
  : >"$tmp/log"
  env PATH="$tmp/bin:$PATH" NIMGCTL="$tmp/bin/nimgctl" REGISTRY=$reg RESCAN_ARCH=amd64 \
    STUB_LOG="$tmp/log" STUB_TAGS="$full_tags" STUB_PRESENT="" "$@" \
    "$root/lib/rescan.sh" >"$tmp/out" 2>&1 || rc=$?
  if ((rc != want)); then
    echo "FAIL $name: exit $rc, want $want"
    sed 's/^/    /' "$tmp/out"
    fails=$((fails + 1))
    current=bad
  else
    current=ok
  fi
  current_name=$name
}
logged() {
  [[ $current == ok ]] || return 0
  if ! grep -qE -- "$1" "$tmp/log" "$tmp/out"; then
    echo "FAIL $current_name: expected '$1' in log/output"
    sed 's/^/    /' "$tmp/out" "$tmp/log"
    fails=$((fails + 1))
    current=bad
  fi
}
not_logged() {
  [[ $current == ok ]] || return 0
  if grep -qE -- "$1" "$tmp/log"; then
    echo "FAIL $current_name: unexpected '$1' in gh/scan log"
    sed 's/^/    /' "$tmp/log"
    fails=$((fails + 1))
    current=bad
  fi
}
done_case() { [[ $current != ok ]] || echo "ok   $current_name"; }

both="$reg/img:1.2.3 $reg/img:latest"
issue7='[{"number":7,"title":"vuln: img (amd64)"},{"number":8,"title":"vuln: img (arm64)"}]'

# MEDIUM-4: the scan target is latest, not the version tag.
case_run "scans latest when both tags exist" 0 STUB_PRESENT="$both"
logged "--ref $reg/img:latest"
not_logged "--ref $reg/img:1.2.3"
done_case

case_run "latest present, version tag missing is an error, issues untouched" 1 \
  STUB_PRESENT="$reg/img:latest" STUB_ISSUES="$issue7" STUB_SCAN=vuln
logged "never finished"
not_logged "^scan "
not_logged "gh issue (create|edit|close)"
done_case

case_run "version tag present, latest missing is an error" 1 STUB_PRESENT="$reg/img:1.2.3"
logged "does not exist but"
done_case

case_run "nothing published is a SKIP" 0 STUB_ISSUES="$issue7"
logged "SKIP img default release"
not_logged "gh issue (create|edit|close)"
done_case

for err in "ERROR: x: manifest unknown" "ERROR: name unknown: repository does not exist"; do
  case_run "SKIP on '$err'" 0 STUB_MISSING_ERR="$err"
  logged "SKIP"
  done_case
done

for err in "unexpected status: 403 Forbidden" "denied: permission_denied" "401 Unauthorized"; do
  case_run "access error '$err' fails the run" 1 STUB_MISSING_ERR="$err" STUB_ISSUES="$issue7"
  logged "cannot inspect"
  not_logged "gh issue (create|edit|close)"
  done_case
done

# Pre-release versions have no latest tag: the exact tag is scanned.
case_run "pre-release scans the exact tag" 0 STUB_TAGS="$pre_tags" STUB_PRESENT="$reg/img:1.2.3-rc1"
logged "--ref $reg/img:1.2.3-rc1"
done_case

case_run "pre-release not published is a SKIP" 0 STUB_TAGS="$pre_tags"
logged "SKIP"
done_case

# Issues: owned per architecture.
case_run "vulnerable: creates the arch-titled issue" 0 STUB_PRESENT="$both" STUB_SCAN=vuln
logged "gh issue create --title vuln: img \\(amd64\\)"
done_case

case_run "vulnerable: updates the own arch issue, not the other" 0 \
  STUB_PRESENT="$both" STUB_SCAN=vuln STUB_ISSUES="$issue7"
logged "gh issue edit 7 "
not_logged "gh issue (create|edit 8|close)"
done_case

case_run "clean: closes only the own arch issue" 0 STUB_PRESENT="$both" STUB_ISSUES="$issue7"
logged "gh issue close 7 "
not_logged "gh issue (create|close 8)"
done_case

case_run "arm64 job ignores the amd64 issue" 0 RESCAN_ARCH=arm64 STUB_PRESENT="$both" STUB_ISSUES="$issue7"
logged "gh issue close 8 "
not_logged "gh issue close 7"
done_case

case_run "scanner crash fails and writes nothing" 1 STUB_PRESENT="$both" STUB_SCAN=crash STUB_ISSUES="$issue7"
not_logged "gh issue (create|edit|close)"
done_case

# Parallel arch jobs may both find the label missing: creation must be idempotent.
case_run "missing label is created with --force" 0 STUB_LABELS="" STUB_PRESENT="$both"
logged "gh label create vulnerability --force"
done_case

# LOW-2: failures of the helpers must not look like "nothing to do".
case_run "empty nimgctl list fails" 1 STUB_LIST="" STUB_PRESENT="$both"
logged "printed nothing"
done_case

case_run "failing nimgctl list fails" 1 STUB_LIST_FAILS=1 STUB_PRESENT="$both"
done_case

case_run "failing gh issue list does not create a duplicate" 1 \
  STUB_PRESENT="$both" STUB_SCAN=vuln STUB_GH_LIST_FAILS=1
not_logged "gh issue (create|edit|close)"
done_case

((fails == 0)) || { echo "$fails check(s) failed"; exit 1; }
echo "all rescan stub tests passed"
