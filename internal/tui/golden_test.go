package tui

// Golden screens of the fixture main screen (4.1-4.3), rendered by the tests
// in layout_test.go.

var wantGoldenWide = ` penates  ~/projects/config-collection/personal-main/opencode-target ▾                                3 pending · ? help

  ▾ personal-opencode  ●2 ↑1 ✗1  ~/kits/opencode                       │done-v2 · personal-opencode
  ● writing  1.0.0                                                     │
  ↑ done-v2  opencode-v2  1.2.0 → 1.3.0                               ✓│Marks the session name with [DONE] on the /done
  ✗ git-via-gh  gh  1.2.0                                              │command.
  ○ done-v1  opencode-v1  1.0.0                                        │
  ▾ team-opencode  ○1 ◌1  ~/kits/team                                  │Status    ↑ update, 1.2.0 → 1.3.0 (minor)
  ○ review  0.3.0                                                     ✓│
  ◌ old-lint  0.2.0                                                   −│Pending   ✓ update to 1.3.0
  ▸ old-kit  ⊘ unreadable  ~/kits/old                                  │
  + add kit                                                            │Tags      opencode-v2
                                                                       │
                                                                       │Contributes
                                                                       │  file  plugin/done.ts
                                                                       │  json  opencode.json  /plugin
                                                                       │
                                                                       │Changelog
                                                                       │  1.3.0  Supports custom marker text
                                                                       │  1.2.0  Initial release for OpenCode v2
                                                                       │
                                                                       │

space unselect · d remove · enter details · / filter · a apply · ctrl+p commands                                  q quit`

var wantGoldenMedium = ` penates  ~/projects/config-collection/personal-main/opencode-target ▾  3 pending · ? help

  ▾ personal-opencode  ●2 ↑1 ✗1  ~/kits/opencode
  ● writing  1.0.0  Text editing skill
  ↑ done-v2  opencode-v2  1.2.0 → 1.3.0  Marks finished sessions                         ✓
  ✗ git-via-gh  gh  1.2.0  AGENTS.md heading Git usage exists
  ○ done-v1  opencode-v1  1.0.0  Marks finished sessions
  ▾ team-opencode  ○1 ◌1  ~/kits/team
  ○ review  0.3.0  Review agent with team checklist                                      ✓
  ◌ old-lint  0.2.0  Removed from kit                                                    −
  ▸ old-kit  ⊘ unreadable  ~/kits/old
  + add kit







space unselect · d remove · enter details · / filter · a apply                      q quit`

var wantGoldenNarrow = ` penates  …/personal-main/opencode-target ▾            3 pending

  ▾ personal-opencode  ●2 ↑1 ✗1  ~/kits/opencode
  ● writing  1.0.0
  ↑ done-v2  1.2.0 → 1.3.0                                     ✓
  ✗ git-via-gh  1.2.0
  ○ done-v1  1.0.0
  ▾ team-opencode  ○1 ◌1  ~/kits/team
  ○ review  0.3.0                                              ✓
  ◌ old-lint  0.2.0                                            −
  ▸ old-kit  ⊘ unreadable  ~/kits/old
  + add kit



space · d · enter · / · a apply                           ? help`
