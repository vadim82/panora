# Desktop integration

Panora submits a static image to the selected desktop adapter. The service
records accepted requests. Use an actual display observation when checking how
an image appeared. Stopping playback or the service leaves the last wallpaper
in place. Changes made externally or through `debug set` do not enter Panora
history. An interrupted or uncertain apply pauses automatic rotation until
explicit recovery.

## Omarchy

Omarchy's shell owns wallpaper presentation. Panora uses the native background
setter and leaves the selected theme palette intact. Centered Fit supports
one unrotated monitor at a time, with black padding and optional enlargement.
Native crop uses the shell's global placement. See [Fit](fit.md) for geometry
and cache behavior.

With the **Omarchy theme** source included, each native theme activation starts
a new rotation interval after the shell reports that its opening intro and
reveal have finished. This also gives a native still or video an interval in
mixed rotation. The service checks the theme generation under Omarchy's native
theme-switch lock before dispatching a wallpaper. Obsolete choices are dropped,
including choices made before a quick return to the same theme.

A theme switch preserves pause. A stopped service stays stopped. When the
installed service creates its first settings file, it can display a still
as soon as the native intro and discovery finish. On subsequent starts the
service gives the current theme a full interval. Manual native background
changes within the active theme keep their ordinary Omarchy behavior and do
not restart Panora's interval. An explicit **Next** can resume selecting from
enabled sources. History records Panora's accepted requests.

Use the [Arch package guide](arch.md) for local builds and installation. The
[optional bar integration](../integrations/omarchy/README.md) uses the same
running service as the native window and CLI. Review shortcuts for conflicts
before enabling them.

Ordinary-session checks cover native keyboard folder selection, physical
shortcuts, bar restart/reconnect, theme retints and handoff, and attended
suspend/resume. See [verification](verification.md) for fixture and hardware
boundaries and [status](status.md) for installed coverage.

## Noctalia

On Niri with Noctalia, Panora requests a global image through Noctalia IPC.
Keep `desktop.fit = "auto"` and `desktop.upscale = false`. Presentation remains
with Noctalia. An initial trial confirmed one wallpaper change. Broader
rotation, lifecycle and restoration still need an installed trial.

## macOS

The Swift companion submits the image to AppKit for each connected screen in
its current Space. On a Space switch, the service reapplies the latest accepted
cached image to the visited desktop without advancing history or rotation.
Panora checks the accepted cached file against its recorded hash before
handing the same bytes to AppKit.
This also works while paused. The helper groups switch notifications within
a 25 ms window. The service coalesces queued switch events and allows a
follow-up when another switch arrives during an apply. Stop joins the
observer before releasing ownership. If the observer times out or exits, the
service retries after one second, doubling the delay up to 30 seconds. Once
it reconnects, the service reapplies the accepted wallpaper to the active
Space to catch a missed switch. If a later screen fails, earlier screens
may already have changed.

AppKit's wallpaper getter and setter take a screen and expose no Space
selector. The verified scope is two visited ordinary Desktops on one display.
Full-screen Spaces, multiple displays and all inactive Desktops still need
separate visual evidence. [AppKit wallpaper API](https://developer.apple.com/documentation/appkit/nsworkspace/setdesktopimageurl%28_%3Afor%3Aoptions%3A%29).

Use the [macOS app guide](macos.md) for the bundle, background service and
startup controls and [CLI-only helper builds](macos-development.md#cli-only-development).
See [status](status.md) for remaining installed verification.
