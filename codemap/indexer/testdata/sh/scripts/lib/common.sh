log() { echo "[$(date)] $*" >&2; }

die() {
  log "error: $*"
  exit 1
}
