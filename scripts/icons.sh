#!/usr/bin/env bash
# Renders assets/icon/icon.svg into the committed PNG and macOS .icns.
# Needs rsvg-convert (brew install librsvg) and macOS iconutil.
set -euo pipefail
cd "$(dirname "$0")/../assets/icon"

rsvg-convert -w 1024 -h 1024 icon.svg -o icon.png

# macOS draws icons inside a margin, so the tile is 824px on a 1024px canvas.
iconset=$(mktemp -d)/icon.iconset
mkdir -p "$iconset"
for size in 16 32 128 256 512; do
  for scale in 1 2; do
    px=$((size * scale))
    pad=$((px * 100 / 1024))
    name=icon_${size}x${size}
    if [[ $scale == 2 ]]; then name+=@2x; fi
    rsvg-convert -w $((px - 2 * pad)) -h $((px - 2 * pad)) \
      --page-width $px --page-height $px --left $pad --top $pad \
      icon.svg -o "$iconset/$name.png"
  done
done
iconutil -c icns "$iconset" -o icon.icns
