#!/usr/bin/env bash
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
source "$DIR/lib/common.sh"

# Deploys one service.
deploy() {
  local svc="$1"
  if [[ -z "$svc" ]]; then
    die "no service"
  fi
  log "deploying $svc"
  kubectl apply -f "$svc.yaml" || die "apply failed"
}

main() {
  for s in "$@"; do deploy "$s"; done
}

main "$@"
