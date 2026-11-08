# 2026-11-08-tasks-builtin-kit.md

**Status:** Active

## Context

penates distributes its own documentation as a kit: two agent skills — a
product reference and a kit-authoring guide — shipped inside the binary and
installed through the same machinery as any other kit. The scheme, agreed
with the peer review agent on 2026-11-08:

- The kit source lives at `internal/builtin/kit/` and is embedded with
  `//go:embed` (`internal/builtin/embed.go` beside it). The content hash is
  computed from the embedded files at runtime; no hash constant is committed,
  because a stale constant silently never replaces.
- The embedded kit reaches disk as a plain directory kit: unpacked to
  `<data dir>/builtin-kit` and registered under the reserved name `penates`,
  whenever that name is free, on every startup — no target is ever
  auto-attached. There is no second kit source kind: `internal/kit` keeps
  reading directories, the state schema stays as it is.
- A stamp file beside the kit (`<data dir>/builtin-kit.sha256`) holds the
  embedded hash, written only after the directory is fully in place (unpack
  into a temporary directory, rename, then stamp). Replacement fires when the
  embedded hash differs from the stamp; a failed replacement (a locked file)
  is a notice and a retry on the next start, never fatal.
- Local edits are protected by the three-state model the product already
  uses: stamp = base, directory hash = current, embedded = incoming. A
  user-touched directory with differing incoming content is a conflict:
  nothing is written, the kit row shows the state, and an explicit reset
  discards the local edits. Granularity is the whole directory — one local
  edit freezes every built-in update until reset — stated in concept.md as
  the price of leaving no stray directories behind.
- Edge rows of the same table: a directory present with no stamp is not
  owned — penates stands down entirely, no unpack and no registration, and
  the report names the blocking path; a directory missing under a current
  stamp is crash-corruption — treated as absent and unpacked again.
- The registry rules the replace on `registry["penates"].Dir` equal to the
  managed path: a kit repointed through Edit kit path counts as foreign, and
  the built-in stands down. Copying the directory and repointing the entry is
  the intended fork escape hatch for users who want to keep local
  modifications — stated in concept.md as behaviour, not accident.
- Items: `reference` installs `skills/penates/SKILL.md`, `kit-authoring`
  installs `skills/penates-kit-authoring/SKILL.md`; independently selectable.
  An item version bumps on every content edit in the same commit, by the
  concept.md conventions; the `metadata.version` of each SKILL.md mirrors the
  item version. The texts are hand-written, not generated from concept.md;
  `make check` validates that the embedded kit loads and its versions parse.
- Documentation rides in the same commits as the behaviour it describes
  (repository rule): concept.md (a built-in kit subsection beside 6.1, the
  6.7 data-dir contents, the 3.2 non-goal exception, 10.1/10.2 screens),
  architecture.md (the new module), CHANGELOG under Unreleased, README with
  the shipped skills.

---

### TASK-01 `builtin-kit-sync`
**Description:** New package `internal/builtin`: `embed.go` exposing the
embedded kit as an `fs.FS` over `internal/builtin/kit/` (placeholder SKILL.md
texts land here, the real ones in TASK-03), and `sync.go` with
`Sync(dataDir string, store *state.Store, src fs.FS) (Report, error)` — cmd
passes the embedded FS, tests pass fakes. Sync implements: unpack into a
temporary directory and rename, stamp written last; replace when the runtime
hash of `src` differs from the stamp; the three-state conflict (directory
hash differs from the stamp's base while incoming differs too — no write,
reported); the two edge rows (a directory without a stamp is not owned —
stand down, no unpack and no registration, report the blocking path; a
missing directory under a current stamp is unpacked again); registration
under `penates` on every startup while the name is free — no target
attached; no write and a report when the name is held by a foreign directory
or the registered `penates` points elsewhere than the managed path; a
failed replace is reported, never fatal. The guard lives in the
user-facing registration path: `state.Store.RegisterKit` refuses the
reserved name `penates` with an error naming the built-in kit, and
`internal/builtin` registers through a dedicated state operation that is
the only other writer of the name. `cmd/penates/cli.go` runs Sync after
`state.Open` and passes the report into the interface. In the same commit:
concept.md gains the built-in kit subsection — the sync model with the
stamp, the replace, the three-state conflict, and the reset semantics
(discard the local edits of the managed directory, take the embedded
content) — the 6.7 data-dir contents (the kit directory and its stamp), and
the 3.2 exception (obtaining
kits is the owner's concern — the one kit penates delivers itself);
architecture.md records `internal/builtin` and its rules (no harness
knowledge: target_hint and install paths come from kit data); CHANGELOG
under Unreleased.
**Definition of done:** a fresh data dir unpacks, stamps, and registers the
kit; a second run changes nothing; changed embedded content replaces the
directory and re-stamps; a user-edited directory with changed incoming
content is left untouched and reported as conflict; a missing directory under
a current stamp is unpacked again; a directory with no stamp is never
touched, nothing is unpacked or registered, and the report names the
blocking path; a foreign kit named `penates` suppresses registration with a
report; a repointed `penates` entry suppresses every write; registering a
directory kit as `penates` fails; make check green.
**Status:** Pending

### TASK-02 `builtin-kit-ux`
**Description:** The interface half. The startup report reaches the model and
its notices render on the notice line. The empty state tells the truth: with
the built-in kit registered and nothing attached, the first-run body names it
and offers attaching it (the attach offer reuses the add-kit target
proposal — the built-in kit's `target_hint` is confirmed or edited as usual);
`firstRun` no longer claims "No kits are registered yet." while a registered
kit renders nowhere. The command palette gains "Attach kit" over the
registered kits not attached to the current target. A kit row gains a
"modified locally" status beside unreadable and detaching, driven by the
startup conflict report, and a "Reset builtin kit" command whose confirmation
names exactly what it discards: the local edits to the managed directory,
replaced by the embedded content. In the same commit: concept.md 10.1 (the
first-run screen with the built-in offer) and 10.2 (the kit-row status and
the reset), and 7.6 gains a one-line pointer disambiguating the two resets
(it resets an installed item; the built-in kit subsection resets the kit
directory), CHANGELOG under Unreleased.
**Definition of done:** on a fresh data dir the first-run screen offers the
built-in kit and attaching it confirms the target_hint; "Attach kit" lists
the registered unattached kits and attaches to the current target; a
conflicting managed directory shows on its kit row; reset, after its
confirmation, restores the embedded content and re-stamps; startup notices
render; make check green.
**Status:** Pending

### TASK-03 `skill-texts`
**Description:** Replace the placeholder texts with the real skills.
`items/reference/`: the product reference — what penates is, the terminology
(kit, item, contribution, shared file, target, lock, tag, variable), the
ownership model and the update/removal tables, the item states and their
glyphs, the common errors (heading exists, file not owned, kit unreadable,
orphan), where the state lives, the TUI keys, the `version` and `licenses`
commands. `items/kit-authoring/`: creating and maintaining a kit repository —
the directory skeleton, `kit.yaml` and `item.yaml` schemas, the three
contribution kinds with examples (including the `<!-- penates:begin/end -->`
markers and the JSON pointer rules), variables with `template: true`, tags
and variants, the versioning conventions and `CHANGELOG.md`, how conflicts
and user modifications play out for the author. Both SKILL.md files carry the
frontmatter the skill format expects, with `metadata` naming the author and a
version mirroring the item version (0.1.0 to start). `kit.yaml`: name
`penates`, description, `target_hint: ~/.config/opencode`, no shared files.
`make check` gains the validation: `tools/check-builtin-kit.go` runs
`kit.Read` over `internal/builtin/kit` and fails on any reported error and on
a non-semver item version. README gains a line that the binary ships its own
documentation skills. CHANGELOG under Unreleased in the same commit.
**Definition of done:** the embedded kit loads clean through `kit.Read`; both
skills install as items into a scratch target and land in
`skills/penates/` and `skills/penates-kit-authoring/`; `make check` runs the
builtin-kit validation; README and CHANGELOG updated in the same commit.
**Status:** Pending
