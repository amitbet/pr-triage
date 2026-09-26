#!/usr/bin/env bash
# Print the release tag for HEAD.
# A stable tag already on HEAD is reused. Otherwise the latest stable tag's
# patch number is incremented. With no stable tags, the result is v0.1.0.
# The caller fetches tags first. Prerelease tags are ignored.
set -euo pipefail

pick_stable() {
  awk '/^v[0-9]+\.[0-9]+\.[0-9]+$/ { print; exit }'
}

on_head=$(git tag --points-at HEAD --sort=-v:refname | pick_stable || true)
if [[ -n "$on_head" ]]; then
  printf '%s\n' "$on_head"
  exit 0
fi

latest=$(git tag -l --sort=-v:refname | pick_stable || true)
if [[ -z "$latest" ]]; then
  printf 'v0.1.0\n'
  exit 0
fi

version=${latest#v}
major=${version%%.*}
rest=${version#*.}
minor=${rest%%.*}
patch=${rest#*.}
printf 'v%s.%s.%s\n' "$major" "$minor" "$((10#$patch + 1))"
