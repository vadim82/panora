# Verification

See [platform status](status.md), [development checks](../CONTRIBUTING.md#run-checks)
and [installed release checks](installed-release-checklist.md).

## Omarchy and Linux

| Area | Verified scope |
| --- | --- |
| First use and daily playback | On an installed r135 package, the stock launcher, keyboard folder selection, local and Wallhaven rotation, paused manual playback, History, and recovery from a missing folder and a provider fault passed on one physical 5120 × 1440 monitor at scale 1. Centered Fit and cache protection passed on the same display. |
| Installed Wayland and shell | Guest checks covered normal and narrow folder setup, canceling the picker, stock bar controls and reconnect, theme changes and scaling at 1.25 on a single virtual output. A guest confirmed Wallhaven consent without running live discovery in that onboarding check. |
| Login and package lifecycle | Login with startup enabled passed on physical hardware and in a guest. Disabled startup was checked by actual guest login. Isolated KVM checks covered ordinary removal and reinstall and an upgrade from v0.2.0 for running, stopped and custom services, plus offline installation and restart-failure recovery. |
| Rotation recovery | The stock renderer showed one overdue change followed by ordinary rotation after the packaged r150 service was paused and resumed as a process. Physical suspend and resume were observed with a fixture setter. Physical suspend with the installed service remains unverified. |
| Fit and mixed sources | A single 5120 × 1440 virtual display was used to test mixed local and Wallhaven playback, request filtering, warm reuse and decoded image rules. |

### Now preview angles

The [canyon preview](../assets/screenshots/panora-now.png)
defines the intended silhouette. In an isolated WebKitGTK 2.54.1 browser
fixture at 960 × 760, the original side previews rendered as rectangles.
The updated previews taper toward the center and match the reference's
outer width and height to within a few pixels. The Linux treatment clips the
card silhouette, so the image inside the clip retains its flat projection.
A 32:9 crop at 1155 × 922 kept the focused Next control visible beside the
center image and its menu. At 480 px wide the side previews stacked below
the center image. The menu opened with Next focused, and Next followed by
Previous returned to the original image. These checks used the development
fixture and a private virtual X server.

### README recording

The [full recording](../assets/screenshots/panora-demo.mp4) used the v0.6.9 Arch
package from revision `51153171a595f167700538aa6a460205c5cf9eb9`. The release
checksum matched, and `pacman -Qkk panora` reported 65 files with zero changes
in an isolated Omarchy 4.5.0 guest at 1600 × 900. The
[Cover Flow loop](../assets/screenshots/panora-cover-flow.gif) is an excerpt
cropped around the window.

The artwork comes from the same three Canyon Passage views as the preview
above, identified by `ui/frontend/src/dev/canyon-{left,center,right}.png` at
revision `62c28e40c61a909bbcb2daec63d6ca4b64461f0d`. The guest used 16:9 JPEG
copies. The side views were cropped inside their white corners before
resizing. The capture shows both side previews filled before and after Next,
with the desktop changing to match. It also shows a 16:9 Wallhaven search
draft, History replay, and Previous from the bar with the app window closed.
The guest was offline and the Wallhaven draft was canceled.

In rehearsal, the native window segfaulted during a paste attempt in the
guest. The process output showed the GTK main loop through Wails. The guest
had no core dump. The service and current wallpaper persisted. The recording
uses typed input.

### Fit encoding comparison

Five fresh renders per case on virtual displays at 5120 × 1440 and 1600 × 900
compared the default and uncompressed PNG encoders at revision
`ada61fe7b61851d49d469723ae21b8d4bb0296c9`. At 5120 × 1440, median time to
visible output fell from 1,934 to 1,586 ms for the synthetic JPEG and from
1,350 to 1,123 ms for the synthetic PNG. The decoded pixels matched and an
uncompressed render at that size took about 21 MiB. These timings describe
the tested inputs and virtual display. The cache tests cover retention of
the displayed and pending renders under byte and file-count pressure.

### Wallhaven monitor ratio comparison

An installed test using synthetic SFW responses on the 5120 × 1440 virtual
display reduced discovery from three metadata pages to one with a ratio
filter. Warm replay made zero detail or original requests in each compared
trial. The guest test measured Fit preparation separately. Its timings do
not characterize Wallhaven’s production network.

The [theme-source tests](../internal/playback/omarchy_theme_test.go) cover image
geometry, invalid and animated files, shared and custom rules, history,
exclusions and stale-generation rejection. The [native adapter tests](../internal/desktop/omarchy_test.go)
exercise stock Omarchy 4.5 and sequenced shell dispatch. The
[gallery tests](../ui/frontend/src/sources/OmarchyThemeEditor.test.tsx) cover
keyboard interaction and 10- and 100-still collections.

## macOS

Installed arm64 bundles 0.1.0 and 0.1.1 were tested on macOS 27.2 in an Apple
silicon virtual machine with one 1280 × 800 point display at 2×. Checks covered
local and Wallhaven playback, window and menu bar controls, Fit, login and
reboot, replacement and removal, and following and restoring two visited
ordinary Desktops. Full-screen Spaces and external displays remain unverified.

An isolated foreground service built at `dcc76a7169298de86fc745daccd45f28b95f9b1e`
was tested on physical macOS 27.2.1 with a built-in Retina display. Active
rotation resumed with one overdue change followed by the normal interval.
Paused playback retained its image and cursor across wake. Cached local
replay remained available during a Wallhaven outage, and discovery retried
after connectivity returned. This physical check covered service recovery.

Automated menu agent checks use private sockets and a fake wallpaper setter.
They cover duplicate menu ownership, window close and reconnect, service
replacement, Stop acknowledgement during a playback request, and agent exit
after external shutdown. The bundle preflight verifies the signed accessory
agent.

The service-owned menu at `1b7e976c29aba594df1d9f7ccc4ff316641495e7`
passed an installed arm64 trial on macOS 27.2 (26B123), with one
1280 × 800 point display at 2×. Guest observations covered unavailable
setup controls, Previous/Next and Pause/Resume, About and Open, window Quit
and reopen, repeated app launches and rejected duplicate helper ownership.
Actual logout/login with window restoration disabled started one service and
menu before any window client. Menu Stop and external CLI Stop removed the
icon and service while retaining the login preference and settings.

This guest used a separately recorded compatibility copy: only the three
bundle minimum-version declarations changed from the build host's 27.2.1 to
27.2, followed by ad-hoc re-signing. Executable code and embedded frontend
remained from the recorded revision; the executable deployment minimum was
already 27.2. A separate guest-only delayed setter trial kept Stop available
during playback. Stop preserved uncertain intent without committing history;
the original helper and valid bundle signature were restored afterward.
Cold guest startup included helper restarts and a Space-observer readiness
timeout; once settled, five status reads took 9–19 ms with a stable menu
process. These checks do not establish a cold-start latency guarantee.

The service regression tests cover retry after an observer readiness timeout,
automatic catch-up after reconnect, the maximum retry delay, and cancellation
on shutdown. The child protocol test checks that readiness is published only
after the expected handshake.

An Apple M2 Pro benchmark with a synthetic 5120 × 2880 JPEG measured five
verification passes per path. The prior full decode and cache reuse averaged
308 ms per pass. The accepted-file hash check averaged 6 ms. This compares
cache work only. The macOS setter and renderer still run on the active Space.
Space refresh logs include service-side elapsed time through acknowledgement.

The macOS Close behavior uses the window app's native Quit path. A headless
AppKit check exercised the pinned Wails window delegate on an unshown window
and confirmed that Close requests Quit. The menu remains owned by the separate
service, whose lifecycle checks cover keeping playback running when the window
client exits.

## Window and accessibility checks

Review Light and Dark at 960 × 760 and 480 × 570 in the
[browser preview](frontend.md#browser-preview). Check keyboard navigation,
focus restoration and dismissal, overflow, and readable controls. Use an
isolated native session to check the native picker, menus and shortcuts.
Broader assistive-technology coverage remains unverified.

Browser hit testing at 960 × 760 and 480 × 570 covered the open Image actions
menu with both angled neighbors available. At normal width, all nine action
sample points were reachable, including six that overlapped the Next card.
Escape dismissed the menu and returned focus to the three-dot button.
Headless WebKit 27.2 reproduced both dead clicks before the focus fix. With
the fix, View on website invoked the origin bridge and Picture settings opened
the settings page at normal and narrow widths. The current-image menu exposes
only actions for the displayed image.

### Navigation during a configuration read

Controlled bridge responses hold `Status` and `Configuration` independently. Next and Previous
wait for status, then dispatch while configuration is still pending. A changed
service instance, accepted entry or unavailable previous cursor prevents
dispatch. Late configuration updates the live settings projection and keeps
the reviewed settings and accepted navigation result. The coordinator and
window suites also exercise reconnect, draft preservation and preview
invalidation. This verifies dispatch ordering at the window bridge.

### Warm playback cache reuse

A benchmark compared the complete warm cache reuse operation on an Apple M2 Pro (Darwin arm64) with baseline
`b07a8ef5ff25e38516c051c8c512ff30507e83e7`. Both runs used the
7680 × 2160 generated JPEG from the large wallpaper preview benchmark
(1,772,972 bytes), with setup outside the timed loop. Median of five runs:

| Cache reuse | Time per operation | Allocated bytes per operation | Allocations per operation |
| --- | ---: | ---: | ---: |
| Full decode | 107.10 ms | 24,960,150 | 52 |
| Verified bytes and matching recorded dimensions | 0.81 ms | 54,744 | 44 |

Both runs included full SHA-256 verification, cache enumeration, capacity
accounting and refreshing recency. Preview generation, catalog I/O and desktop
acknowledgement were outside the timed operation.

### Cover Flow and upcoming previews

Compared installed arm64 clients built from `1b7e976c29aba594df1d9f7ccc4ff316641495e7`
and `31942d91f1da242c5432eb163b48311a8849e2b5` in a macOS 27.2 virtual
display at 1280 × 800 points and 2× scale. For the guest test, the copies'
bundle minimum-version metadata was changed from 27.2.1 to 27.2 and they were
re-signed. Both clients used the same private service with synthetic images
and a fake setter that waited 2.3 seconds before acknowledging a switch.

The previous client first showed center movement in a video sample about
2.5 seconds after dispatch. The updated client showed movement by the first
sample at 179 ms after input, while status still reported the previous
accepted image and cursor. A delayed rejection restored that accepted image,
kept its History entry, and displayed unconfirmed-state recovery. Browser
tests also cover reduced motion, repeated keyboard input and focus.

The private socket returned the first usable prepared thumbnail 76 ms after
the first Next response in this fixture. A separate delayed-setter test
verified that the second thumbnail was warm before dispatch and was
promoted after acceptance. A blocked-provider test verified that a source
rule change discarded in-flight preparation.

### Repeated Cover Flow navigation

Compared `4ab3b7e` and `3685356` at 960 × 760 using the same populated
frontend fixture in headless WebKit on macOS 27.2.1 and WebKitGTK 2.54.3 on
Arch x86-64 under emulation. A fixture setter acknowledged after 750 ms.
The cold fixture held the target preview for 160 ms. The contended fixture
returned busy during that period and let the window retry.

The table gives medians of three runs per case. First frame is the first
animation-frame callback with the new image ready and visible in the DOM.
Animation end and acknowledgement are measured from the same click. These
samples describe renderer behavior under a simulated bridge.

| Renderer | Target | First frame (ms) | Animation end (ms) | Acknowledgement (ms) | Target reads |
| --- | --- | ---: | ---: | ---: | ---: |
| macOS WebKit | Warm | 19 → 23 | 315 → 188 | 751 → 752 | 4 → 1 |
| macOS WebKit | Cold | 183 → 177 | 482 → 346 | 751 → 751 | 4 → 1 |
| macOS WebKit | Contended | 316 → 312 | 617 → 484 | 751 → 751 | 8 → 3 |
| Arch WebKitGTK | Warm | 102 → 104 | 365 → 286 | 755 → 755 | 4 → 1 |
| Arch WebKitGTK | Cold | 304 → 289 | 637 → 448 | 754 → 757 | 4 → 1 |
| Arch WebKitGTK | Contended | 370 → 409 | 694 → 573 | 759 → 756 | 7 → 3 |

The 170 ms animation finished sooner in all six cases. The first visible
contended frame arrived later in the Arch fixture. Installed compositor
timing remains unverified.

For a 7680 × 2160 JPEG, a repeated server preview took about 0.8 ms on the
Apple M2 Pro and 4.1 ms in the Arch container. Both repeated reads include a
full SHA-256 check. Cold reads, including decode and thumbnail generation,
took about 116 ms and 153 ms, respectively. A blocked download test returned
busy for a visible preview on the baseline. With staging separated from the
publication lock, both cold and warm reads finished while the download was
still blocked. A separate lookahead test held the second choice in its
provider and verified that the first preview was already readable.
