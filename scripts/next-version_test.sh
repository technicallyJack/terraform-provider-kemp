#!/usr/bin/env bash
# Tests next-version.sh against throwaway git repos. Run: scripts/next-version_test.sh
set -euo pipefail
script="$(cd "$(dirname "$0")" && pwd)/next-version.sh"
fail=0

# check NAME EXPECTED TAG COMMIT_MESSAGE...: repo with one tagged commit (or
# none if TAG is "-"), then one commit per message.
check() {
  local name=$1 want=$2 tag=$3; shift 3
  local dir; dir=$(mktemp -d)
  (
    cd "$dir"
    git init -q -b main
    git -c user.name=t -c user.email=t@t commit -q --allow-empty -m "chore: init"
    [[ $tag == - ]] || git tag "$tag"
    for msg in "$@"; do git -c user.name=t -c user.email=t@t commit -q --allow-empty -m "$msg"; done
  )
  local got; got=$(cd "$dir" && "$script")
  rm -rf "$dir"
  if [[ "$got" == "$want" ]]; then echo "ok   $name"; else echo "FAIL $name: got '$got', want '$want'"; fail=1; fi
}

check "first release"           v0.1.0 -      "feat: x"
check "no tags, no commits"     v0.1.0 -
check "feat bumps minor"        v0.2.0 v0.1.0 "fix: a" "feat: b"
check "fix bumps patch"         v0.1.1 v0.1.0 "fix(client): a"
check "perf bumps patch"        v1.2.4 v1.2.3 "perf: faster"
check "docs only: no release"   ""     v0.1.0 "docs: a" "chore: b" "test: c" "build: d"
check "nothing since tag"       ""     v0.1.0
check "bang below 1.0 -> minor" v0.2.0 v0.1.5 "feat!: drop thing"
check "bang at 1.x -> major"    v2.0.0 v1.4.2 "fix(api)!: change"
check "BREAKING CHANGE footer"  v2.0.0 v1.0.0 $'feat: x\n\nBREAKING CHANGE: removed y'
check "highest bump wins"       v1.1.0 v1.0.3 "fix: a" "feat(rules): b" "docs: c"
check "feature mentioned in body only" "" v0.1.0 $'chore: deps\n\nfeat: not a subject'
check "non-conventional ignored" ""    v0.1.0 "Merge branch 'x'" "update stuff"

exit $fail
