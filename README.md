# penates

penates is a TUI tool that installs, updates, and removes agent-harness
customization — skills, instruction files, plugins, configuration fragments — from
local kit directories into a harness configuration directory such as
`~/.config/opencode`.

What it is built around:

- A kit is a plain directory bound to one harness; its unit is the **item**.
- Variation between machines is expressed by separate items and per-item variables,
  not by conditional logic.
- Everything penates writes is recorded in a lock; anything not recorded belongs to
  the user and is never overwritten or removed.
- penates performs no network access and knows no harness: destination paths come
  from kit data.

The full design is specified in [docs/rfc-penates.md](docs/rfc-penates.md), the stack
and modules in [docs/architecture.md](docs/architecture.md).

Status: early development, no releases yet.

## Development

```
go build ./...
go test ./...
golangci-lint run
```

License: [MIT](LICENSE). Releases follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
