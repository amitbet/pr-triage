#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
script=$root/scripts/next-version.sh
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

export GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.com
export GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.com

cd "$tmp"
git init -q -b main
git commit -q --allow-empty -m init

check() {
  local want=$1
  local got
  got=$(bash "$script")
  if [[ "$got" != "$want" ]]; then
    echo "expected $want, got $got" >&2
    exit 1
  fi
}

check v0.1.0

git tag v0.1.0
check v0.1.0

git commit -q --allow-empty -m two
check v0.1.1

git tag v0.1.9
check v0.1.9

git commit -q --allow-empty -m three
check v0.1.10

git tag v0.2.0-rc.1
check v0.1.10

git tag v8.0
check v0.1.10

git commit -q --allow-empty -m four
git tag v0.9.0
git commit -q --allow-empty -m five
git tag v0.10.0
git commit -q --allow-empty -m six
check v0.10.1

git tag v1.2.9
check v1.2.9

git commit -q --allow-empty -m seven
check v1.2.10

git tag v1.0.0
git tag v2.0.0
check v2.0.0

echo "next-version ok"
