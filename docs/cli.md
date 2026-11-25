# Command-line reference

[Install the Arch package](arch.md) for `/usr/bin/panora`, or
[set up the tools](../CONTRIBUTING.md#local-development), build with
`go build -o bin/panora ./cmd/panora`, and put the binary on PATH. From the repository root,
`go run ./cmd/panora` can be used instead. Run `panora COMMAND --help` for flags.

## Commands

| Command | Requires service | Purpose |
| --- | --- | --- |
| `serve` | Starts it | Run rotation and controls in the foreground |
| `stop` | Yes | Request graceful shutdown, leaving the last accepted wallpaper in place |
| `status`, `next`, `previous`, `pause`, `resume` | Yes | Inspect or control playback |
| `exclusions ls/add/remove` | Yes | List marks, exclude current, or remove a mark |
| `config path`, `config check` | No | Locate or validate saved settings |
| `history ls`, `history replay ID` | Yes | List accepted requests or replay an entry by its stable history ID |
| `shell snapshot` | Reads it | Read bounded status/details for a thin desktop shell client |
| `doctor` | No | Check configuration, desktop readiness and service connectivity |
| `scan DIRECTORY` | No | Inspect local image headers against saved rules |
| `debug set IMAGE` | No | Apply an image directly, outside playback history |
| `provider local`, `provider wallhaven` | No | Handle internal provider requests on stdin/stdout |
| `provider install DIRECTORY` | No | Install a trusted provider program for the next service start |
| `help`, `completion` | No | Help and shell completion |

## Foreground service and controls

Run the service; it creates default settings when they are absent:

```sh
panora serve
```

On a clean installation, it waits for provider setup without changing the desktop.
Status reports `setup_required: true`; Next/Previous explain that a folder or search
must be enabled. Existing settings are never reset. An explicit `--config` path
must already exist. See [first-run defaults](configuration.md#first-run-defaults).

Use another terminal for controls:

```sh
panora status
panora status --verbose
panora status --json
panora next
panora previous
panora pause
panora resume
```

Next follows forward history before choosing a fresh image. Previous revisits
available entries that still match the rules. Pause stops automatic rotation;
manual navigation and background discovery continue. Resume and successful
manual changes start a fresh interval.

For installed service lifecycle and logs, see
[Omarchy](arch.md#install-upgrade-and-remove) and
[macOS](macos.md#background-service-controls). The installed Omarchy service uses
`--omarchy-session` to wait for the graphical socket and background handler and
to seed the active theme source when the default config is missing.
`--pause-for-setup` starts paused when no inputs are enabled. `--log-file
/absolute/private/service.log` keeps up to 1 MiB each in the current and previous
log. It requires private files and rejects symlinks.

Use `serve --paused` for manual-only playback. `--interval 30m` overrides the saved
interval at startup; an explicit API interval edit can replace that override.
[Configuration](configuration.md#startup-flags-and-persisted-settings)
explains saved settings and other startup flags. Controls accept `--socket` and
`--json`; use the same socket override as the service.

Controls require a running service. Scripts can pass `--instance ID` to
`next`, `previous`, `pause`, `resume` or `stop` to target the service observed
in `status --json`. Its `instance_id` changes on restart.
See the [guarded action API](socket-api.md#playback-and-service-lifecycle).

`panora stop` requests shutdown. With `--json`, it returns `version: 1`,
`stopping: true` and the current `instance_id`. The acknowledgement is sent
before cleanup finishes. Repeated requests are harmless while the service drains.
After it exits, controls report an unavailable service. Closing a graphical
client leaves the installed service running.

Stop, Ctrl-C and SIGTERM cancel active work, join providers and the rotation
scheduler, drain the transport and release storage/socket ownership. They leave
the last accepted wallpaper, saved settings and history in place. A canceled
in-flight desktop request retains its uncertain intent for inspection after
restart.

### Exclude and restore images

With the service running:

```sh
panora exclusions add
panora exclusions ls
panora exclusions ls --json --limit 10
panora exclusions remove 12  # Use an ID from the list.
```

`add` excludes the service's current image from future playback. Repeating it
does not create another mark. It leaves the wallpaper, navigation position,
rotation interval and original file unchanged.

Exclusions survive restart and removal of old history. Playback skips excluded
images and validated duplicates. Separate marks for identical content must be
removed individually. You can list/remove marks even when the original image
is unavailable.

Bare `exclusions` lists the first page. Results are newest-first, with 10 per page;
`--limit` accepts 1–10 and `--before ID` requests older marks. JSON returns
`entries` with stable `id`, `provider`, provider `image` ID, optional `source_id`,
title/origin page and `excluded_at`. `next_before` is omitted on the final page.
Listing does not inspect image files or establish availability.

All subcommands accept `--socket` and `--json`. Adding without a current image
returns `no_current`; resolve an uncertain wallpaper change before excluding it.
A concurrent playback action can cause `busy`. Removing an absent mark returns
`exclusion_not_found`. To reverse an exclusion, list its ID and remove it.

### Status and scripting

Actions print short confirmations. Default status is compact:

```text
Playback: paused (every 30m)
Last accepted image: /home/alex/Pictures/Wallpapers/lake.png (source: home)
```

Status shows the last wallpaper request accepted by the desktop command. It
does not detect changes made by other apps. `--verbose` adds history, the requested
desktop integration, refresh timestamps/counts and retry details. Errors, uncertain
outcomes and active retry delays remain visible in compact status. Another
playback action can return `busy`; status remains available.

With no enabled inputs, compact status says `Playback: waiting for setup`.
The additive JSON field `setup_required` is true and `next_at` is omitted.

If an action times out or loses its response, check `status` or `exclusions ls`
before retrying:
the action may already have completed. Panora does not automatically retry lost
mutation responses.

Commands offering `--json` emit one versioned result/error document on stdout,
including parser failures. For example:

```json
{"version":1,"error":{"code":"control_request_failed","message":"..."}}
```

API error codes are preserved. Parser errors use `invalid_arguments`, config
errors use `config_invalid`, and other failures use `command_failed` or
`control_request_failed`. JSON preserves data without styling or duplicate human
errors on stderr. Human results go to stdout and errors to stderr. Success exits
0; failure exits 1. Successful status may still report warnings or prior errors.

Help groups playback, service, diagnostics and advanced commands. Color is limited
to supported interactive streams; `NO_COLOR`, absent/dumb `TERM`, pipes and
redirection disable it. Human strings neutralize terminal control characters.


## Inspect images and desktop readiness

Use `config path` to locate settings, `config check` to validate the file, and
`doctor` to diagnose desktop or service problems. Save a valid config before
running `doctor` or `scan`:

```sh
panora config path
panora config check
panora doctor
panora doctor --json
panora scan /absolute/path/wallpapers
panora scan /absolute/path/wallpapers --min-width 1920 --min-height 1080
```

`config path` prints the absolute TOML path even when the file does not exist.
`config check` checks fields, types and version using the same validator as the
service; it does not test folder access, online providers or the desktop. Both accept `--config` and `--json`; path JSON contains
`version`/`path`, and successful checks add `valid: true`. Neither writes files.

`doctor` checks config, desktop readiness and private-socket connectivity. It
accepts `--config`, `--socket` and `--json`, with an eight-second overall deadline
and a two-second socket deadline. Invalid config or an unavailable/ambiguous
desktop integration fails; a stopped service produces a warning. Other checks
continue after a failure, but invalid config skips desktop probes. JSON contains
`version`, `checks`, optional `configured_backend`, `resolved_backend` and
`running_requested_backend`, and `error` on failure.

Backend selection comes from `desktop.backend`. Auto requires exactly one ready
integration; an explicit choice probes only that integration and never falls
back. Configuration API desktop edits apply to the next wallpaper action;
direct TOML edits require restarting `serve`.

Use `scan` to see why local images pass or fail your saved rules. It inherits
folder depth, minimum dimensions, ratio and pixel tolerance. Explicit flags
affect this scan only and use the saved pixel tolerance for any selected ratio.
`--config` selects a different settings file. `--ratio monitor-similar`
uses the [display’s shape with a 5% tolerance](configuration.md#match-monitor).
Output includes effective rules, PASS/REJECT reasons and inspected-header counts.
Unreadable headers produce warnings and exit 1; rule rejections alone do not.
Traversal skips symlinks beneath the root and respects cancellation, although an
individual filesystem call may still block. Scan reads headers only; it does not
fully decode images, add folders to playback or change the wallpaper.

## Apply a specified image

`debug set` applies directly without playback filters or history. It requires a
valid config and ready desktop integration:

```sh
panora debug set /absolute/path/image.jpg
panora debug set /absolute/path/image.jpg --fit contain --upscale
panora debug set /absolute/path/image.jpg --fit crop
```

Saved presentation defaults come from `desktop.fit`/`desktop.upscale`; flags override
this invocation. Contain shows the whole image; crop fills the screen. Crop and
upscaling cannot be combined. Noctalia controls its own presentation and rejects
`contain`, `crop` or enlargement overrides; explicit `auto` and `--upscale=false`
are accepted. See [Fit](fit.md) for adapter limits.

A running service can replace this image at its next change. Pause rotation to
retain it until the next manual playback action.

## Provider diagnostics

The Arch package bundles `panora provider local` and `panora provider wallhaven`
in `/usr/bin/panora`. To inspect the local provider without a desktop:

```sh
printf '%s\n' '{"jsonrpc":"2.0","id":"demo","method":"describe","params":{"protocol":2}}' \
  | panora provider local
```

Each process reads one JSON-RPC line and exits. A line may contain a batch. See
[Providers](providers/README.md#running-a-provider). Developer-only
`serve --local-plugin /absolute/plugin-directory` substitutes another local
implementation using a [plugin manifest](providers/protocol.md#plugin-manifest).

For native or shell client integration, see the [private socket API](socket-api.md).
