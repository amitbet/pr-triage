# Desktop build

`make desktop` builds the Wails app on the current operating system. The app
opens the existing UI in a native window and uses the cgo tree-sitter parser
for code-map indexing and declaration lookup. No frontend build or Node.js is
needed. Running the desktop executable with no arguments opens the window.
On Windows, run `bash scripts/build-desktop.sh` from an MSYS2 UCRT64 shell if
`make` is unavailable.

| Build host | Output | Build requirements |
| --- | --- | --- |
| macOS arm64 | `dist/desktop/PR Manager.app` | Xcode command line tools |
| Linux amd64 | `dist/desktop/pr-manager-linux-amd64` | GCC, GTK 3, WebKit2GTK 4.1 development packages, pkg-config |
| Windows amd64 | `dist/desktop/pr-manager-windows-amd64.exe` | MinGW GCC; WebView2 runtime to run |

The build is native because cgo compiles each tree-sitter grammar for the host
OS and architecture. `.github/workflows/ci.yml` runs tests and builds on
all three hosts, then uploads the results as workflow artifacts. A push to
`main` also tags the next patch version and adds those desktop packages to the
GitHub release alongside the CLI archives. Pushing a version tag directly still
publishes through `.github/workflows/release.yml`. The release assets are
`PR-Manager-macos-arm64.zip`, `pr-manager-linux-amd64.tar.gz`, and
`pr-manager-windows-amd64.exe`. The macOS app is ad-hoc signed but not
notarized. Each stable release also updates `Casks/pr-manager-desktop.rb`, which
installs the app and removes quarantine (see [install](install.md)).

The CLI and browser server remain the pure Go build. `go run . serve`,
`make serve`, and `make build` use gotreesitter and do not require a C compiler.
The desktop build uses the `desktop` Go build tag; its parser dependency graph
does not include gotreesitter. `make test` tests the pure Go build, while
`go test -tags desktop ./...` tests the cgo variant on macOS or Windows. On
Linux with WebKit2GTK 4.1, use `go test -tags desktop,webkit2_41 ./...`.
