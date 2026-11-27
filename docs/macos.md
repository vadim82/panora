# Panora on macOS

## Install with Homebrew on Apple silicon

Install Xcode Command Line Tools and [Homebrew](https://brew.sh/). The tap and
trust commands are a one-time setup. Homebrew builds and signs Panora locally,
including the desktop app and the `panora` CLI.

```sh
brew tap vadim82/panora https://github.com/vadim82/panora.git
brew trust --formula vadim82/panora/panora
brew install panora
open "$(brew --prefix panora)/libexec/Panora.app"
```

If you used the previous private cask, first [prepare Panora for
removal](#data-replacement-and-removal) and quit the app. Run
`brew uninstall --cask panora` and `brew untap vadim82/panora`, then follow
the new tap commands above.

To check for upgrades, run `brew update` and `brew outdated panora`. Before
running `brew upgrade panora` or `brew uninstall panora`, use **Prepare to move
or remove app…** and quit Panora. After upgrading, reopen the app and re-enable
**Start Panora at login** if you used it.

## Download an Apple silicon build

When a [GitHub release](https://github.com/vadim82/panora/releases) includes
an Apple silicon ZIP, download it with `SHA256SUMS.macos-arm64`. Run this command
beside both files before extracting the ZIP and moving `Panora.app` into
`~/Applications` with Finder.

```sh
shasum -a 256 -c SHA256SUMS.macos-arm64
```

The build receipt lists the minimum macOS version. These ZIPs are ad-hoc signed
and unnotarized. Gatekeeper may block the downloaded app. The
[macOS development guide](macos-development.md) also covers local builds.

## Set up wallpapers

Follow the [native interface guide](ui.md#sources-manager) to start rotation from
a folder or a saved SFW Wallhaven search. When you visit another ordinary Desktop
in Spaces, Panora reapplies its current wallpaper there. See
[desktop integration](desktop.md#macos) for the verified Spaces scope.

## Menu bar and Settings

The installed service shows one Panora icon in the menu bar, including when
it starts at login. Closing the main window or pressing **⌘Q** quits the window
app and removes it from **⌘Tab**. The menu and rotation keep running. Use the
menu to open Panora, see its version under **About Panora**,
or control playback with **Previous**, **Pause rotation**/**Resume rotation**,
and **Next**.

**Stop Service & Quit…** stops the service and removes its menu bar icon.
Stopping the service from Settings or the CLI also removes the icon.
The last wallpaper and the login preference stay in place.

Open Settings from **Panora → Settings…**, press **⌘,**, or select the gear at the
top right of the window. To launch the service on future logins, turn on
**Start Panora at login** under **Startup**. It’s off by default.

## Background service controls

Open **Settings → Startup → Background service** to start or stop the service. Reopen
the app or select **Start service** if it has stopped. If switching Desktops
stops updating the wallpaper, Panora retries its Spaces connection in the
background and updates the active Desktop when it reconnects.
If you started a separate service, stop it where you started it before starting
the managed service from Panora.

Choose **Open logs folder** in the same settings section to find
`~/Library/Logs/Panora/service.log`.

## Data, replacement and removal

Before moving, manually replacing or uninstalling Panora, select
**Prepare to move or remove app…** in **Settings → Startup → Background service**.
Confirm to stop the managed service and remove its login registration. A separately
started service keeps running. Then quit
Panora and move or replace the complete app in Finder.

Your settings, history, exclusions and original images stay in place. Opening
a moved app registers its new location. If you already deleted the app and its
login entry remains, reinstall and reopen Panora to run the same preparation.
[Panora file locations](configuration.md#default-file-locations)
