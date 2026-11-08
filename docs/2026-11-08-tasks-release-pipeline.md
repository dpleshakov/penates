# 2026-11-08-tasks-release-pipeline.md

**Status:** Active

## Context

The build and release scheme of tfscli (../../tfscli) is taken whole: the check
half (Makefile, check-coverage, ci.yml) was already ported in 3be6ec8, the
release half was not, and nothing technical blocks it. tfscli remains the
reference for every piece: its files are adapted, not reinvented. Decisions
spanning the tasks:

- The distribution channel is GitHub Releases; this resolves open question 3 of
  concept.md §12 (closed in TASK-07).
- Six release platforms: linux, windows, darwin × amd64, arm64.
- Release archives carry LICENSE only: no skills/ directory, no release footer.
- cobra owns the command line; the first argument is always a command. No
  positional kit path: the first-run screen already covers registering a kit.
- `penates version` and `penates licenses` are subcommands, not flags. Version
  output is `penates X.Y.Z (commit)`; a plain `go build` reports
  `penates dev (unknown)`.
- Stderr errors print as `Error: message`, with no category: the tfscli
  category contract is deliberately not taken. The message doctrine is taken:
  every error names the concrete next step, in parentheses where it fits. The
  TUI keeps its own human wording and takes no part of this.
- The first release number is chosen when the first release is cut.
- Documentation and CHANGELOG ride in the same commits as the behaviour they
  describe (repository rule).
- If `make licenses` ever finds a dependency outside MIT, BSD-3-Clause, or
  Apache-2.0, that is a decision to raise, not a list to edit.

---

### TASK-01 `cobra-command-surface`
**Description:** Make cobra the root of the command line in cmd/penates: no
arguments open the TUI (as now, minus the kit path); `penates version` prints
the stamped version; any other first argument is an unknown-command error —
one stderr line in the `Error: message` form that names the next step, exit
1 — and extra arguments to a known command are errors too. Remove the
positional kit path argument and everything that passes it down; tests that
start the TUI with a kit path adapt to the first-run flow. Cobra's own
`--help` and completion stay as they come. In the same commit:
architecture.md records cobra as a dependency with its reason; concept.md
rewords the "command-line interface" non-goal (a full CLI remains outside v1,
but the binary takes the version and licenses commands) and rewrites the
start-with-a-kit-path scenario; README describes starting without arguments;
CHANGELOG records the `penates version` command under Added and starting with
a kit path under Removed. Starting this task takes the `release-pipeline`
entry out of docs/backlog.md, per the backlog rules.
**Definition of done:** `go run ./cmd/penates version` prints
`penates dev (unknown)`; `go run ./cmd/penates foo` exits 1 with one stderr
line in the `Error:` form naming what to do; `go run ./cmd/penates` opens the
TUI; no code reads a kit path from the arguments; make check green; the four
documents ride in the same commit.
**Status:** Done

### TASK-02 `licenses-command`
**Description:** Port internal/licenses from tfscli (licenses.go, doc.go, and
the tests, adapted): the own license embedded from LICENSE as penates.LICENSE,
the Go distribution license as go.LICENSE, per-platform embed files for the
six release platforms plus the other-platform fallback, each embed/
directory committed with its PLACEHOLDER alone. Text() prints the components
and, for a plain build, the notice that the third-party licenses are embedded
in release builds only, pointing at
https://github.com/dpleshakov/penates/releases. The `penates licenses`
subcommand prints Text() to stdout. CHANGELOG records the command under
Added, in the same commit.
**Definition of done:** `go run ./cmd/penates licenses` on a plain build
lists penates and the Go standard library and runtime, then the notice with
the penates releases URL; make check green.
**Status:** Done

### TASK-03 `license-check`
**Description:** Port tools/check-licenses.go with ./cmd/penates, the same
six platforms, and the same allowed list (MIT, BSD-3-Clause, Apache-2.0); the
pinned go-licenses version is shared with .goreleaser.yaml and the two change
together. github.com/mattn/go-localereader (windows-only, indirect through
bubbletea) is MIT by its README alone — the v0.0.1 module ships no LICENSE
file, so go-licenses cannot detect it; the check passes it through --ignore
with that justification recorded in the code. Makefile: a `licenses` target,
`check: build lint test licenses`, and `clean` extended to dist/ and the
generated license sets. .gitignore gains
`/internal/licenses/embed/*/third-party/`.
**Definition of done:** from a clean checkout `make check` runs the license
check and passes on all six platforms; `make clean` after a goreleaser
snapshot removes dist/ and the generated sets, leaving the committed
PLACEHOLDERs.
**Status:** Done

### TASK-04 `release-tools`
**Description:** Copy tools/release-section.go and tools/release-notes.go from
tfscli verbatim: they are project-agnostic — the changelog path comes as an
argument — and the penates CHANGELOG already matches the format they parse.
**Definition of done:** `go run tools/release-notes.go Unreleased` prints the
current [Unreleased] section of CHANGELOG.md; on a scratch copy of the
changelog, release-section inserts `## [X.Y.Z] - YYYY-MM-DD` directly below
[Unreleased] and refuses a duplicate version, an empty [Unreleased], and a
missing [Unreleased] heading.
**Status:** Done

### TASK-05 `goreleaser-config`
**Description:** Port .goreleaser.yaml: project_name penates; one build from
./cmd/penates with CGO_ENABLED=0 and the version and commit ldflags; a
before-hook installing go-licenses (same pin as check-licenses); a per-build
pre-hook saving the target's licenses into
internal/licenses/embed/<os>-<arch>/third-party with GOOS and GOARCH set —
the hook ignores github.com/dpleshakov/penates itself and
github.com/mattn/go-localereader (MIT by README, no LICENSE file in the
module; see TASK-03);
goos linux, windows, darwin × goarch amd64, arm64; archives named
penates-{{ .Version }}-{{ .Os }}-{{ .Arch }}, tar.gz with zip for windows,
wrap_in_directory, files: LICENSE alone; checksums.txt; a draft release named
penates v{{ .Version }} with no footer.
**Definition of done:** `goreleaser build --snapshot --clean` succeeds from a
clean tree; each of the six dist binaries reports the snapshot version
through `penates version` and the linux/amd64 one lists the third-party
modules through `penates licenses`; `make clean` restores the tree.
**Status:** In progress

### TASK-06 `release-workflow`
**Description:** Port .github/workflows/release.yml: workflow_dispatch with a
version input (no leading v), concurrency group release, contents: write; a
guard refusing to run off main; checkout with fetch-depth 0; setup-go with
go-version-file: go.mod; golangci-lint-action v2.13 install-only; make check;
release-section rewriting the changelog; release-notes reading the section
into $RUNNER_TEMP and printing it; goreleaser build --snapshot --clean; the
embedded-licenses check — every internal/licenses/embed/*/third-party holds
github.com/charmbracelet/bubbletea and the linux/amd64 binary lists it
through `penates licenses`; the Release $VERSION commit, the tag, and their
push; goreleaser release --clean --release-notes. The version input reaches
steps through the environment, never through ${{ }} inside a run body.
**Definition of done:** the workflow exists with the steps in the tfscli
order under penates names; the embedded-licenses step checks bubbletea; no
run body interpolates the input directly; permissions, concurrency, and the
main-only guard match the tfscli file.
**Status:** Pending

### TASK-07 `contributing-docs`
**Description:** Create CONTRIBUTING.md on the tfscli pattern with the
penates specifics: a quick reference of the make targets; setup (Go,
golangci-lint v2 on PATH; goreleaser only to preview a release locally); code
rules (doc comments on exported identifiers, no recipe assumes a Unix shell,
checks live in Go programs under tools/ tagged //go:build ignore, coverage
threshold 70); the error-text doctrine — stderr errors print as
`Error: message` and name the concrete next step, with no categories until a
real CLI arrives; and the releasing chapters (where the version lives,
choosing the version under SemVer with the 0.y.z convention, the steps, when
it goes wrong). AGENTS.md keeps the repository rules and points to
CONTRIBUTING.md for the build and release handbook instead of duplicating it.
Close open question 3 of concept.md §12: the binary reaches users through
GitHub Releases archives for the six platforms; a CHANGELOG entry only if it
reads as user-facing.
**Definition of done:** CONTRIBUTING.md covers the targets, setup, rules,
error doctrine, and the full releasing chapter; AGENTS.md references it;
concept.md has no distribution open question; the document references agree
with each other.
**Status:** Pending
