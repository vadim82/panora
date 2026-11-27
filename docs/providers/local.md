# Local files

The bundled `panora provider local` finds regular JPEG, PNG, and static WebP files. The service
starts it for background discovery and selected-image resolution.

## Settings

Add these tables to the [configuration](../configuration.md#user-configuration):

```toml
[[providers]]
id = "local"
enabled = true

[[providers.folders]]
id = "home"
path = "/home/alex/Pictures/Wallpapers"

[[providers.folders]]
id = "work"
path = "/home/alex/Wallpapers"
```

Folder IDs must be unique and stable for history. Paths must be absolute, with
no `~` or environment expansion. A root may be a symlink; links beneath it are
skipped. Per-folder `max_depth` overrides `filters.max_depth`, with root files at
depth 0. An omitted folder `enabled` defaults to true; the provider gate also
applies. Optional `name` changes only the display label. See
[folder management](../configuration.md#local-folder-management) for edits and inheritance.

A configured folder that disappears or cannot be accessed reports `unavailable`.
A missing mount, a changed symlink or inaccessible parent can all prevent the
same saved path from being opened. The next automatic check uses [bounded
backoff](../architecture.md#provider-retry-policy), including while playback is
paused. Restoring access at the same path lets discovery resume on its own.
Malformed settings or a root that resolves to an existing non-directory report
`invalid_config`. Validated cached content remains usable during a folder outage.

## Discovery and selection

Discovery returns paths in pages without decoding images or applying rules. Its
cursor holds a traversal offset, a digest of the last returned path and a
settings fingerprint. Each page walks the folder again and checks that the
offset still identifies the same path. A missing or shifted path invalidates
the cursor and restarts the scan. Settings changes also invalidate cursors.
The previous complete scan remains available until the retry completes.

Background jobs commit pages and checkpoints to SQLite. Interrupted discovery
resumes its committed cursor; completed scans restart at the next refresh.
Playback reads committed candidates, checks headers and image rules, then
resolves and rechecks the selected original before copying it to the shared cache.
See [discovery scheduling](../architecture.md#background-discovery) for current
budgets and [selection](../architecture.md#what-happens-when-you-run-next) for fairness.
Filesystem watching is not implemented.

The service API can edit, enable, disable and remove folders at runtime.
Removing a folder preserves originals and history; navigation skips entries
outside enabled inputs or their original root. Root edits keep the folder ID
but separate image identity/cache associations for the old and new roots.

## Read-only diagnostic

With Go installed, run from the repository root:

```sh
printf '%s\n' '{"jsonrpc":"2.0","id":"demo","method":"describe","params":{"protocol":2}}' \
  | go run ./cmd/panora provider local
```

This describes capabilities without scanning or changing the wallpaper. See the
[discovery exchange](protocol.md#local-image-example) and
[implementation](../../internal/sources/local/local.go).
