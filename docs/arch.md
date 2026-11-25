# Arch package for Omarchy

Panora’s Arch x86-64 package targets the Omarchy/Hyprland graphical session and
its native background handler. Pacman installs GTK 3, WebKitGTK 4.1 and
`xdg-utils` as dependencies. Panora uses `xdg-open` to open links and locations.
The optional bar integration uses `socat`.

## Install, upgrade and remove

Build from a [recorded revision](#build-from-a-recorded-revision). Inspect
the resulting package with `pacman -Qip` for dependencies and `pacman -Qlp`
for files. Install the package path printed by `makepkg --packagelist` with
`sudo pacman -U`.

Open **Panora** from the Omarchy application launcher. It connects to a
compatible Panora service or starts the packaged systemd user service and
waits for its private socket. The service checks the graphical session and
Omarchy background handler before it starts. On a fresh profile, Panora enables
the active **Omarchy theme** source and starts rotation once the native intro
has finished and a still is ready. Add folders and searches in **Sources**.
Closing the window leaves the service running.

On Omarchy 4.5, starting the packaged service adds the Panora button to the
bar on first use, including for existing profiles that have no widget.
**Start Panora at login** also starts the button without opening the window.
The button hides when the service stops. Use **Setup → Plugins** in Omarchy
to disable or enable the button. Starts and upgrades preserve that choice
and the button's position.

In **Settings → Startup → Background service**, the button shows **Start
service** when the service is stopped and **Stop service…** while it is
running. Panora checks the state automatically, including after Stop.
**Start Panora at login** is off by default. Enabling it links only this user's
unit to the graphical session. Turning it off keeps the current service
running. Use the user journal and Panora’s diagnostics to check startup and
wallpaper readiness.

```sh
journalctl --user -u panora.service -b
/usr/bin/panora doctor
```

For a compatible upgrade, build the package from the newer revision and install
it with `sudo pacman -U`. After a successful transaction, the hook restarts each
verified running stock service through its systemd user manager. The desktop
window can stay closed. A stopped service stays stopped and **Start Panora at
login** keeps its current setting. Saved sources,
preferences, history, exclusions, and paused playback remain in the user
profile. When the service starts with the new package, it asks the running
Omarchy Shell to refresh the bundled widget. Its enablement, position and
settings carry over. For a stopped service, this happens at its next start.
Foreground and customized units keep their own lifecycle. An offline
installation leaves live user sessions alone. See [verification](verification.md#omarchy-and-linux)
for tested scope.

If a restart fails, pacman prints the affected user ID after installing the
package. Panora writes a widget refresh failure to its user journal. In that
user's graphical session, check the journal and retry:

```sh
journalctl --user -u panora.service -b
systemctl --user restart panora.service
```

The launcher uses the desktop ID `io.github.vadim82.panora.desktop`. If a
shortcut references the earlier `vadim82.panora.desktop` ID, update that
reference. If you previously installed the optional widget, follow the
[bar migration steps](../integrations/omarchy/README.md#migrate-an-existing-bar-entry).

Remove Panora with pacman.

```sh
sudo pacman -R panora
```

Removal asks each stock packaged service running this version to shut down and
release its database and private socket. The transaction waits up to 30 seconds
and prints a scriptlet error if it cannot verify shutdown. Pacman still
removes the package in that case. Foreground services and custom units keep
their own lifecycle. The per-user **Start at login** choice
remains saved. While the package is absent the activation link has no target.
After reinstall, an enabled profile starts at the next graphical login.

Settings, playback history, exclusions, caches and original images stay in
their [per-user locations](configuration.md#default-file-locations). If you
installed the optional bar entry, its layout belongs to your shell profile.
Follow the [shell removal steps](../integrations/omarchy/README.md#remove)
to remove that entry.

See [CLI diagnostics](cli.md#inspect-images-and-desktop-readiness) for help
interpreting readiness failures.

## Installed paths

| Path | Purpose |
| --- | --- |
| `/usr/bin/panora` | Service, CLI and bundled provider commands |
| `/usr/bin/panora-ui` | Native client |
| `/usr/lib/systemd/user/panora.service` | Graphical-session user service |
| `/usr/share/libalpm/hooks/90-panora-restart.hook` | Post-transaction upgrade hook |
| `/usr/lib/panora/package-restart` | Verified, conditional user service restart |
| `/usr/share/applications/io.github.vadim82.panora.desktop` | Launcher |
| `/usr/share/licenses/panora/LICENSE` | Panora's MIT license |
| `/usr/share/licenses/panora/THIRD-PARTY-NOTICES` | Bundled dependency license and notice texts |
| `/usr/share/icons/hicolor/*/apps/panora.{png,svg}` | Canyon Passage launcher icon at standard and scalable sizes |
| `/usr/share/panora/omarchy/io.github.vadim82.panora/` | Quickshell plugin manifest and release-specific runtime assets |
| `/var/lib/panora-package-lifetime` | Package-owned lifecycle marker for managed service shutdown |

The service registers the read-only Quickshell assets at runtime. Use
`/usr/bin/panora` for optional shortcuts and
[provider diagnostics](cli.md#provider-diagnostics). See the
[Omarchy shell client guide](../integrations/omarchy/README.md#packaged-widget).

## Build from a recorded revision

For development from a checkout, see
[Native development on Omarchy](frontend.md#native-development-on-omarchy).

Use a clean, up-to-date Arch x86-64 build environment with `base-devel` and a
regular build user. With Task, Git, a full checkout and the build dependencies
available in that environment, run:

```sh
mkdir -p build
task release REVISION=HEAD OUTPUT="$PWD/build/arch-release"
```

Choose the revision explicitly and use a new output directory. The task builds
an unsigned package with the commit epoch and runs the package checks. It
records the archive and source checksums in `SHA256SUMS`, with `BUILDINFO`,
`PKGINFO` and `RELEASE-RECEIPT.txt` beside the package.
If the build is interrupted or fails, use a fresh output directory for the retry.
Review the generated package and checksums before distribution.

To prepare the source on a different machine, use Git and a full
Panora checkout. Run the following from the repository root, choosing the commit
to build and an unused output directory:

```sh
mkdir -p build
packaging/arch/prepare-snapshot HEAD "$PWD/build/arch-package"
```

The command prints the full commit, package version and `SOURCE_DATE_EPOCH`
from the commit timestamp. It puts the committed
source archive, package install script and generated PKGBUILD with both
SHA-256 checksums from that revision in the build directory. Local changes
and generated files in the checkout are excluded. Tagged release packages
use the release version and a pacman epoch of 1, which orders them after
earlier revision-numbered packages. Untagged development packages use the
first-parent commit count and abbreviated commit ID. The release archive
filename omits the epoch, and pacman reads it from the package metadata.

The package embeds that Arch version in **Settings → About Panora**, alongside
the release version when the commit has an exact `vX.Y.Z` tag. When preparing a
release before tagging, set `PANORA_RELEASE_VERSION=X.Y.Z` to the planned tag
without the `v`. The package check compares the binary's identity with the
recipe. `panora-ui --version` reports the same fields without opening a window.

Copy that directory to the clean Arch environment when building separately.
With permission to install dependencies **in that build environment**, build
as the regular user from the prepared directory:

```sh
SOURCE_DATE_EPOCH=COMMIT_TIMESTAMP makepkg --syncdeps --cleanbuild
makepkg --packagelist
```

Replace `COMMIT_TIMESTAMP` with the value printed by `prepare-snapshot`.
The commit timestamp fixes package metadata and file timestamps across rebuilds.

The recipe needs network access for the Go module cache and the locked
frontend dependencies. It uses Arch's Go (at least 1.27.3), Node, Corepack and
desktop-file-utils. Corepack selects the pnpm version pinned in
[`ui/package.json`](../ui/package.json). The React bundle is built and
embedded in the native binary. Go, Node, Corepack and Python are unnecessary
when running the installed package. Record the exact Arch dependency versions,
source revision, and artifact SHA-256 when comparing builds. For a byte
comparison, use the same source archive, generated recipe, signed Arch
dependency versions, build paths and makepkg settings for both builds. Save
the first package outside the build directory, rebuild with a clean source
tree and Go compilation cache, then compare both package hashes and retain
the `.BUILDINFO` receipt.

With Arch's default makepkg settings, the native UI uses GCC link-time
optimization and retains a content-derived GNU ELF build ID for native
debugging. The UI omits the Go action-based build ID, which varied across
clean builds with GCC LTO. Its Go module and toolchain information remain
available through `go version -m`. The CLI retains its Go build ID.

### Review bundled notices

The build creates `THIRD-PARTY-NOTICES` from the linked Go modules, Go
toolchain, and packages and build-tool helpers rendered into the production
frontend. It copies the upstream license, patent and notice files at those
package paths. Review
the installed notice inventory when Go modules, the Go toolchain or the
frontend lockfile change. To regenerate it using locally built binaries
after `mise exec -- task dev:build`, run:

```sh
mise exec -- node ui/tools/bundled-notices.mjs \
  build/panora build/panora-ui build/THIRD-PARTY-NOTICES
```

Panora uses a CSS backdrop and keeps the Radix confirmation dialog's focus
and dismissal behavior. A pinned patch marks the unused Radix overlay
initializer as pure so the production bundler can remove that export and
its scroll-lock dependencies. The production notice inventory follows the
rendered code. The pnpm lockfile still resolves
`react-remove-scroll-bar@2.3.8`. Its exact copyright notice remains
unconfirmed for any distribution that includes the npm tarball.
[Npm release metadata](https://registry.npmjs.org/react-remove-scroll-bar/2.3.8) ·
[Upstream attribution discussion](https://github.com/theKashey/react-remove-scroll-bar/pull/76)

The generator requires the Go toolchain's LICENSE and PATENTS. On Arch it
uses the Go package's copy of LICENSE and, when PATENTS is absent from the
SDK, the pinned upstream text for Go 1.27.3 and 1.27.4. Both versions have
byte-identical PATENTS texts. [Go 1.27.3](https://github.com/golang/go/blob/go1.27.3/PATENTS) ·
[Go 1.27.4](https://github.com/golang/go/blob/go1.27.4/PATENTS)

The notice file covers code bundled in the Arch binaries. System libraries
such as GTK and WebKitGTK come from Arch packages. Review the source archive
and separate artwork terms before distribution.
