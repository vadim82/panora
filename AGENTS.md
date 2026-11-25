# Working on Panora

Read [README.md](README.md), [CONTRIBUTING.md](CONTRIBUTING.md) and the owning
reference before changing behavior. [Architecture](docs/architecture.md)
defines package boundaries. [Status](docs/status.md) tracks supported scope
and platform coverage.

## Runtime invariants

- Keep provider discovery, playback and desktop rendering separate. Providers
  find images. Playback owns selection, history, persistence and the only
  rotation timer. The control layer and clients use the private service API.
- Keep the CLI free of graphical toolkit dependencies. The separate `ui/` Go
  module embeds its built frontend. Keep shell controls thin and polling
  read-only for playback and service lifecycle.
- Keep generated state in the service-owned SQLite catalog and user preferences
  in versioned TOML. Use shared revision-checked preference and source
  writers. Preserve saved data on validation, concurrency or version errors.
- Commit accepted history only after backend acknowledgement. Preserve uncertain
  intent when dispatch may have occurred. Preview reads never resolve originals,
  run providers, change playback or mutate the desktop.
- Keep original images outside cache ownership. Guard cache reads and eviction
  with the shared lock and preserve current and pending selections.
- Pass contexts through blocking operations and bound subprocess input, output
  and lifetime. Use argument arrays, wrap errors with context and avoid
  exposing secrets or raw plugin traffic.

## Checks and delivery

Run `mise exec -- task fmt` and `mise exec -- task check` for Go changes.
Use the checks in [CONTRIBUTING.md](CONTRIBUTING.md) and
[Native UI development](docs/frontend.md) for desktop and integration work.
Use the [isolated installed validation guide](docs/installed-release-checklist.md#choose-an-isolated-test-environment)
before using the host desktop. Start with headless KVM for graphical workflows
and a container or signed Arch build root for package-only checks. Keep guest
input, state and sockets separate from the owner’s login. Run one live desktop
experiment at a time, with an owner-coordinated window for any required host
trial. See the XWayland and suspend rules in the contribution guide.

Update the reference that owns the changed contract:

| Change | Reference |
| --- | --- |
| User settings, defaults and paths | [Configuration](docs/configuration.md) |
| Commands and service API | [CLI](docs/cli.md), [private socket API](docs/socket-api.md) |
| Runtime ownership or storage | [Architecture](docs/architecture.md), [catalog](docs/catalog.md), [ADRs](docs/adrs/README.md) |
| Discovery and provider messages | [Providers](docs/providers/README.md), [protocol](docs/providers/protocol.md) |
| Desktop behavior and presentation | [Desktop](docs/desktop.md), [Fit](docs/fit.md), [UI](docs/ui.md) |
| Checks and current platform limits | [Verification](docs/verification.md), [status](docs/status.md) |

Use GitHub issues for work order and PRs for delivery evidence. Preserve
source revisions and measured limits. Check changed examples and links.
Merge only after the required checks and review, and clean owned temporary
branches, worktrees and trials without changing unrelated user work.
