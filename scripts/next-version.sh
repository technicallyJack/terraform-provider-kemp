#!/usr/bin/env bash
# Prints the next release version (vX.Y.Z) for the commit at HEAD, derived
# from the Conventional Commits since the latest v* tag, or nothing if no
# commit since then warrants a release.
#
#   feat:                          minor bump
#   fix: / perf:                   patch bump
#   type!: or a BREAKING CHANGE    major bump (minor while below 1.0.0)
#   anything else (docs:, chore:, test:, build:, ...): no release
#
# With no tags yet, the first release is v0.1.0.
set -euo pipefail

last=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null || true)
if [[ -z "$last" ]]; then
  echo "v0.1.0"
  exit 0
fi

if [[ ! "$last" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
  echo "latest tag $last is not vX.Y.Z" >&2
  exit 1
fi
major=${BASH_REMATCH[1]} minor=${BASH_REMATCH[2]} patch=${BASH_REMATCH[3]}

bump=none
rank() { case $1 in major) echo 3 ;; minor) echo 2 ;; patch) echo 1 ;; *) echo 0 ;; esac; }
raise() { if (( $(rank "$1") > $(rank "$bump") )); then bump=$1; fi; }

# One record per commit: subject, then body, separated by NUL.
while IFS= read -r -d '' commit; do
  subject=${commit%%$'\n'*}
  body=${commit#"$subject"}
  if [[ "$subject" =~ ^[a-z]+(\([^\)]*\))?!: ]] || [[ "$body" =~ (^|$'\n')BREAKING[\ -]CHANGE: ]]; then
    raise major
  elif [[ "$subject" =~ ^feat(\([^\)]*\))?: ]]; then
    raise minor
  elif [[ "$subject" =~ ^(fix|perf)(\([^\)]*\))?: ]]; then
    raise patch
  fi
done < <(git log -z --format='%B' "$last..HEAD")

# Below 1.0.0, breaking changes bump the minor version.
if [[ $bump == major && $major == 0 ]]; then
  bump=minor
fi

case $bump in
  major) echo "v$((major + 1)).0.0" ;;
  minor) echo "v$major.$((minor + 1)).0" ;;
  patch) echo "v$major.$minor.$((patch + 1))" ;;
  none) ;;
esac
