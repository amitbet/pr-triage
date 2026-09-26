#!/usr/bin/env bash
# Build and run the code-map indexer (cmd/codemap) against a development map
# in .cache/map.
#
# The indexer type-checks every Go repo in the workspace, so it must be
# compiled with a Go toolchain at least as new as the newest `go` directive in
# those repos. This wrapper picks that version and lets GOTOOLCHAIN fetch it.
# The workspace is $PR_MANAGER_WORKSPACE (a directory containing code/<repo>);
# $CODEMAP_CONFIG optionally overrides the embedded scoring rules.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WS="${PR_MANAGER_WORKSPACE:-}"
BIN="$ROOT_DIR/.cache/codemap/bin/codemap"
MAP="$ROOT_DIR/.cache/map"
PATHS=(-output "$MAP" -cache "$ROOT_DIR/.cache/graphs")
CONFIG=()
if [ -n "${CODEMAP_CONFIG:-}" ]; then
  CONFIG=(-config "$CODEMAP_CONFIG")
fi

newest_go() {
  [ -n "$WS" ] && [ -d "$WS/code" ] || return 0
  find -L "$WS/code" -maxdepth 3 -name go.mod -not -path '*/node_modules/*' -print0 2>/dev/null |
    xargs -0 grep -h '^go [0-9]' 2>/dev/null | awk '{ print $2 }' |
    sort -t. -k1,1n -k2,2n -k3,3n | tail -1
}

want="$(newest_go)"
if [ -n "$want" ]; then
  case "$want" in
    *.*.*) ;;
    *) want="$want.0" ;;
  esac
  export GOTOOLCHAIN="go$want"
fi

stamp="$BIN.toolchain"
if [ ! -x "$BIN" ] || [ "$(cat "$stamp" 2>/dev/null)" != "${GOTOOLCHAIN:-}" ] ||
  [ -n "$(find "$ROOT_DIR/cmd/codemap" "$ROOT_DIR/codemap" -name '*.go' -newer "$BIN" -print -quit)" ] ||
  [ "$ROOT_DIR/go.mod" -nt "$BIN" ]; then
  mkdir -p "$(dirname "$BIN")"
  (cd "$ROOT_DIR" && go build -o "$BIN" ./cmd/codemap)
  printf '%s' "${GOTOOLCHAIN:-}" >"$stamp"
fi

cd "$ROOT_DIR"
if [ "${1:-}" = build ] && [ -n "$WS" ]; then
  shift
  exec "$BIN" build -workspace "$WS" "${PATHS[@]}" ${CONFIG[@]+"${CONFIG[@]}"} "$@"
fi
case "${1:-}" in
  build|rank) sub="$1"; shift; exec "$BIN" "$sub" "${PATHS[@]}" ${CONFIG[@]+"${CONFIG[@]}"} "$@" ;;
  lookup|top) sub="$1"; shift; exec "$BIN" "$sub" -map "$MAP" "$@" ;;
esac
exec "$BIN" "$@"
