# Omarchy shell client

`io.github.vadim82.panora/` is a native Quickshell bar-widget plugin for the current Omarchy
shell plugin API. The popup shows the last accepted image, a row with Previous,
Play/Pause and Next, and service or recovery feedback. The preview fits the
complete image within the available space. The bar icon and popup use the shared
`qs.Ui` and `qs.Commons` kit and update their colors when the active theme changes.

## Install from the Marketplace

The widget requires the Panora app and its background service. Install the
[Panora package](../../README.md#install-on-omarchy) and open the app from the
Omarchy launcher. On Omarchy 4.5 the service registers and enables the bundled
widget on first use. If you previously disabled it, use **Setup → Plugins →
Enable** to bring it back.

The Marketplace listing uses manual setup. The root manifest describes the
same widget the package installs, with the same ID. The package supplies the
CLI and desktop executable paths. Use the package installation above to set up
the widget, then follow [Remove](#remove) or the
[app removal guide](../../docs/arch.md#install-upgrade-and-remove) when needed.

## Packaged widget

On Omarchy 4.5, when the packaged background service starts, it registers
and enables the bar button if this user has no Panora widget. This also
applies to existing Panora profiles after an upgrade. With **Start Panora
at login** enabled, the button appears without opening the app window.

The button follows the background service connection. Left-click the icon
for the wallpaper preview and playback controls. Right-click for **Open Panora**,
**About Panora**, **Hide from bar**, and **Quit Panora**. Menu or Shift+F10 also
opens the menu from the wallpaper popup. Omarchy's keyboard summon command
for the wallpaper popup is `omarchy-shell shell summon io.github.vadim82.panora`.

**Quit Panora** stops the observed service instance and closes already-open
Panora windows using that service. The button disappears when the service
disconnects. The last wallpaper and **Start Panora at login** preference persist.
To keep the service from starting at the next login, first clear **Start Panora
at login** in the app's Settings. To leave the service running with the icon
hidden, choose **Hide from bar**, use **Setup → Plugins → Disable**, or run:

```sh
omarchy plugin disable io.github.vadim82.panora
```

Re-enable it from **Setup → Plugins → Enable**. Panora preserves existing
enablement and placement. If you have the old `vadim82.panora` widget, use the
[bar migration steps](#migrate-an-existing-bar-entry) to retain its position.
On upgrade, the packaged service asks Omarchy Shell to load the new widget
files through the existing registration. If Panora is stopped, the widget is
refreshed the next time the service starts.

## Install a development copy

These steps target the native Omarchy shell plugin API on Omarchy 4.5. Build the
CLI and, optionally, the native window with
[`mise exec -- task dev:build`](../../docs/frontend.md#native-development-on-omarchy).
Keep the executables at stable absolute paths owned by you or the system. Start
Panora separately using your normal service launch and check `panora status`
before adding the widget. Keep the service running through shell reloads.

From the Panora checkout, validate the plugin and copy it to a new directory.
Stop if `mkdir` reports an existing Panora plugin directory.

```sh
plugin_source="$PWD/integrations/omarchy/io.github.vadim82.panora"
plugin_parent="$HOME/.config/omarchy/plugins"
omarchy plugin validate "$plugin_source" &&
  mkdir -p "$plugin_parent" &&
  mkdir "$plugin_parent/io.github.vadim82.panora" &&
  cp -R "$plugin_source/." "$plugin_parent/io.github.vadim82.panora/"
```

In `~/.config/omarchy/shell.json`, add one object to the existing
`bar.layout.right` array (or your preferred bar section). Keep the other entries,
theme and layout settings. For a managed config, edit its source. If the file
does not exist yet, use the output of `omarchy-shell shell listShellConfig` as
the starting JSON. Substitute your own absolute executable paths.

```json
{
  "id": "io.github.vadim82.panora",
  "executable": "/home/user/code/panora/build/panora",
  "uiExecutable": "/home/user/code/panora/build/panora-ui"
}
```

Omit `uiExecutable` for playback-only development. Open and About require the
native client. Quit still stops the service without a configured native client.
Without an explicit `socket`, both clients use Panora's default private runtime
socket. For an
existing service on a custom socket, add its absolute path as `socket` and use
that same path in any shortcuts. The widget invokes only configured absolute
executable paths, as separate argv entries. Choose paths you trust.

Run `omarchy-shell shell rescanPlugins` to discover the copy and
`omarchy-shell shell reloadConfig` to reload the layout if needed. Confirm the
entry with `omarchy plugin list --json` and open it from the bar or with
`omarchy-shell shell summon io.github.vadim82.panora`. After a normal
`omarchy restart shell`, open the widget again. The new widget reads the existing
service over the configured socket. If the CLI path is missing or relative, the
popup asks for an absolute executable path.

## Migrate an existing bar entry

If your bar has a `vadim82.panora` entry, validate and copy the new plugin
as shown above. In the shell config source you used for installation, find
the existing entry in `bar.layout` and change its ID to
`io.github.vadim82.panora` in the same position. Keep its executable paths,
socket and other settings. If `disabledPlugins` contains the old ID, update
that entry too to keep the widget disabled. If the old plugin directory
exists, move it to a backup location outside `~/.config/omarchy/plugins/`
so Omarchy no longer discovers it. Use the rescan and reload commands above and
confirm the new ID with `omarchy plugin list --json`.

Package upgrades leave user plugin copies and shell configuration in place.
Panora continues to use the same service, private socket and playback history.

## Optional Hyprland shortcuts

Check the running compositor before adding either binding. Review the whole menu
and JSON output, including submaps and physical keycodes. The JSON filter shows
entries in the Super+Alt modifier mask regardless of submap, along with catch-all
entries. A named physical bind can have a blank JSON key field, so also consult
the native menu and your binding source files.

```sh
omarchy menu keybindings --print
hyprctl -j binds | jq '.[] | select(.modmask == 72 or .catch_all)'
```

The preferred chords are **Super+Alt+Right** for Next and **Super+Alt+Left** for
Previous. On the reviewed Omarchy installation those chords already move the
active window into a group, while **Super+Alt+F12** and **Super+Alt+F11** were free.
Keep existing actions. Choose a free pair confirmed in your session, or skip
these optional bindings.

For that alternative pair on Omarchy's Lua configuration, add the two lines to
`~/.config/hypr/bindings.lua` (or its managed source) after substituting the same
trusted CLI path. If the service uses a custom socket, include
`--socket '/absolute/private/control.sock'` after each playback command, shell
quoting its path with Omarchy's `o.shell_quote` helper.

```lua
local panora = o.shell_quote("/home/user/code/panora/build/panora")
o.bind("SUPER + ALT + F12", "Panora Next wallpaper", panora .. " next")
o.bind("SUPER + ALT + F11", "Panora Previous wallpaper", panora .. " previous")
```

Run `hyprctl reload`, check `hyprctl configerrors` and inspect the binding list
again. Both shortcuts issue one request to the running service. A shortcut
continues to work when the bar popup is closed or the shell is reloading.

## Remove

Remove only the two Panora lines from `bindings.lua` if you added them. Run
`hyprctl reload` and check `hyprctl configerrors` again. Use `omarchy plugin remove io.github.vadim82.panora`
to remove the widget. Omarchy 4.5 disables it and unlinks the packaged plugin
registration. For a manually copied plugin, Omarchy moves the copy to a hidden
backup and prints its path. Confirm that Panora disappears from
`omarchy plugin list --json`.
The service, wallpaper history and executables remain available for your other
Panora clients.

## Requests and keyboard controls

The controller runs at most one bounded subprocess. It observes small JSON v1
snapshots every five seconds while closed and two seconds while open. It requests
image details and a cached preview only while open and when the image changes,
details are missing, or the user selects **Check status**. Response retention stops at 3 MiB while reading,
with a 7.5-second read deadline. Stderr is drained without retention and
the child is terminated above 64 KiB. Actions have a 35-second deadline.
The controller checks fresh status, then sends the observed service ID with
the action. A replacement service rejects the request. Finish an unconfirmed
action by checking status before trying again. Preview details are
keyed by service instance, image ID and history entry. Disconnection retains only
labeled last-known data; a new authoritative identity clears it.

See the [snapshot schema](../../docs/socket-api.md#thin-shell-snapshot-json-v1).
All dynamic labels use `Text.PlainText`; tooltips are static translated UI text.
Navigation and playback commands remain owned by the service, which enforces
readiness/busy/uncertainty and acceptance. Closing/unloading the widget never
stops playback or starts an additional service. The popup starts on Next
when available, Check status when a status check can help, or Close when the
other controls are unavailable. Tab and Shift-Tab move between enabled controls.
Click the bar icon or press Enter or Space when it has keyboard focus to toggle
the popup. Escape or a click outside dismisses the popup or the context menu.
When an open popup regains focus, the selected control receives focus again.
Up, Down, Tab and Shift-Tab navigate the context menu. Enter and Space activate
its focused action. **Open Panora** opens the app against the same service and
**About Panora** opens its version information.
The Play/Pause icon shows the rotation state. When the running service supplies
a valid next-change time, it appears below the controls.

Checks:

```sh
omarchy plugin validate integrations/omarchy/io.github.vadim82.panora
python3 -m unittest tools.test_omarchy_shell
node integrations/omarchy/tests/model.test.mjs
```

The Python suite runs headless native Controller QML subprocess tests when `qs`
is available, using an isolated runtime directory. It checks oversized/no-newline
and incomplete output, excessive stderr, a hung child ignoring SIGTERM, instance changes before action, and unconfirmed actions
without replay. A separate headless shared-kit PopupContent check verifies Tab/Shift-Tab,
Return/Space, Escape during a playback action, selected-control focus
restoration, the missing-executable Close fallback, and menu order and invocation.
The tests report skips for missing optional tools. For bar placement, plugin
reload and service lifecycle, use the
[isolated Omarchy guest](../../docs/installed-release-checklist.md#headless-omarchy-guest).
