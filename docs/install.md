# Install and release

Each release contains one executable with the web UI, code-map indexer and
standard scoring rules embedded. No source checkout, Node.js, shell scripts or
adjacent UI files are needed. Git and GitHub CLI (`gh`) remain runtime dependencies
for GitHub PRs. Log in with `gh auth login`. Install the CLI or configure credentials
for whichever model provider you choose.

## Homebrew on macOS

The tap lives in this repository, so tap it by URL once:

```sh
brew tap amitbet/pr-triage https://github.com/amitbet/pr-triage
brew install --cask amitbet/pr-triage/pr-triage
gh auth login
pr-triage serve
```

Homebrew installs `git` and `gh` as dependencies. The server picks an available
port on `127.0.0.1`, prints its URL and opens your browser. Stop it with Ctrl+C.
`-addr` still accepts an explicit address.

The initial cask removes quarantine from this unsigned executable using a
post-install hook. Apple signing and notarization are not configured yet.

## Direct downloads, including Windows and Linux

Download the archive for your OS and architecture from GitHub Releases and verify
it against `checksums.txt`. Extract `pr-triage` or `pr-triage.exe` into a directory
on your PATH. macOS and Linux use `.tar.gz`; Windows uses `.zip`. Both amd64 and
arm64 are built. On Windows, install Git and GitHub CLI separately, then run:

```powershell
gh auth login
pr-triage.exe serve
```

There is no Windows package-manager manifest yet.

## Data and code maps

Installed commands use `os.UserCacheDir()/pr-triage`:

- macOS: `~/Library/Caches/pr-triage`
- Linux: `$XDG_CACHE_HOME/pr-triage`, or `~/.cache/pr-triage`
- Windows: `%LocalAppData%\pr-triage`

Results, drafts and clones live under this directory. `-cache DIR` overrides their
root; `-codemap DIR` independently selects the map output. Existing checkout-local
`.cache` data is not migrated automatically. `make ui` uses `.cache` and
`.cache/map` for development.

PR triage builds missing code maps with the bundled indexer. It links the repo from
the local code directory (`-code-root`, `PR_TRIAGE_CODE_ROOT`) if it is there, else
clones it under `CACHE/workspace/code`, or uses the workspace selected by
`PR_TRIAGE_WORKSPACE`. `pr-triage index -org ORG` indexes a whole GitHub or GitHub
Enterprise org at once (see the README's "Indexing your own repos").
A workspace can also contain `repos.yaml` metadata alongside `code/` checkouts.

You can also index one local checkout:

```sh
pr-triage codemap build -C /path/to/repo
pr-triage codemap top
pr-triage codemap lookup 'repo/path/to/file.go'
pr-triage -C /path/to/repo -classifier off -summarizer off
```

`codemap build -C` writes a map for that checkout, replacing the map at its output
location. Use `-output DIR` and the triage command's `-codemap DIR` for separate maps.
Use `codemap build -workspace DIR` to rank several repositories together.

Go type-aware indexing needs a Go toolchain on PATH. If it is absent, or the source
requires a newer Go type checker than the executable contains, the indexer warns
and retains declarations and complexity without typed call edges. Install Go and
use `codemap build -force` after upgrading to rebuild degraded graphs. Other
language parsers are compiled into the binary.

`codemap build -config FILE` overrides the embedded scoring rules. Output and graph
cache paths in that file are relative to its directory; `-output` and `-cache`
override them. `pr-triage serve -codemap-config FILE` uses the same override for
automatic builds. Start from `codemap/indexer/default.yaml`.

## Maintainer setup

The Homebrew tap is this repository. CI commits `Casks/pr-triage.rb` to the
default branch when it publishes a release, so `brew tap amitbet/pr-triage
https://github.com/amitbet/pr-triage` picks it up. No separate tap repository or
extra token is needed. The workflow `GITHUB_TOKEN` uploads the release assets
and pushes the cask. If the default branch is protected, allow GitHub Actions to
push to it.

A push to `main` publishes a release. CI resolves the next patch version, tags
that commit, and uploads the binaries. The first release is `v0.1.0`. The next
pushes become `v0.1.1`, `v0.1.2`, and so on. If the commit already has a stable
version tag, CI publishes that tag instead of incrementing the patch number.

`.github/workflows/ci.yml` runs vet, tests, and the smoke check, builds the
desktop apps for macOS arm64, Linux amd64, and Windows amd64, then publishes
those packages with the CLI archives and checksums and updates the cask.
`checksums.txt` covers the GoReleaser CLI archives. The desktop packages are
separate release assets covered by `desktop-checksums.txt`. Pull requests run
the same tests and desktop builds without publishing.

Pushing a version tag yourself, such as `v1.0.0`, still runs
`.github/workflows/release.yml`. Use that for a minor or major bump. Tags
created by CI do not start that workflow again. A prerelease tag such as
`v0.2.0-rc.1` publishes release assets and leaves the cask unchanged.

To validate locally:

```sh
goreleaser check
goreleaser release --snapshot --clean
python3 scripts/smoke-release.py dist/pr-triage_darwin_arm64_v8.0/pr-triage
```

Use the actual native executable path produced under `dist/` for the smoke check.
Snapshot mode builds archives and the cask locally without publishing. Releases
include version, commit and build date, available with `pr-triage version`.
GoReleaser is pinned to v2.18.2 in CI and in the tag workflow.
