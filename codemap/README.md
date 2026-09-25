# codemap

`codemap` scores every piece of code in a workspace on two axes: **impact**, how
much a bad change there can break, and **likelihood**, how often changes there
go wrong (its git history and complexity).
It indexes every repo under `<workspace>/code/` and writes a map directory, a set of JSONL files
with one record per repo, directory, file and busy symbol. A PR bot (or an
agent) looks up the hunk it is reviewing and falls back from symbol to file to
directory to repo when the exact code is not in the map.

```sh
make codemap WORKSPACE=~/ws                 # refresh (re-extracts only repos that changed)
make codemap WORKSPACE=~/ws MAP_REPO=api    # force one repo to re-extract from scratch
make codemap WORKSPACE=~/ws FORCE=1         # re-extract everything from scratch
make codemap-rank CODEMAP_CONFIG=my.yaml    # re-score after editing a scoring config (no extraction)
make codemap-lookup TARGET='api/internal/user/server.go:(*Server).GetProfile'
make codemap-lookup TARGET=api/dao/postgresql/query.sql.gen.go:250-270
scripts/codemap.sh lookup -repo api -diff pr.diff
scripts/codemap.sh top -level file -repo api -n 20             # -by impact|likelihood|fixes|rank|rollback
```

The development map goes to `.cache/map`. `scripts/codemap.sh` compiles `cmd/codemap` with the newest Go toolchain
any indexed repo asks for; the type checker refuses packages newer than itself.
The workspace comes from `WORKSPACE` or `PR_TRIAGE_WORKSPACE`; scoring rules
default to the embedded [`indexer/default.yaml`](indexer/default.yaml).

## What gets measured

### Rank (blast radius)

The indexer builds one dependency graph for the whole workspace. An edge
`A -> B` means A calls, references or implements through B.

- **Go** is loaded with full type information (`go/packages`). Every top-level
  function, method, type, var and const is a node. Every resolved reference is
  an edge, including struct field access (mapped to the owning type).
  Interface methods link to every implementation in the workspace, including
  implementations in other repos, so calls through DI interfaces reach the
  concrete code. Mocks and fakes are excluded.
- **Cross-repo Go** edges come from module imports: services import
  shared libraries and each other's `pkg/client` modules. Symbols
  resolve by import path, so version pins do not matter as long as the
  symbol still exists at the indexed commit.
- **OpenAPI contracts** turn into operation nodes (`api.yaml:<operationId>`).
  Generated client methods point at the operation, and the operation points
  at the generated server interface, which points at the handler. A change to
  a handler therefore ranks by who calls the API, even across repos.
- **TypeScript** is lexed, not type-checked: top-level
  declarations and class members, import bindings (with tsconfig path
  aliases and barrel re-exports), identifier uses per declaration, and URL
  literals. A declaration ends where the next top-level statement starts
  (`Foo.displayName = ...` and `root.render(...)` belong to the module body),
  and decorators belong to the declaration below them. JSX is read as
  markup: component names and `{expressions}` are code, attribute names and
  text are not (`.ts` files have no JSX). A class depends on its members;
  `this.m` inside the class and `C.m` on a class name reach the member, other
  receivers (`this.repo.save()`) are not resolved.
  `'/settings/api/v1/profiles'` (the first path segment names the service's
  repo) and template literals built from exported URL constants are matched
  to operations in that service's specs, which is
  how backend handlers pick up webapp callers. Plain JavaScript goes through
  the same lexer, including CommonJS (`require`, `module.exports`,
  `exports.x`).
- **Java**, **Python**, **C#** and **Rust** are parsed with tree-sitter
  grammars (pure-Go [gotreesitter](https://github.com/odvcencio/gotreesitter),
  no cgo), not compiled. Declarations, imports and complexity come off the
  syntax tree; name resolution then runs on the tree's tokens.
- **Java**: types (nested ones too), methods and
  constructors. Names resolve through the package, single, wildcard and
  static imports. A call resolves when the receiver's type is known: a type
  name, `this`/`super`, an unqualified call in the enclosing class, or a
  field, parameter or variable declared with a workspace type. An overriding
  method gets an `impl` edge from the method it overrides, looked up through
  `extends`/`implements`. Tests (`src/test/`, `*Test.java`) are left out.
- **Python**: module-level functions, classes, methods and
  module-level assignments. Imports resolve to files through the source
  roots (repo root, `src/`, the directory of each `pyproject.toml`/`setup.py`,
  the parent of each top-level package), including relative imports,
  package re-exports and star imports. Attribute chains (`pkg.mod.func`,
  `Cls.method`) resolve through modules and classes. Calls on `self`/`cls`
  and on names assigned or annotated with a workspace class resolve to the
  method, through base classes. Parameters and locals shadow module names.
  Tests (`tests/`, `test_*.py`, `conftest.py`) are left out.
- **C#**: types (nested ones too), methods,
  constructors, properties, indexers and operators. Both sides of an `#if`
  are read. Type names resolve
  through the enclosing namespaces and `using` directives (`global using`,
  `using static`, aliases). The parts of a `partial` type share their
  members. A call or property use resolves when the receiver's type is
  known: a type name, `this`/`base`, an unqualified member of the enclosing
  type, or a field, property, parameter or variable declared with a
  workspace type (`var x = new T()` too). Extension methods resolve by name
  when their static class is visible in the file. Overriding members get
  `impl` edges through the base list. Tests (`*Tests.cs`, `*.Tests/`,
  `tests/`) and `bin/`/`obj/` are left out; `*.g.cs`, `*.Designer.cs` and
  `<auto-generated>` files are tagged generated.
- **Rust**: functions, structs, enums, traits, type aliases, consts,
  statics, `macro_rules!`, inline modules, and the functions and consts of
  `impl` blocks (`Type::fn`) and traits (`Trait::fn`). Each Cargo package is
  a crate named after its `[lib]`/`[package]` name; modules follow the files
  under `src/` (`a/b.rs` and `a/b/mod.rs` are `crate::a::b`, `#[path]` is
  followed) plus inline `mod` blocks. Names resolve through the module's
  items, `use` declarations (lists, aliases, globs, `pub use` re-exports,
  `crate`/`self`/`super`) and other workspace crates. A method call or field
  resolves when the receiver's type is known: `self`, or a parameter, `let`
  binding or struct field declared with a workspace type (`Box`, `Arc`,
  `Option`, `Result` and references seen through, generic parameters by
  their bound), or a binding from a call that returns one
  (`let s = Svc::new()`). A trait's methods get `impl` edges to the
  implementations. Tests (`tests/`, `benches/`, `*_tests.rs`, `#[test]`,
  `#[cfg(test)]` modules) and `target/` are left out.
- **Shell, PowerShell, C, C++, PHP, Scala, Kotlin, Ruby, Swift and Dart**
  share one table-driven parser (`codemap/decls/gen.go`, a spec per
  language in `gen_langs.go`) and one extractor
  (`codemap/indexer/extract_gen.go`). Declarations: shell and PowerShell
  functions (dashes included) and PowerShell classes; C functions, structs,
  unions, enums, typedefs and function-like macros (a macro in a `.c` file
  is private to it); C++ classes and namespaces, methods defined in the
  class or out of line (`int A::f()` is `A::f`) and virtual methods declared
  without a body; PHP, Scala, Kotlin, Swift and Dart types (classes,
  interfaces, traits, objects, enums, protocols, mixins, extensions),
  their methods, constructors and computed properties; Ruby modules,
  classes and methods. Visibility follows the language (`static`, C++
  access sections, `private`/`protected`/`internal`, Ruby's `private`,
  Swift `public`/`open`, Dart `_names`). A name resolves to a member of the
  enclosing type (implicit `this`, not in PHP, PowerShell and shell), a type
  nested in an enclosing one, the file's own top level, what it imports
  (`#include`, `source`/`.`, `require`/`require_relative`, Dart imports and
  `as` prefixes, qualified and wildcard imports, PHP `use`), its package or
  namespace, its Swift module (the directory under `Sources/`), and last to
  the one exported declaration of that name in the repo. A member access
  resolves when the receiver's type is known: `this`/`self`/`$this`/`@x`, a
  type name (`T.f`, `T::f`, `[T]::f`), or a variable, parameter or field
  declared with a workspace type (`x: T`, `T x`, `T* x`, `x = T(...)`,
  `new T`, `T.new`, `[T]::new()`); otherwise to the only member of that
  name in the repo, if there is one. C and C++ resolve together, so a `.c`
  function is reached through its header. Overriding members get `impl`
  edges through the base types, interfaces and mixins (Ruby `include`, PHP
  `use` traits). Tests (`test/`, `tests/`, `spec/`, `Tests/`, `src/test/`,
  `*_test.*`, `*Test.*`, `*_spec.rb`, `*.Tests.ps1`), `vendor/`,
  `third_party/` and build output (`build/` except for scripts, `target/`,
  `.build/`, `Pods/`, `.dart_tool/`) are left out; `*.g.dart`,
  `*.freezed.dart` and `DO NOT EDIT` files are tagged generated.
- **Virtual consumers** stand in for callers outside the workspace (public API
  clients, partner platforms). They get a share of PageRank's teleport mass and
  point at the operations they use.

PageRank runs four times: on symbols, and on the graph collapsed to files,
directories and repos. Each record reports:

| field | meaning |
| --- | --- |
| `rank` | PageRank percentile among records of the same level, workspace-wide. 0 = nothing depends on it. |
| `rank_in_repo` | the same percentile within the repo |
| `pr` | raw PageRank × node count; 1.0 is the average node of that level |
| `callers`, `caller_files`, `caller_repos` | direct users, skipping through interface/operation hops |
| `dep_syms`, `dep_files`, `dep_repos` | everything that can transitively reach this code |
| `top_callers` | the five highest-ranked direct callers (files, for file records) |

Files with no code graph (SQL, Helm, CRDs) inherit the rank of their nearest
ranked directory (`rank_source: inherited:<dir>`). Directories with no code of
their own take the max of their children (`rank_source: children`).

### Rollback (how hard it is to undo)

`rollback` is 0-100: 10 means a redeploy undoes the change, 95 means data or
contract damage survives the rollback. It is the strongest tag's score, plus
`multi_tag_bonus` for each additional tag at 40 or above. Tags come from:

- **Rules** in [`default.yaml`](indexer/default.yaml). They match repo, category from
  `repos.yaml`, path globs, and for symbols the kind, extraction facts, a symbol
  regex or exported-ness. Examples: `db-migration` (95), `parquet-model` (90),
  `crd-schema` (90), `public-api` (85), `customer-deployed` (60, every agent
  repo), `user-api` (55), `internal-api` (50), `generated-client` (45).
- **Extraction facts**: struct tags (`parquet:`, `ch:`, `db:`), kubebuilder and
  code-generator markers (every exported type in a CRD API package is
  `crd-type`), and generated-file headers.
- **Sinks**: calls whose effects outlive the release (Postgres writes through
  sqlc, ClickHouse inserts, queue publishes, S3 writes, Kubernetes mutations
  from agents, Slack/Jira sends). The sink method and its direct callers get the
  full score. Callers up to `max_hops` away lose `decay` × score per hop,
  and `via` records the distance.
- **Contract propagation**: handlers that implement an operation inherit
  `serves:<api tier>`.

File rollback is the max over its symbols and path rules. Directory and repo
rollback is the 75th percentile of their files, so one migration does not make
a whole service look irreversible.

### Impact

```
impact = rank_weight * rank + rollback_weight * rollback     (0.55 / 0.45)
impact = max(impact, impact_floor of matching rules)         migrations, CRDs, public API
impact = min(impact, impact_cap of matching rules)           tests, docs, stories
```

Levels: `critical` ≥ 75, `high` ≥ 55, `medium` ≥ 35, else `low`. All weights,
floors, caps and levels are in the scoring config.

### History (git)

Extraction reads each repo's last 730 days of commits with
`git log --name-status --no-renames` (commit trees only, so blobless clones
are fine; a moved file starts a new history) and caches them with the graph.
Re-extraction reads only the commits since the cached HEAD when that commit
is still an ancestor, and everything after a rebase or reset.
Scoring uses the `history:` window (365 days by default), counted back from
the indexed commit:

| field | meaning |
| --- | --- |
| `hist.commits` | commits touching the file, not counting bulk commits (more than `bulk_files` files) |
| `hist.fixes`, `hist.reverts` | commits whose subject matches `history.fix` (and not `not_fix`) / `history.revert` |
| `hist.recent`, `hist.recent_fixes` | the same, each commit weighted 0.5^(age / `half_life_days`) |
| `hist.authors` | distinct author emails |
| `hist.age_days` | days from the last change to the indexed commit |
| `cochange` | files that changed with this one in ≥ `min_commits` commits and ≥ `min_conf` of its commits; manifests, docs, CI, generated code and mocks are left out |

Directory and repo `hist` sums their files (authors are counted once).

### Complexity

`cx.cyclo` is cyclomatic complexity (1 + if, for, case, &&, ||, and for TS,
Java and C# also while, catch and ternaries, ?? for TS and C#, foreach and
switch-expression arms for C#; for Python if, elif, for, while, except,
and, or and match cases; for Rust if, while, for, &&, || and match arms
other than `_`; for the generic-parser languages each language's branch
keywords, `&&`/`||` (`-and`/`-or`, `and`/`or`), ternaries, `??`/`?:`,
catch/rescue and case, `when` and `match` arms other than the catch-all) and
`cx.nest` the deepest control-flow nesting (an else-if stays at its if's
depth), measured by `codemap/cx`: on the go/ast tree for Go, on the
tree-sitter trees for Java, Python, C#, Rust and the generic-parser
languages, on the lexer's tokens for TypeScript. Symbols carry their own; files and directories carry
their worst function (`cyclo`, `nest`) plus `total` and `funcs`.

### Likelihood

```
likelihood = min(100, Σ min(max, max(0, value - from) * per))
```

over the `likelihood:` point rules in the scoring config. Map records score their
history (fixes, reverts, churn, authors) and complexity. A symbol uses its
file's history and its own complexity. A directory's likelihood is its
75th-percentile file, like rollback. Generated code scores 0: it is
regenerated, not edited, so its defects live in the source. pr-triage adds the
change rules (complexity added, author experience, missing tests and
partners, PR spread, fix PRs) for each PR unit.

## Output format

```
<map dir>/
  meta.json         indexed commit per repo, weights, levels, path rules, rule reasons, stats
  repos.jsonl       one record per repo
  <repo>.jsonl      dir, file and symbol records, sorted by path then line
```

IDs are hierarchical, so a plain `grep` works:

```
settings                                               repo
settings/api/v1/user/                                  dir
settings/api/v1/user/server.go                         file
settings/api/v1/user/server.go:(*Server).GetProfiles   symbol
settings/api/v1/user/api.yaml:GetProfiles              API operation
```

```sh
grep '"id":"settings/api/v1/user/server.go' .cache/map/settings.jsonl
```

Symbol names use the `pr-triage` unit format: `Func`, `(*T).M`, `type T`,
`var X`, `const X`. TS symbols use the declared name and
`Class.member` (a constructor is `Class.constructor`; overloads and a
getter/setter pair share one), Java symbols
`Type`, `Outer.Inner` and `Type.method` (overloads share one), Python symbols
`func`, `Class`, `Class.method` and `VAR`, C# symbols `Type`, `Outer.Inner`
and `Type.Member` (overloads share one; indexers are `Type.this[]`), Rust
symbols `item`, `Type::fn`, `Trait::fn` and `inner::item` for inline
modules (functions of two impls of one type share one), generic-parser
symbols `func`, `Type`, `Outer.Inner` and `Type.method` (`::` for C++ types
and members, PHP members and Ruby modules: `Shop::Store::Repo.save`;
overloads and reopened types share one), API operations
their `operationId`. Names are the key. `lines` are 1-based at the indexed commit and
only for display: they drift as soon as a repo moves past the indexed commit.

A symbol gets its own record when another file uses it, when it carries a
rollback tag of 60 or more, or when it is an API operation. Everything else
resolves to its file. This keeps the map at about 18k symbols out of 36k nodes.

## Lookups

`lookup` returns the most specific match (`basis`), the ancestors (`chain`),
and an assessment (`impact`, `impact_level`, `rollback`, `path_rules`, `notes`).
Records also carry `likelihood`, `hist`, `cx` and `cochange`.

1. Symbol by name when one is given (receiver `*` is ignored).
2. Otherwise symbols whose stored line range overlaps the hunk; the highest
   impact first. This is only right while the file matches the indexed
   commit. pr-triage never uses it when it has the PR's base revision: it
   parses the base file with `codemap/decls` (the indexer's own TS, Java,
   Python, C#, Rust, generic-parser and OpenAPI parsers, plus `go/parser`)
   and queries by name. Declarations nest; a line belongs to the innermost
   one (an `impl` block's own lines to the type). A TS member without a
   record of its own resolves to its class.
3. The file record.
4. Directories up to the repo root, then the repo.

When the file is not in the map (a new file, a test, an asset), the estimate is
half the ancestor's rank (new code has no dependents yet) plus the stronger of
the ancestor's rollback and the path rules stored in `meta.json`. Test and doc
rules cap it.

Diff mode matches hunks on the old (base) side, which is what the map
indexed. Paths in the diff are repo-relative (`-repo` names the repo) or
workspace-relative (`code/<repo>/...`).

Go code uses this package (`github.com/amitbet/pr-triage/codemap`,
standard library only); `triage/impact.go` is the pr-triage integration:

```go
m, _ := codemap.Open(".cache/map")
res := m.Lookup(codemap.Query{Repo: "settings", Path: "api/v1/user/server.go", Sym: "(*Server).GetProfiles"})
rep := m.LookupDiff("settings", hunks) // hunks from codemap.ParseDiff
```

## Extending

- **New repo**: add it to the workspace `repos.yaml` and `code/`; `make codemap` picks it up.
  Repos under `code/` that are missing from `repos.yaml` are indexed with
  category `unlisted`.
- **New rule, sink, floor or cap**: copy `indexer/default.yaml`, edit it, run
  `make codemap-rank CODEMAP_CONFIG=<file>`.
  Sink patterns match callee keys such as
  `go:github.com/aws/aws-sdk-go-v2/service/s3:(*Client).PutObject`; to find the
  exact spelling, grep the cached graphs in `.cache/graphs/` (gzip).
- **Generated clients the heuristics miss**: add a `contracts.generated_clients`
  entry (an orval-generated TS client, for example).
- **New language or file kind**: write an `extractX(repo, root, tracked, g)`
  that appends nodes, edges and files to the `Graph`, and call it from
  `extractRepo` in `codemap/indexer/discover.go`. Ranking and output need no changes.
- **Denser symbol coverage**: lower `symbols.min_external_callers` or set
  `emit_exported: true`.

Extraction output is cached per repo in `.cache/codemap/graphs/` and keyed by
HEAD, working-tree changes, `extractorVersion`, the Go toolchain and the
versions of the indexer's dependencies (a gotreesitter upgrade re-extracts;
rebuilding the indexer does not, so bump `extractorVersion` in
`codemap/indexer/graph.go` whenever an extractor's or parser's output
changes). Ranking always runs over all cached graphs, so re-extracting one
repo refreshes every score.

Re-extracting a repo is incremental where the result can't differ from a
full run. Java, Python, C#, Rust and generic-parser parses are kept per file in
`.cache/codemap/parse/<repo>/<lang>.gob`, keyed by path and content, so only
changed files are re-parsed (decoding a parse is about 14x faster than
parsing; the cache is about 2.5x the source size). History appends the
commits since the last extraction. Name resolution, edges and Go
type-checking always cover the whole repo, and TS/JS is not cached (its lexer
is faster than decoding a cache). Forcing a repo (`MAP_REPO`, `FORCE=1`)
skips both and rebuilds the caches. On `codex-rs` (1,100 Rust files) a
re-extraction with nothing changed takes 0.4s against 0.9s cold.

## Known gaps

- RabbitMQ publishers are not linked to consumers by queue name, and ClickHouse
  tables are not linked across services. Publishing and writing are still
  tagged as sinks.
- TS URLs built by string concatenation (`BASE + '/x'`) or passed through
  function parameters are not matched. The last build matched 265 of 285 URL
  literals.
- Java, Python and C# calls on values whose type isn't declared nearby
  (`var x = make()`, return values, untyped parameters) are not linked, and
  neither are calls into libraries outside the workspace. Java and C#
  overloads share one node. C# extension methods link to every visible
  extension of that name, whatever the receiver.
- Rust calls through closures, iterator chains (`v.iter().map(..)`), trait
  methods resolved by inference, and `let` bindings from method calls are not
  linked. Macro bodies are scanned as tokens, so a macro's arguments link,
  its expansion does not. The module tree comes from the file layout, so
  modules gated by `#[cfg]` are all indexed.
- The generic-parser languages resolve by name and nearby declarations
  only: no build files (`CMakeLists.txt`, `composer.json` autoload, Gradle
  source sets, SwiftPM targets beyond `Sources/<Module>/`), no type
  inference, no macro expansion. Calls on untyped values (Ruby, PHP and
  shell locals, `auto`, results of calls) link only when one member in the
  repo has that name. Kotlin and Scala companion members merge into their
  class; Swift and Dart extensions into the extended type when it is in
  the repo. gotreesitter gives up on some whole C++ files (macro-heavy
  code); those are parsed again a top-level declaration at a time, which
  keeps the functions but can lose which class an in-class method belongs
  to. Error recovery for the C and C++ grammars is turned on
  (`GOT_C_RECOVERY=c,cpp`, unless set), which parses most of them whole.
- Go files with platform-only cgo dependencies (e.g. linux-only) do not
  type-check on other hosts; those files get partial reference information.
- Stored line ranges drift as code changes. Query by name (pr-triage resolves
  names on the PR's base revision). Rebuild the map when main moves so new
  symbols get records.
