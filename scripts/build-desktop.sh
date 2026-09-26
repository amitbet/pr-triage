#!/usr/bin/env bash
set -euo pipefail
export CGO_ENABLED=1

os=$(go env GOOS)
arch=$(go env GOARCH)
out=${DESKTOP_OUT:-dist/desktop}
desktop_version=${DESKTOP_VERSION:-dev}
mkdir -p "$out"

case "$os/$arch" in
  darwin/arm64)
    app="$out/PR Triage.app"
    mkdir -p "$app/Contents/MacOS"
    CGO_LDFLAGS="${CGO_LDFLAGS:-} -framework UniformTypeIdentifiers" \
      go build -tags desktop,production -trimpath -ldflags "-s -w -X main.version=$desktop_version" -o "$app/Contents/MacOS/pr-triage" .
    bundle_version=${desktop_version#v}
    bundle_version=${bundle_version%%-*}
    if [[ ! $bundle_version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then bundle_version=0.1.0; fi
    cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleName</key><string>PR Triage</string>
<key>CFBundleDisplayName</key><string>PR Triage</string>
<key>CFBundleIdentifier</key><string>com.amitbet.pr-triage</string>
<key>CFBundleExecutable</key><string>pr-triage</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>$bundle_version</string>
<key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST
    ditto -c -k --keepParent "$app" "$out/PR-Triage-macos-arm64.zip"
    ;;
  linux/amd64)
    go build -tags desktop,production,webkit2_41 -trimpath -ldflags "-s -w -X main.version=$desktop_version" -o "$out/pr-triage-linux-amd64" .
    tar -C "$out" -czf "$out/pr-triage-linux-amd64.tar.gz" pr-triage-linux-amd64
    ;;
  windows/amd64)
    go build -tags desktop,production -trimpath -ldflags "-s -w -X main.version=$desktop_version -H windowsgui" -o "$out/pr-triage-windows-amd64.exe" .
    ;;
  *)
    echo "unsupported desktop target: $os/$arch" >&2
    exit 1
    ;;
esac
