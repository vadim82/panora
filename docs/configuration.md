# Configure Panora

Use [Sources and Settings](ui.md) for ordinary setup and editing. This reference
covers saved TOML, defaults, and which changes take effect while Panora runs.
`panora config path` locates the file. `panora config check` validates it without
creating or changing anything.

## First-run defaults

On first launch Panora creates version-1 TOML when the default file is absent.
The installed Omarchy service enables the active **Omarchy theme** source with
shared image rules and starts rotation after the native intro finishes and
a still is ready. Other first launches start without wallpaper sources. Choose
a local folder from the welcome screen or add a folder in Sources. Explicitly
save an enabled Wallhaven search in Sources to opt in to network discovery.
Defaults include automatic desktop and Fit selection, upscaling off, a
30-minute interval, 20 cached images or 512 MiB, a folder depth of 2, and
image rules of 1×1 / Any.
The full [field table](#user-configuration) covers omitted values.

With no enabled inputs, the service reports `setup_required` and waits without
applying a wallpaper. Existing settings, even invalid or unsupported ones,
are preserved. An explicit `--config` path must already exist.

## Installed providers

Install a trusted local provider bundle with `panora provider install DIRECTORY`
and restart the service. Panora loads installed programs from the
`panora/providers/` subdirectory of the OS user config directory. The
[provider editor contract](providers/editor-manifest.md) describes the bundle.

Inputs added in Sources are stored under the provider in
`[[providers.inputs]]`. The host saves each input's stable `id`, optional
`name` and `enabled`, provider settings in the `settings_json` string, and
optional `image_rules`. Keep provider input settings in Sources to use its
validator. Removing a configured input preserves the installed provider,
originals, and history. A missing provider leaves its configured inputs saved.

## User configuration

Adjust the folder paths, minimum dimensions and aspect ratio for your setup.
The example uses 5120×1440 images at 32:9; TOML comments explain optional fields:

```toml
version = 1

[playback]
interval = "30m"
rotation_mode = "balance_sources"

[desktop]
backend = "auto"
fit = "auto"
upscale = false

[cache]
max_images = 20
max_bytes = 536870912 # 512 MiB

[filters]
min_width = 5120
min_height = 1440
ratio = "32:9" # Use "any" for every aspect ratio.
max_depth = 2  # Files directly in each folder are depth 0.

[[providers]]
id = "local"
enabled = true
refresh_interval = "5m" # Default for folders; independent of playback.interval.

[[providers.folders]]
id = "home"
path = "/home/alex/Pictures/Wallpapers"

[[providers.folders]]
id = "work"
path = "/home/alex/Wallpapers"
name = "Work wallpapers" # Optional; otherwise use the basename.
enabled = true         # Optional; defaults to true.
max_depth = 0          # Optional; otherwise inherit filters.max_depth.
refresh_interval = "15m" # Optional per-input override.
```

Removing a folder preserves its files and history; navigation skips images from
disabled/removed folders. Paths must be absolute: `~` and environment variables
are not expanded. The root folder may be a symlink, but links beneath it are
skipped. Unknown fields and invalid types are rejected;
config is limited to 1 MiB and must declare version 1.

| Field | Default | Meaning |
| --- | --- | --- |
| `version` | Required | Must be `1` |
| `desktop.backend` | `"auto"` | `auto`, `omarchy`, `noctalia` or `macos`; loaded at startup and applied to the next action after an explicit API edit |
| `desktop.fit` | `"auto"` | `auto`, `contain` or `crop`; saved presentation mode |
| `desktop.upscale` | `false` | Allow smaller images to be enlarged in supported containment modes; cannot combine with crop |
| `cache.max_images` | `20` | Positive image-count budget for the shared playback cache |
| `cache.max_bytes` | `536870912` | Positive byte budget for the shared playback cache |
| `playback.rotation_mode` | `"balance_sources"` | Optional. Set to `"shuffle_all_images"` for one chance per distinct eligible image. Applies to fresh choices |
| `playback.interval` | `"30m"` | Positive duration; API edits apply at runtime. `serve --interval` supplies a temporary startup override |
| `omarchy_theme.id` | Required when present | One globally unique ID for the active Omarchy theme source |
| `omarchy_theme.enabled` | `false` | Include the active Omarchy theme's stills in rotation |
| `omarchy_theme.image_rules` | Inherit | Optional resolution and ratio override for this source |
| `providers[].id` | Required | `local`, `wallhaven`, or an installed provider ID; unique in the file |
| `providers[].refresh_interval` | `"5m"` | Discovery duration of at least `1s`; empty inherits the default |
| Folder/search `refresh_interval` | Provider interval | Optional discovery duration of at least `1s`; empty inherits the provider interval |
| `providers[].enabled` | `false` | Enable discovery and playback for this provider |
| `providers[].folders` | Empty | Local folder tables with an `id` and absolute `path`; no searches accepted |
| `providers[].searches` | Empty | Wallhaven search tables with an `id` and optional `query`; no folders accepted |
| Folder/search `id` | Required | Stable, globally unique identifier, at most 128 bytes, without surrounding whitespace |
| Folder `name`, `enabled`, `max_depth` | Basename, `true`, inherited depth | See [local folder management](#local-folder-management) |
| `filters.max_depth` | `2` | Nonnegative default subfolder depth |
| `filters.min_width` | `1` | Positive minimum width; the default adds no optional size restriction |
| `filters.min_height` | `1` | Positive minimum height; the default adds no optional size restriction |
| `filters.ratio` | `"any"` | `any`, one positive integer custom ratio such as `32:9`, `presets`, `monitor`, or `monitor-similar` |
| `filters.ratio_presets` | `[]` | Up to 12 distinct presets or integer `width:height` ratios when ratio is `presets`; otherwise empty |
| `filters.ratio_tolerance_pixels` | `10` | Nonnegative whole number up to 9,007,199,254,740,991. Applies to pixel-based aspect ratios for all sources |

Enable at least one wallpaper source to begin playback. Configurations with
no enabled source are valid idle settings. Local accepts folders and Wallhaven
accepts searches.
Unavailable folders produce discovery warnings while validated cached images
remain usable. Disabled folder paths must still be absolute.

### Omarchy theme source

The Omarchy source follows the active theme. On Omarchy, use **Sources →
Add source → Omarchy theme** if the source is absent:

```toml
[omarchy_theme]
id = "active-theme"
enabled = true
```

The ID must be globally unique among sources. With no `omarchy_theme.image_rules`
table, the source follows the shared resolution and aspect ratio. Add that table
to override its rules using the same values as a folder or search. A minimum of
1 × 1 and a ratio of `"any"` clear those two limits. A saved override follows
the source across theme changes. See the [desktop handoff](desktop.md#omarchy)
and [Omarchy discovery](providers/omarchy.md) for eligibility and playback.

### Wallhaven settings

Append this to the local example to save Wallhaven searches:

```toml
[[providers]]
id = "wallhaven"
enabled = true

[[providers.searches]]
id = "mountains"
query = "mountains"
name = "Mountain landscapes" # Optional; otherwise use the query.
enabled = true              # Optional; defaults to true.
categories = "100"          # General; no anime or people.
tags = ["mountains", "coast"]
tag_match = "any"            # Match at least one tag.
sorting = "relevance"       # Existing provider sort choices.

[[providers.searches]]
id = "space"
query = "space"
```

Each search defaults to enabled, and the provider gate must also be enabled.
The [Wallhaven guide](providers/wallhaven.md#search-settings) covers categories,
ordering, paging, and defaults. Saved TOML without `tag_match` requires all
selected tags. Searches created through the app or private API default to
matching any selected tag. Discovery confirms availability after saving.
Wallhaven is restricted to SFW searches.

### Refresh schedules

Discovery continues while paused. Providers refresh every five minutes by
default. A folder or search can override that schedule with `refresh_interval`.
Use Go duration syntax (`30s`, `15m`, `2h`) with a one-second minimum.
Schedule edits take effect on the next settings check and count from the last
completed refresh. On restart, enabled inputs are due immediately unless a
saved retry gate applies. A query, folder path or scan depth edit affects that
input's discovery revision. Image rule changes affect the inputs that use those
rules. Unchanged inputs keep their catalog and retry state. See
[background discovery](architecture.md#background-discovery).

### Configuration selection

`--config FILE.toml` selects that file explicitly. Without it, Panora uses the OS
user config directory below. JSON config is rejected without conversion. If a
default `config.json` exists, remove it or explicitly select a TOML file.

## Local folder management

| Folder field | Omitted value | Meaning |
| --- | --- | --- |
| `name` | Folder basename | Display label, at most 256 bytes without NUL or newlines |
| `enabled` | `true` | The local provider must also be enabled |
| `max_depth` | `filters.max_depth` | Nonnegative scan depth. `0` includes root files only, `1` includes immediate subfolders |

The folder ID stays stable when you edit its name, path, depth, or schedule.
Adding the same normalized path in the app reuses its existing entry. A path
edit to another saved folder's root is rejected. Existing TOML with duplicate
roots stays readable and symlink aliases are treated as distinct roots.
An unavailable root can be saved and appears as a discovery error.

Disabling or removing an input stops its discovery and playback contributions.
If it was the last enabled input, Panora returns to setup while preserving
pause, wallpaper, history, and original files. Changing the root keeps the
folder ID but changes the discovered image identities. History keeps the old
origin and skips it while that root is unavailable to playback. Returning to
the same normalized root reuses its identities. See [catalog identity](catalog.md#schema-and-identity)
and the [folder API](socket-api.md#configuration-routes).

## Image rules for each source

Each folder, search, installed-provider input or Omarchy theme source can
have an `image_rules` block that replaces the shared minimum dimensions and
shape for that source. Folder depth is set separately with `max_depth`.
Fit, upscaling, rotation, storage and startup apply to the app as a whole.

```toml
[[providers.folders]]
id = "pictures"
path = "/home/user/Pictures"

[providers.folders.image_rules]
min_width = 1920
min_height = 1080
ratio = "presets"
ratio_presets = ["16:9", "21:9"]
```

The same block is accepted under `providers.searches` and `providers.inputs`. An optional
`exact_resolutions = ["1920x1080", "5120x1440"]` accepts up to 12 distinct
pixel dimensions. A nonempty list takes precedence over the minimum size.
The ratio rule applies independently. A `ratio = "presets"` list can mix
predefined and custom ratios. Provider hints narrow discovery while playback
checks the decoded image against the effective rules.

An existing source without a block inherits `[filters]`. New folders,
searches and installed-provider inputs start with 1×1 / Any unless you select
other rules. A new Omarchy theme source inherits shared rules. Rule changes
apply to the next selection or replay and keep the current wallpaper and
rotation deadline. An image found by overlapping searches can qualify through
one search even if a different search excludes it. History replay uses the
retained source and its current rules.

## Service-owned writes

Use Sources or Settings to edit through the service. The
[configuration API](socket-api.md#configuration-routes) explains revision
checks, conflicts, and saved versus active values.

For direct edits, stop Panora, edit the TOML, validate it with
`panora config check`, and restart. Discovery and selection can observe direct
provider/rule edits while running. Direct desktop, interval, and cache-budget
edits take effect after restart. Keep direct editors and in-app edits from
overlapping. In-app writes preserve validated settings values and serialize
them back to TOML, which can change comments and formatting.

## Runtime desktop settings

Saving Desktop settings applies to the next wallpaper action and preserves
the current wallpaper, history, pause, and rotation deadline. Panora validates
the requested adapter and presentation before saving. **Auto** requires exactly
one ready adapter. An explicitly selected adapter reports a failure if it
later becomes unavailable. See [desktop integration](desktop.md) for platform
limits and [Fit](fit.md) for geometry.

An in-app edit replaces only the temporary startup overrides for fields you
change. For example, if `serve --upscale` is active, switching to Crop also
requires disabling enlargement. The next launch uses the saved settings unless
you supply new overrides.

## Runtime cache budgets

Both cache budgets must be positive. Saving either budget in Settings applies
it immediately and evicts eligible older copies. The current and pending
images and any active cache reads stay protected. Shared content counts once
per file. History, original images, and the separate Omarchy Fit cache are
outside this cleanup.

Protected files can exceed the saved budget. Increase the budget if Panora
cannot prepare the next image. Settings shows usage and cleanup failures.
After correcting a filesystem problem, reload settings and save the budget
again to retry cleanup. See [cache status](socket-api.md#cache-status).

## Rotation mode

In the candidates loaded for a fresh choice, **Balance sources** gives each
eligible provider an equal chance, then chooses an input and image. **Shuffle
all images** gives each distinct eligible image an equal chance across
sources, including images with known copies. Panora applies the rules for the
selected membership. See the [selection bounds](architecture.md#what-happens-when-you-run-next).

Save a mode in Settings under **Wallpaper timing**. It applies to fresh Next
and scheduled choices. Saving it preserves the current image, history cursor,
pause state and rotation deadline.

## Rotation timing

Changing the interval in Settings starts a fresh countdown. Saving an
equivalent duration or editing an unrelated setting preserves the deadline.
Pause stays paused. Resume or a successful Next, Previous, or replay starts
a fresh interval.

`serve --interval 1h` overrides the saved interval for the current session.
An explicit interval edit in Settings replaces it, even if the new value
matches the TOML. After restart, the saved interval runs from the last
persisted playback action. One change may already be due. Missed changes
coalesce into one action.

## Image eligibility

Ratio components and dimensions must be positive integers fitting the platform's
`int` type (up to 9223372036854775807 on 64-bit builds). Overflowing values are
rejected. For pixel-based aspect ratios, either dimension can differ from the
ideal shape by up to `filters.ratio_tolerance_pixels`. The default is 10 pixels.
Use 0 to require an exact ratio. The tolerance applies to source-specific ratios
and shared ratios, while minimum and exact resolutions stay strict. For example,
5120×1439 is close enough to 32:9 but fails a minimum height of 1440.

To accept several shapes, set `ratio = "presets"` and
`ratio_presets = ["16:9", "21:9"]`. Supported preset names are `16:9`, `16:10`,
`21:9`, `32:9`, `4:3`, `3:2`, `1:1` and `9:16`. Any selected preset can match;
neither minimum dimension is relaxed. The rounded `21:9` preset accepts exact
7:3, 64:27 (for example, 2560×1080) and 43:18 (3440×1440), each with the configured
pixel tolerance. A custom `ratio = "21:9"` uses only the 7:3 shape.

Existing version-1 files use the 10-pixel default and need no conversion.
The optional preset field is omitted when empty. Older Panora builds reject
`presets` and the new field rather than silently interpreting them as Any.
Changing away from presets through the API must also clear `ratio_presets`.

### Match monitor

`ratio = "monitor"` resolves one shared ratio from exactly one unambiguous
display. Omarchy uses the full unrotated monitor's pixel dimensions. macOS uses
the full AppKit frame converted to backing pixels with
[`convertRectToBacking`](https://developer.apple.com/documentation/appkit/nsscreen/convertrecttobacking(_:));
menu-bar/Dock areas stay included. These are the current backing-store dimensions,
which can differ from physical panel pixels in scaled modes. They are already
oriented; the reported scale is informational and is not multiplied again.
Noctalia geometry remains unsupported. Rebuild an older macOS helper to enable
geometry queries.

**Similar to my screen** in the Wallhaven search picker saves
`ratio = "monitor-similar"`. It uses the same display geometry and accepts an
image whose width divided by height is within 5% of the display ratio, including
both boundaries. For a 3024 × 1964 display, this includes 3:2 and 16:10. The
search’s minimum width and height are configured independently. The `monitor`
rule uses the configured pixel tolerance.

Configuration reads and explicit preference saves return `rule_resolution` with
state, resolved integer ratio, reason, backend and display when available.
Multiple displays stay ambiguous even if their ratios happen to agree; there is
no implicit primary-display selection or independent wallpaper assignment.
Unavailable geometry never broadens to Any. The next playback/history action
rechecks geometry with a three-second query budget. An unavailable rule rejects
the action with `rule_unavailable`, preserving the accepted wallpaper and history.
A changed valid ratio applies to the next action, without reapplying the current
wallpaper, changing pause or resetting the timer. Minimum dimensions stay strict.

`scan` inherits these rules with per-run flag overrides. `debug set IMAGE` bypasses
playback eligibility/history. See [CLI](cli.md).

### Preview a draft rule

Settings can preview unsaved rules against already discovered enabled images.
Shared rules count only sources that inherit them. The preview labels partial
results and unknowns. It can read local headers or already validated cached
images; it leaves drafts and online sources untouched. See the
[rule-preview API](socket-api.md#draft-image-rule-previews) for scope and limits.

## Startup flags and persisted settings

The installed Omarchy and macOS services use `--pause-for-setup` to pause
when no inputs are enabled. Configured profiles keep their saved pause state.
For foreground commands and flags, see the [CLI reference](cli.md#foreground-service-and-controls).

| Setting | Where you set it | What happens after restart |
| --- | --- | --- |
| Folder and image rules | `config.toml` | Reloaded at startup and for each image-selection or history-navigation request |
| Provider/input refresh intervals | TOML `refresh_interval` | Reloaded every second; enabled inputs refresh immediately after restart unless retry limits apply. |
| Rotation interval | TOML/API `playback.interval`, optional `serve --interval 30m` | Saved duration applies at startup unless a CLI override is supplied; an explicit API interval edit supersedes the running override. See [rotation timing](#rotation-timing). |
| Exclusions | `exclusions add`, `exclusions ls`, `exclusions remove ID` | Saved in SQLite independently of history; original files remain untouched. |
| Pause | `pause` / `resume`, or `serve --paused` | Saved in state. `--paused` forces pause; omitting it does not clear a saved pause. Use Resume to clear it. |
| Desktop integration | TOML/API `desktop.backend` | Loaded at startup; API edits apply to the next action. Auto requires exactly one ready integration. No backend flag/override. |
| Presentation | TOML/API `desktop.fit` and `desktop.upscale`; optional temporary `--fit`/`--upscale` overrides | Saved settings return on restart; explicit API edits replace the corresponding running override. See [Fit](fit.md). |
| Config/state location and plugin substitution | `serve --config`, `--state-file`, `--local-plugin` | Supply the same overrides again to use the same files and implementation. |
| Control socket | `--socket` | Supply the same override to both `serve` and each control command. |

Pause, startup installation and plugin registration are stored outside TOML.
For when direct edits take effect, see [service-owned writes](#service-owned-writes).

On Linux, the default socket requires an owned, private `XDG_RUNTIME_DIR`
supplied by your login session. You can instead choose an absolute socket path
in a private directory. Both terminals must use that path:

```sh
# First terminal:
panora serve --config /absolute/path/config.toml --socket /private/path/control.sock
# Second terminal:
panora next --socket /private/path/control.sock
```

Panora creates a missing socket directory with mode 0700. An existing directory
must be owned by you, private, and not a symbolic link. The socket is mode 0600.
These checks also apply on macOS, including when `XDG_RUNTIME_DIR` is set there.

## Default file locations

The [Arch package](arch.md) places executables and desktop assets under `/usr`.
The per-user locations below apply equally to package and checkout builds.

`--config FILE` and `--state-file FILE` override the config and state paths. On
Linux, XDG config, state, and cache directory overrides must be absolute.
In Settings, **Storage usage** shows the connected service's playback cache
path, and **Configuration file** opens its active TOML file.

| File | Linux default | macOS default |
| --- | --- | --- |
| User settings | `$XDG_CONFIG_HOME/panora/config.toml`, or `~/.config/panora/config.toml` | `~/Library/Application Support/panora/config.toml` |
| Playback state | `$XDG_STATE_HOME/panora/state.db`, or `~/.local/state/panora/state.db` | `~/Library/Application Support/panora/state.db` |
| Playback cache | `$XDG_CACHE_HOME/panora/playback/`, or `~/.cache/panora/playback/` | `~/Library/Caches/panora/playback/` |
| Control socket | `$XDG_RUNTIME_DIR/panora/control.sock` | The same XDG path if set; otherwise `~/Library/Caches/panora/run/control.sock` |
| Omarchy startup registration | `~/.config/systemd/user/graphical-session.target.wants/panora.service` by default, activates `/usr/lib/systemd/user/panora.service` | Not applicable |
| macOS startup registration | Not applicable | `~/Library/LaunchAgents/org.panora.service.plist`; owned by the installed app lifecycle layer |
| macOS service logs | Optional `serve --log-file` path | `~/Library/Logs/Panora/service.log` and `service.log.1`, at most 1 MiB each |
| Prepared Omarchy Fit images | `$XDG_CACHE_HOME/panora/rendered/`, or `~/.cache/panora/rendered/` | Native macOS rendering needs no prepared-image cache |

Login registration is separate from TOML and SQLite. Start at login is optional
and initially off. The app lifecycle layer serializes changes with `appservice.lock`
beside the private socket; it does not write playback settings. Follow the
[macOS guide](macos.md#data-replacement-and-removal) before moving/removing the app.

## Generated files and ownership

Stop the service before moving or removing its state or caches. The last
wallpaper can still use a [prepared Fit image](fit.md#generated-files) after
shutdown. Removing it can break that wallpaper.

SQLite keeps preferences-independent state such as discovery checkpoints,
up to 100 history entries, exclusions, pause, and recovery data. The
[catalog guide](catalog.md#opening-and-compatibility) covers database upgrades
and recovery. Unsupported or corrupt state is preserved. To start with separate
state, use `--state-file` and a separate socket and cache directory.

Lock files can remain after shutdown. They protect settings, state, the socket,
and each cache. Keep them in place while Panora runs. The
[cache design](architecture.md#shared-playback-cache) explains how Panora
protects selected images and preserves user originals.
