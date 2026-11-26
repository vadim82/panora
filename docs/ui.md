# Native interface

The native window shares playback and preferences with the CLI and shell
controls. The Linux window uses Paper for light appearance and Midnight
when the webview reports a dark appearance preference.

On macOS, Panora follows the system’s light or dark appearance and uses system
typography, a compact toolbar, neutral buttons and blue focus and selection
states.

Settings uses the same layout on Linux and macOS. Short labels and controls
share rows where space permits and stack in smaller windows. The desktop
framing preview sits alongside Picture fit controls at normal window widths
and below them in a narrow window.

In an Omarchy session, the window follows the active theme while it is open.
The theme colors the window, panels, dialogs, text, borders, and controls.
Panora checks text and button labels at 4.5:1 contrast and control boundaries
and selection indicators at 3:1. It substitutes readable colors for missing or
low-contrast accents and uses Paper or Midnight if the theme cannot supply
readable foreground and background colors. Theme changes preserve focus,
unsaved edits, and playback. Saved Wallhaven swatches retain their colors.
**Use theme palette** copies the current swatches into the search draft on
request.

## Everyday use

The title and navigation tabs stay visible as you scroll each section.

- **Now** shows the whole last accepted wallpaper in the center of the
  wallpaper strip. Click the left or right navigation button, or use Left and Right
  while the strip or a navigation card has focus. The angled side cards show cached previews when available, with chevrons for
  navigation. They load independently of the center image and move below it
  in a narrow window. Keyboard focus highlights the navigation control while
  keeping the current wallpaper and its menu in front. Now reuses
  recently decoded previews for matching images.
  A ready neighbor moves immediately while Panora applies the wallpaper.
  Repeated input can move up to two positions from the accepted image while
  a change is in progress. Panora keeps the latest destination and applies it
  after any desktop write already underway. Image details and history follow
  desktop acknowledgement. Now retries temporary preview failures while
  loading. Next selects a fresh image once forward history ends. The image details show
  its resolution, aspect ratio, and saved search name when available.
  Use **Pause rotation** to stop scheduled changes and keep manual navigation
  available. The **⋮** menu sits in the upper-right corner of the preview and
  opens above the neighboring cards. It
  opens the original location or **Picture settings** and lets you exclude
  the image. Restore exclusions individually in **History → Excluded**.
  If the wallpaper or service changes before Exclude is applied, review the
  current image before trying again.
- **Sources** manages folders, saved SFW Wallhaven searches, the active
  Omarchy theme, and inputs for installed providers. Open **Add source** to
  choose a source. Select **Start wallpapers** when a saved source is ready and
  no wallpaper has been displayed.
- **History** lists accepted images and supports replay. **Excluded** lists
  marked images, including those outside the retained history window. Restore
  clears its exclusion mark. If the preview is busy, reload History. A preview
  whose cached bytes are missing is shown as unavailable.
- Now, History and Excluded show saved Wallhaven titles when available.
  Without a title, Now uses the image ID as its heading and names Wallhaven
  in the details. History and Excluded show **Wallhaven · ID**.
- The gear at the top right opens **Settings** for timing, picture fit, shared
  image rules and storage.
  When a single display and a wallpaper preview are available, **Picture fit**
  estimates framing using the selected settings. The estimate does not change
  the wallpaper.
  **Shared image filters** sets minimum resolution and aspect ratio for sources
  that inherit [shared rules](configuration.md#image-rules-for-each-source).
  **Aspect ratio tolerance (pixels)** applies to pixel-based aspect ratios
  across all sources and defaults to 10.
  Choose a resolution preset or use **Custom size**. **Startup** contains
  **Start Panora at login** and platform-specific service controls for
  [Omarchy](arch.md#install-upgrade-and-remove) and
  [macOS](macos.md#background-service-controls).
  At the bottom of Settings, **Storage usage** shows the playback cache path.
  **Configuration file** shows the active TOML path and opens it using the
  desktop. Omarchy uses its configured editor if the file has no associated app.

**Settings → Wallpaper timing** controls the interval and
[rotation mode](configuration.md#rotation-mode).

Open **Settings → About Panora** to see and copy the version of the running
app, including the exact Arch package version when installed from a package.
On macOS, **Panora → About Panora** opens the same screen. On Omarchy,
`panora-ui --about` opens it directly. About works while the background service
is stopped. A checkout build shows **Development build** with its source
revision when available.

## Sources manager

On its first launch with a fresh profile, the installed Omarchy app enables
the active **Omarchy theme** source and starts rotation when an eligible
wallpaper is ready. Use **Sources** to add folders or edit image rules.

On other fresh profiles, **Choose folder and start** saves the selected folder,
then displays its first wallpaper and starts rotation when that folder is
ready. Cancel in the picker to leave setup as it was. If the check takes longer,
the folder stays saved and **Start wallpapers** lets you continue later.

To use online images, open the Wallhaven search editor in **Sources**. Read the
network disclosure and use **Enable Wallhaven and add source** to opt in to
safe-for-work discovery. Opening or canceling the editor has no effect on
saved searches or enabled sources.

Search by name, path, query or type. Use **In rotation**, **Off** or **Needs
attention** to filter the list. Each row shows whether the source uses shared
or source-specific image rules. Click a source row to open its editor underneath.
Opening another editor or closing the row prompts you before discarding unsaved
edits. Searching and filtering keep the open editor available. **Save** and
**Cancel** return focus to the row when it is visible.

Use a row’s **⋮** menu to include a source in rotation, take it out of rotation
or remove it. The menu works with the editor open or closed. An **Out of
rotation** label and a pause badge mark a source you have turned off. Removing
a source requires confirmation and keeps original files and wallpaper history.

Type a **Folder path** or use **Browse…** to pick one. Expand **Folder options**
for local image filters and scan settings.

In a Wallhaven search, leave **Search terms** blank to browse by filters alone.
Open **Filters** for tags, categories, and colors. Choose **Any (OR)** to match
at least one selected tag or **All (AND)** to require every selected tag.
New searches default to Any. Older searches keep their saved behavior. Enter
a tag name and press Enter or click **Add tag**. Choose a
[bundled suggestion](providers/wallhaven-tags.md) or **Browse Wallhaven tags**
to look up other tags. With no categories selected, Panora searches all
three safe-for-work categories.

**Add color** opens the Wallhaven swatches, a custom color control, and
**Use theme palette**. The palette action copies the current Omarchy swatches
into the draft. You can select up to six colors and remove each one before
saving. Wallhaven matches any selected color using its closest supported
swatches.

Open **Image rules** to see the effective shared rules from Settings or
choose **Set for this source**. Under **Resolutions**, require one minimum
size or choose several exact sizes. **My screen** copies the pixel dimensions
of one detected display. Under **Aspect ratios**, select preset or custom
ratios, or use **Same shape as my screen** with the pixel tolerance from
Settings. **Similar to my screen** retains the legacy 5% rule.

Open the **Omarchy theme** row to see the active theme, its palette, and the
total and eligible still counts. Use **View all** to browse the larger
collection. The common **Image rules** control starts with shared rules and
lets you save a custom minimum size or aspect ratio for this source.
In Automatic presentation, native theme stills fill the display as they do in
Omarchy. Choose Fit to show the whole image without cropping.

Use **Rename** to set a display name or clear it to show the search terms or
**Wallhaven**. Use **More settings** to adjust sort direction, pages and refresh
timing.

Open **Source troubleshooting** to see **Source status**. For each enabled
saved Wallhaven search, the catalog count shows how many candidates Panora has
discovered and how many are eligible under the saved rules and exclusions.
If the screen shape can't be resolved, the discovered total is still shown.
The latest check's inspected count is reported separately. Catalog counts
refresh after successful discovery checks while Source status is open. Use
**Refresh status** to read current counts on demand.

While editing valid image rules for a saved, enabled search, **With your unsaved
rules** shows a catalog estimate after a short pause. The saved count stays
visible. The estimate reports unknown images and partial checks, and uses the
existing catalog without exclusions. Save changed search criteria before checking
the affected catalog. Saving or discarding the draft clears the estimate.

**Show Wallhaven URL** gives you a browser URL you can copy. Panora applies
local eligibility rules after discovery and validates originals when selecting
wallpapers. The **Check sources** shortcut on Now opens and focuses the
disclosure.

A rejected rotation change retains the saved value and offers **Retry** in
the row’s actions. A revision conflict
or uncertain save requires **Reload settings** before another edit. Reload
preserves edited fields and refreshes untouched fields.

### Installed providers

Providers with a compatible [editor manifest](providers/editor-manifest.md)
appear under **Add source**. Panora renders their fields and keeps a rejected
or stale draft open for correction. Choose **Use shared rules** or **Customize
for this source** in **Image rules**. For installed-provider suggestions,
enable the prefix-sharing option in the editor.

## Recovery and accessibility

Connection and settings-load problems remain visible across screens.
The window keeps labeled last-known details through a disconnect and disables
actions that need fresh state. A failed save is never replayed automatically.
Keyboard focus returns to Retry or the active heading after explicit recovery,
unless it has already moved elsewhere. Errors identify their fields and open
collapsed options before focus moves.

If a wallpaper application is unconfirmed, inspect the desktop. Use **Next**
or **Previous** on **Now** to establish a confirmed wallpaper before you resume
rotation. If no wallpaper has been accepted yet, use **Next wallpaper** on
**Now** to recover the first change.

Closing the window leaves installed playback running. Use the installed
service controls to stop it. The
[frontend guide](frontend.md#review-the-native-window) covers keyboard,
accessibility and narrow-window development checks.

## Omarchy bar

The packaged service adds the button on Omarchy 4.5. It appears while the
service runs, including at login if you enable **Start Panora at login**.

The [optional bar plugin](../integrations/omarchy/README.md) provides a bounded
preview of the last accepted image and a row with Previous, Play/Pause and Next.
The bar icon and popup follow the active Omarchy theme as it changes. Right-click
the button, or press Menu or Shift+F10 in the wallpaper popup, for **Open Panora**,
**About Panora**, **Hide from bar**, and **Quit Panora**. Hide disables the bar
widget and leaves rotation running. Re-enable it in Omarchy's Plugins settings.
Quit stops the background service, closes its open Panora windows, and hides the
button. The wallpaper and startup preference persist. To keep the service from
starting on the next login, clear **Start Panora at login** in Settings before
quitting. When an apply result is uncertain,
use Next or Previous to recover before resuming automatic rotation. Use the full
window for setup, Sources, History and Settings.
