# Centered Fit

Fit preserves the whole image. On Omarchy, Panora centers it on a black,
monitor-sized PNG without enlarging it by default; the native shell displays it.

## Commands

Create a [valid configuration](configuration.md#user-configuration) before using
these direct-apply diagnostics:

```sh
go run ./cmd/panora debug set /absolute/path/image.jpg
go run ./cmd/panora debug set /absolute/path/image.jpg --fit contain
go run ./cmd/panora debug set /absolute/path/image.jpg --fit contain --upscale
go run ./cmd/panora debug set /absolute/path/image.jpg --fit crop
```

Saved `desktop.fit` defaults to `auto`; `desktop.upscale` defaults to false.
Flags override those settings for one invocation. On Omarchy and macOS,
contain preserves the whole image. Automatic uses the same behavior except
when playing a still from the Omarchy theme source with enlargement off.
In that case Omarchy fills the display using its native crop.
Noctalia requires auto without enlargement
and uses its own display settings; `contain`, `crop` and enlargement overrides
are rejected before the wallpaper changes.

Containment keeps the entire image within the monitor and preserves its aspect
ratio, with integer rounding of at most one pixel. Unused space is opaque black.
Transparent PNG pixels are composited onto black as well. Smaller images retain
their original pixel dimensions unless `--upscale` explicitly permits enlargement.
This can produce padding on all four sides. Larger images are reduced to fit.
Native `--fit crop` sends the original path directly to Omarchy and lets it crop
and scale globally. Combining crop with `--upscale` fails rather than ignoring
the flag. Unknown modes, including stretching, fail before backend resolution.

`debug set` bypasses playback filters and history. See
[runtime desktop settings](configuration.md#runtime-desktop-settings) and
[startup overrides](configuration.md#startup-flags-and-persisted-settings)
for service preferences.

## macOS

macOS uses AppKit's native placement modes. With enlargement off, the helper
reads the image's oriented pixel dimensions and compares them with each screen's
backing dimensions. An image that fits is centered at its original size. An
image larger than either display dimension is scaled proportionally to fit.
Turning enlargement on uses proportional Fit for any image size. Both Fit modes
request black padding. Crop uses AppKit's proportional fill and allows clipping.
AppKit's wallpaper API does not support a single downscale-only mode.
[AppKit image scaling options](https://developer.apple.com/documentation/appkit/nsworkspace/desktopimageoptionkey/imagescaling).

Panora sends the original cached image to AppKit. macOS Fit requires no
additional rendered-image cache. See [helper setup and scope](macos-development.md#cli-only-development)
and [desktop integration](desktop.md#macos) for current Spaces coverage.

## Monitor support and preparation

Omarchy queries `hyprctl monitors -j` with a three-second timeout. Fit requires
exactly one enabled, unrotated monitor with valid dimensions and scale.
Disabled outputs are ignored. Multiple outputs, rotated/flipped outputs, missing
Hyprland IPC, and malformed responses fail before wallpaper application. Explicit
crop retains Omarchy's native global scope and needs no monitor query.

The canvas uses the monitor's native pixel dimensions.
JPEG, PNG, and static WebP headers and full decoding are checked; both the source and canvas are
limited to 64 million pixels before allocation. `golang.org/x/image/draw` supplies
[ApproxBiLinear scaling](https://pkg.go.dev/golang.org/x/image/draw#ApproxBiLinear).
Decoded images are not automatically oriented from EXIF metadata.

After preparation, a second monitor query must match name, dimensions, and scale.
A changed monitor configuration fails with a retry message. Application then uses
the existing native setter's five-second timeout and one-second `WaitDelay`.
Cancellation is checked between synchronous image-processing stages. After a
display change, request another wallpaper: Panora does not reapply automatically.

## Capability inspection

Clients can read [desktop capabilities](socket-api.md#desktop-capabilities) through the
running service before offering presentation controls. On Omarchy, auto/contain
and their enlargement option are available only for one valid unrotated monitor;
native global crop remains available with multiple, rotated or unavailable
geometry. Disabled outputs are ignored. The snapshot reports detected dimensions,
scale and transform, but topology may change after inspection; application still
rechecks the actual target before using a prepared image.

Noctalia exposes auto only, leaving presentation to its settings. macOS exposes
the implemented native containment/crop options with current-Spaces scope.
Noctalia geometry is unsupported. The macOS helper exposes oriented full-frame
backing pixels for [Match monitor](configuration.md#match-monitor); scale is
reported separately and does not multiply those dimensions. This does not change
AppKit wallpaper rendering or establish Retina/Spaces visual acceptance.
No independent per-monitor assignment or new desktop compatibility evidence is
provided by this API.

## Generated files

Fit results live in the [rendered-image cache](configuration.md#default-file-locations).
The root must be absolute. Fit writes uncompressed PNGs for faster preparation
and display, with the same decoded pixels as compressed PNGs. Larger files
can reach the byte limit sooner. See the
[Fit encoding comparison](verification.md#fit-encoding-comparison).

PNGs are encoded completely before atomic publication under a SHA-256 content
name. Identical encoded bytes reuse the same path. Originals are never
overwritten. Failed encoding or publication removes temporary files. A later
monitor or setter failure can leave a complete unused result.

Omarchy's background link points to the PNG; keep it after Panora exits and across
shell restarts. Cleanup runs before preparation and application under a process
lock held through the native setter. It keeps at most **20 generated files and
512 MiB**, evicting the least recently modified first. Reusing a rendering refreshes
its age. These limits are separate from the playback cache budget.
The current native background, requested input, and prepared next image are
protected. If protected artifacts alone exceed the budget, application fails
instead of removing them. Preparation can temporarily exceed the budget by one
image; a crash or cancellation is cleaned up on a later Fit application.

Only regular lowercase SHA-256-named PNGs and `.fit-<digits>.png` temporary files
in Panora's rendered directory are managed. Stale temporary files are removed;
unknown files, directories, and symlinks are left alone and do not count toward
the quota. Crop and other backends do not generate Fit files or run this cleanup.

To protect the displayed image, cleanup resolves the native setter's
`~/.local/state/omarchy/current/background` link, independently of `XDG_STATE_HOME`.
Cleanup fails before application if an existing link is dangling or unreadable.
The link is refreshed before each removal. Concurrent native theme changes do
not honor Panora's lock, so cleanup cannot make a native change and deletion
atomic; lifecycle behavior still requires desktop validation. All Panora Fit
applications serialize through the same cache lock, even with different state files.

History retains the original image identity, so Panora can regenerate an
evicted Fit PNG. See [Omarchy verification](verification.md#omarchy-and-linux) for physical
Fit checks on the supported single-display setup.
