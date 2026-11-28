# Platform status

Panora rotates JPEG, PNG and static WebP images from local folders, saved SFW
Wallhaven and Alpha Coders searches, and the active Omarchy theme. The CLI and native client share
one service for settings, playback history, exclusions and on-demand discovery.

## Platform coverage

| Platform | Available | Coverage limits |
| --- | --- | --- |
| [Omarchy on Arch x86-64](arch.md) | Native Wayland window, graphical user service, static rotation, centered Fit on one unrotated monitor, and optional bar controls. Installed first use and daily playback passed on a physical desktop. | Physical sleep with the packaged service, broader display and GPU coverage, and assistive technology. |
| [macOS on Apple silicon](macos.md) | Build from source for the build host's macOS version. Installed playback, native window and menu bar, login startup, Fit at 2× and restoration on two ordinary Desktops passed in a macOS 27.2 guest. Physical recovery checks used a foreground service on macOS 27.2.1. | External display reconnect, full-screen Spaces, broader display coverage and assistive technology. A public signed and notarized binary is not yet available. |
| Niri with Noctalia | Experimental global static-wallpaper adapter. One wallpaper change was observed. | Installed rotation, service lifecycle, presentation and restoration. |

See [desktop integration](desktop.md) for adapter behavior and
[verification](verification.md) for the scope of the trials.
