# Contributing to penates

The developer handbook for this repository. `README.md` documents the tool for its users;
`docs/concept.md` specifies the product, and `docs/architecture.md` records the stack and
the modules.

## Quick reference

| Target | What it does | When to run it |
|---|---|---|
| `make build` | `go vet`, `go test`, then builds the binary into the repository root | While working on the code |
| `make lint` | `go mod tidy -diff`, which fails if `go.mod` or `go.sum` drifted from the imports, and `golangci-lint run` | Before committing |
| `make test` | Measures coverage of `internal/...` and fails below 70% | While working on tests |
| `make licenses` | `go-licenses check` for each of the six release platforms (`tools/check-licenses.go`), which fails if a dependency linked into the binary is under a license other than MIT, BSD-3-Clause, or Apache-2.0 | Before committing a dependency change |
| `make check` | `build`, `lint`, `test`, and `licenses` | Before pushing — CI runs exactly this |
| `make clean` | Removes the binary, `coverage.out`, `dist/`, and the license sets a snapshot generates, through `git clean`, touching nothing else | Any time |

`make check` is the whole gate: `.github/workflows/ci.yml` installs the linter, runs
`make check`, and does nothing else, so a green check locally is a green CI by
construction.

A single test:

```
go test -run TestName ./internal/tui
```

Cross-compilation:

```
GOOS=windows GOARCH=amd64 go build ./cmd/penates
```

## Setup

- **Go 1.27 or newer** — matches the `go` directive in `go.mod` and the version both
  workflows install through `go-version-file`.
- **[`golangci-lint`](https://golangci-lint.run) v2.13 or newer**, for `make lint` — the
  version CI pins. The linter type-checks the standard library with the `go/types`
  compiled into it, so build it with a Go no older than the module's `go` directive;
  `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13` does that
  with the Go you have.

Every other target needs Go alone; `make licenses` installs go-licenses into a temporary
directory itself. [`goreleaser`](https://goreleaser.com) v2 is needed only to preview a
release locally (see "Releasing"); the release installs it in the workflow. The preview
installs go-licenses with `go install` and runs it by name, so Go's install directory —
`$(go env GOBIN)`, or `$(go env GOPATH)/bin` when that is unset — must be on `PATH`.

## Code rules

- **Lint.** `.golangci.yml` lists the enabled linters, their settings, and the reason for
  each. Suppress per site, with a reason: `//nolint:<linter> // <reason>`. Turning a
  linter off repository-wide is a change to `.golangci.yml` and needs its reason recorded
  there.
- **Coverage.** `make test` measures `internal/...` and fails below 70%
  (`tools/check-coverage.go`). `cmd/penates` stays out of the profile: the threshold
  guards the packages that carry the product behaviour, and the command tree is pinned by
  its own tests.
- **Doc comments on exported identifiers**, methods on unexported receivers included —
  such methods are the bulk of this codebase.
- **No recipe may assume a Unix shell.** File removal goes through `git clean`, and the
  checks that would otherwise need a shell live in Go programs under `tools/`, tagged
  `//go:build ignore` so that `go build ./...`, `go vet ./...`, and `go test ./...` do
  not see them.
- **Error text.** Errors that reach the terminal print on stderr as `Error: message` —
  no category — and the message names the concrete next step, in parentheses where it
  fits: `unknown command "foo" for "penates" (run "penates --help" for the available
  commands)`. Categories arrive with a real command-line interface, if one ever does;
  until then none are invented. The TUI keeps its own human wording and takes no part in
  this.

## Before changing behaviour

Read `docs/concept.md` before any user-facing change, and `docs/architecture.md` before
adding or swapping a dependency. The repository rules hold alongside them — the
public-repository policy, the documentation duty, the architecture rules, and the commit
style — and live in `AGENTS.md`, not here.

**Dependencies.** The stack and its rationale are recorded in `docs/architecture.md`; a
new dependency is a decision recorded there, not a `go get`. A dependency that lands
must pass `make licenses` on all six platforms; a package whose license go-licenses
cannot detect is a decision to raise, and the exception lives in
`tools/check-licenses.go` next to the allowed list.

## Project documents

Each document has one job, and new information goes to the document whose job it is.

| Document | What it records | When it changes |
|---|---|---|
| `docs/concept.md` | The product behaviour: the kit format, ownership and conflict rules, updates, removal, the interface | A product decision changes — not when a feature is added |
| `docs/architecture.md` | Technology stack, architecture, and project structure, with rationale | The stack, the modules, their interactions, or the top-level structure change |
| `docs/tui-screens.md` | The screen specification the TUI implements | A screen changes |
| `README.md` | What penates does and how to use it | Any user-visible behaviour changes |
| `CHANGELOG.md` | User-visible changes, per release | Any user-visible change |
| `CONTRIBUTING.md` | How the project is built, checked, released, and worked on | The build, the rules, or the process change |
| `docs/backlog.md` | Accepted work not started yet, in priority order | Work is accepted, started, or dropped |
| `docs/YYYY-MM-DD-tasks-<slug>.md` | The plan and progress of one unit of work | The work progresses; archived to `docs/archive/` when done |

## Commits

`AGENTS.md` holds the commit style: an imperative subject line in English, one logical
change per commit. The one process exception is the Release workflow, which pushes the
release commit and the tag to `main` directly (`.github/workflows/release.yml`).

## Releasing

### Where the version lives

In two places, neither of them a source file:

- **the git tag** `vX.Y.Z` — goreleaser takes `.Version` as the tag without the leading
  `v`;
- **the section heading** `## [X.Y.Z] - YYYY-MM-DD` in `CHANGELOG.md`, from which
  `tools/release-notes.go` extracts the release notes.

Both are written by `.github/workflows/release.yml` from the version typed into it, so
they cannot disagree. Nothing else carries a version number: `version` and `commit` in
`cmd/penates/main.go` are targets for `-ldflags -X` and are never edited by hand, and a
binary built without them reports `penates dev (unknown)`.

### Choosing the version

penates follows [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html). While
the major version is 0, the usual convention for `0.y.z` applies:

- a release that breaks existing usage raises the minor version and resets the patch
  version — `0.1.0` after `0.0.6`. Breaking usage means removing or renaming a command,
  or refusing input that the state or kit format used to accept. An entry under
  `Removed` in `[Unreleased]`, or an entry under `Changed` that does one of these, makes
  the release breaking. An entry under `Deprecated` does not: what it names still
  works;
- any other release — new features, deprecations, and fixes only — raises the patch
  version.

Version `1.0.0` is a separate, explicit decision. From then on, a breaking release
raises the major version.

### Steps

1. Everything going into the release is merged into `main` and recorded under
   `## [Unreleased]` in `CHANGELOG.md`.
2. `README.md` still describes the tool as it now is: the running instructions and the
   `version` and `licenses` commands. Nothing verifies this.
3. **Actions → Release → Run workflow**, from `main`, with the version chosen as in
   "Choosing the version" — `0.1.0`, without the leading `v`.
4. When the job is green, open the releases page, read the notes, confirm the seven
   assets are there, and press **Publish**. Nothing is public until then.

There is no other entry point: the workflow runs on `workflow_dispatch` alone, and
pushing a tag by hand does nothing.

What the job does, in order: `make check` on the commit it is about to tag; then
`tools/release-section.go`, which inserts `## [X.Y.Z] - YYYY-MM-DD` directly below
`## [Unreleased]`, so that the unreleased entries become the new version and
`[Unreleased]` is left empty; then `tools/release-notes.go`, which reads that section
back from the rewritten changelog into a file outside the repository and prints it to
the log; then `goreleaser build --snapshot`, which builds the six binaries and publishes
nothing, and a check that each platform's license set was generated and that the
linux/amd64 binary prints it; then the commit `Release X.Y.Z`, the tag, and the push;
then goreleaser, run directly by its action rather than through the `Makefile`. Both
goreleaser runs first install go-licenses v2.0.1, which they find because
`actions/setup-go` puts `$(go env GOPATH)/bin` on `PATH`; each build then saves the
licenses of its target's dependencies into
`internal/licenses/embed/<os>-<arch>/third-party/`, which the binary embeds and
`penates licenses` prints. The result is six archives — linux, windows, darwin × amd64,
arm64 — each unpacking into a directory of its own name and holding the binary and
`LICENSE`, plus `checksums.txt`, attached to a draft release titled `penates vX.Y.Z`.
Its body is exactly the extracted section, passed as `--release-notes`: the
configuration defines no `release.footer`, and `--release-notes` turns off goreleaser's
own changelog generated from the commit log. What goes into each archive is also
`.goreleaser.yaml`; that the release is a draft is `draft: true` there.

The body of the next release is the `[Unreleased]` section as it stands;
`go run tools/release-notes.go Unreleased` prints it. To see what the archives would
contain without making a release, run `goreleaser release --snapshot --clean`, which
builds them into `dist/` and publishes nothing. It also leaves the generated license
sets in `internal/licenses/embed/`, which git ignores and `make clean` removes. A
snapshot renders no release body: goreleaser skips the changelog step entirely for it.

### When it goes wrong

Everything that can be checked is checked before the push, and a failure there leaves
the repository exactly as it was — fix the problem and run the workflow again:

- `make check` is red on the commit being released;
- the version is not of the form `X.Y.Z`, or the changelog already has a section for it;
- `[Unreleased]` has no entries, so there is nothing to release;
- the snapshot build fails, or a binary would ship without its third-party licenses.

After the push the failure to expect is a release body that is wrong rather than
unusable — the wrong entries under `[Unreleased]`, say. The release is a draft, so
delete it on GitHub, delete the tag, and revert the release commit:

```
git push origin --delete vX.Y.Z
git tag -d vX.Y.Z
```

The revert restores `[Unreleased]`, so the workflow can be run again with the same
version. A release that has already been published is not withdrawn this way — issue
the next patch version instead.
