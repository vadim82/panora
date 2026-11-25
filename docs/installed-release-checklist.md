# Omarchy installed release checks

Build the [Arch package](arch.md#build-from-a-recorded-revision) and run
[the contribution checks](../CONTRIBUTING.md#run-checks) from a clean checkout.
Record the source revision and package checksum for each trial. Reuse a
controlled profile within each installed sequence and keep guest and physical
profiles separate.

## Choose an isolated test environment

Use the smallest environment that can establish the result the issue needs.

| Check | Start here |
| --- | --- |
| Package build, integrity, dependency notices, file placement or upgrade/removal of package-owned paths | Use a disposable Arch container such as Docker, or a signed-dependency build root, with the [recorded-revision recipe](arch.md#build-from-a-recorded-revision). This checks package contents and package-manager transactions. |
| Installed onboarding, launcher, daily window or shell workflows, graphical login and user-service lifecycle | Use a headless KVM guest with its own stock Omarchy/Hyprland session, systemd user manager, synthetic sources and exact package. Use guest-only input and capture the virtual output. |
| Real monitor pixels, hardware scaling or physical suspend | Use the guest for setup and regression checks. Coordinate an idle window with the owner for behaviors that need the physical target. Test one coordinated live experiment at a time and verify restoration before moving on. |

Start a guest from a verified Arch base with the target Omarchy dependencies.
Give it a private disk, account, sockets and evidence directory. Use a virtual
display and guest input through QEMU control scoped to the guest. Keep host
home, desktop configuration, compositor IPC, display sockets, input devices
and credentials out of the guest. Use synthetic media and enable only the
network access the test needs. Confirm the stock Apps entry, user manager,
service identity and background handler before testing. Record observations
at the boundary they establish. An accepted setter call and a visible
wallpaper are separate observations.

### Headless Omarchy guest

Use a signature-verified Arch bootstrap and signed packages to provision a
disposable guest disk. Install Omarchy with Hyprland, UWSM and Quickshell,
GTK3, WebKitGTK 4.1, `xdg-utils`, `socat`, `grim` and `imagemagick` in the
guest. Omarchy uses `magick identify` before decoding wallpaper images, so
check that command against a synthetic PNG as the guest desktop user.
For Arch QEMU packages with modular devices, include `qemu-system-x86`,
`qemu-hw-display-virtio-vga` and `qemu-hw-display-virtio-gpu` in the QEMU
environment. QEMU can run from a private package root with `/dev/kvm` exposed.

With a raw ext4 test disk and a matching kernel and modules, the validated
boot layout used the following command from the private VM directory. The
disk contains the disposable guest account and graphical login setup. `share/`
contains only synthetic images, artifacts and their checksums.

```sh
qemu-system-x86_64 -no-user-config -nodefaults \
  -machine q35,accel=kvm -cpu host -m 3072 -smp 2 -no-reboot \
  -kernel ./vmlinuz -append 'root=/dev/vda rw console=tty0 console=ttyS0,115200n8' \
  -drive file=./system.ext4,format=raw,if=virtio \
  -device virtio-vga,xres=1600,yres=900 -display none -nic none \
  -qmp unix:./qmp.sock,server=on,wait=off -vnc unix:./vnc.sock \
  -chardev socket,id=serial,path=./serial.sock,server=on,wait=off,logfile=./serial.log \
  -serial chardev:serial \
  -virtfs local,path=./share,mount_tag=qa-share,security_model=none,readonly=on
```

Use the serial socket for guest package commands and `loginctl` receipts.
Use QMP `send-key`, `input-send-event` and `screendump` for guest-only keyboard,
pointer and framebuffer evidence. Mount the artifact share read-only in the
guest, verify SHA-256 before `pacman -U`, and record `pacman -Qkk panora`.
For a login test check the new session ID, unit invocation, PID and closed UI
before launching the client. For a wallpaper test compare the virtual output
with the expected synthetic pixels. Shut down the VM and remove the owned
disk and sockets once the selected receipts are retained.

Keep host input and the physical seat outside automated guest and package
runs. Before any required physical trial, record the original wallpaper,
theme, focus, workspace and relevant service state. Wait for the owner’s idle
handoff and follow the [contribution guide](../CONTRIBUTING.md#desktop-verification)
for attended recovery and cleanup. Report the guest and physical observations
with their separate source and environment receipts.

## Installed workflow checks

- Open Panora from the stock launcher in a fresh profile with synthetic theme
  stills. Confirm that the Omarchy source is enabled and Panora displays its
  first wallpaper after native theme readiness. Add a folder from **Sources**
  using the keyboard and check the folder picker's Cancel action at normal and
  narrow window sizes.
- Exercise automatic rotation with the window closed, manual playback when
  paused, bar reconnect, History, exclusions, and recovery after temporarily
  removing a source folder or interrupting discovery.
- Check login with startup enabled and disabled. Verify window-closed
  playback, service Stop and Start, and the visible result after an overdue
  deadline. Record actual sleep and wake separately from process pause.
- Verify that an upgrade restarts the stock service if it was running and
  leaves it stopped otherwise. Check saved preferences, history and
  exclusions after upgrade, removal and reinstall.
- Open the old Omarchy wallpaper popup before upgrading. In the same shell
  process, reopen it and verify that the new controls appear. Check widget
  placement, settings, and enablement, including a deliberately hidden widget.
  Repeat with the service stopped, then start it and verify the new popup.
- Compare **Settings → About Panora** with the installed package and release
  identifiers. Verify keyboard copy and opening About while the service is
  stopped.

## Inputs for playback and Fit

Use two eligible images at the display’s pixel size, a third with a
deliberately mismatched aspect ratio, and a smaller asymmetric image to
check centering and the enlargement setting. For a 5120 × 1440 display,
use a 5120 × 1439 mismatch and a 1280 × 720 smaller fixture. Use an
explicitly enabled SFW Wallhaven search to check mixed rotation.
