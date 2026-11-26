# Build and verify Panora on macOS

## Build and install

Install Xcode Command Line Tools and the pinned tools from
[development setup](../CONTRIBUTING.md#local-development), including Python
3.11 or newer. The installed bundle runs independently of the build tools.
From the repository root:

```sh
mise exec -- python3 ui/package_macos.py
```

Copy `build/Panora.app` into `~/Applications` using Finder, then open it. Keep the
complete bundle together. Paths containing spaces work. To build directly into a
chosen location, stop the previous service and quit its client first:

```sh
mise exec -- python3 ui/package_macos.py \
  --output "$HOME/Applications/Panora.app" --version 0.1.0
```

The packager builds the client, service, menu agent and wallpaper helper from
one private source snapshot. It verifies the native architecture and local signatures
before replacing a prior Panora bundle. A failed build preserves the previous
app.

The Space observer runs from
`Contents/Helpers/Panora Space Observer.app` under the separate
`org.panora.space-observer` background identity. Its AppKit event loop receives
real desktop-switch notifications without registering another Panora client.
The packager signs the helper executable and nested bundle before the outer app;
keep this helper with the complete app when installing or upgrading.

The menu agent runs from `Contents/Helpers/Panora Menu.app` under the
`org.panora.menu` accessory identity. The installed service starts and joins
it. The menu remains available when the main window quits.

The build targets the current Mac's architecture and sets the minimum macOS
version to that build host's version. Local builds use ad-hoc signatures.
`Contents/Resources/BuildInfo.plist` records version, revision, dirty-source flag,
source snapshot hash, architecture, SDK and compiler/macOS versions. The signed
bundle includes Panora's `LICENSE` and `THIRD-PARTY-NOTICES` in
`Contents/Resources`.

## Prepare a macOS release archive

For a release alongside the [Arch package](arch.md#build-from-a-recorded-revision),
choose the same committed source revision and the desired release version. On a
Mac with the pinned tools, run from the repository root:

```sh
mkdir -p build
mise exec -- task release:macos REVISION=HEAD VERSION=0.4.0 OUTPUT="$PWD/build/macos-release"
```

Use a new output directory with an existing parent. The task builds the chosen
revision in a temporary detached checkout, packs a ZIP, extracts it, and runs
the [headless bundle preflight](#headless-bundle-preflight) on the extracted app.
A successful build produces `panora-vVERSION-REVISION-macos-ARCH-adhoc.zip`,
`SHA256SUMS.macos-ARCH`, and `RELEASE-RECEIPT.macos-ARCH.json`. The receipt records
the full source revision, ZIP SHA-256, component hashes, build environment and
configured minimum macOS. The release task leaves the checkout and existing
output directories intact. See the [release guidance](releases.md) before distributing
the verified archive alongside the Arch package.

Verify the ZIP before attaching or installing it. For an Apple silicon build:

```sh
shasum -a 256 -c SHA256SUMS.macos-arm64
```

Run the command beside the ZIP. Extract the archive and use Finder to copy
`Panora.app` to `~/Applications`. Follow the
[replacement instructions](macos.md#data-replacement-and-removal) for an upgrade.
The archive is native to the build Mac's architecture, either `arm64` or
`x86_64`. Check the receipt for the macOS minimum and record verified platforms
in release notes. Attach only verified outputs for the approved release scope.

These archives use ad-hoc signatures. Gatekeeper may block a downloaded copy.
Avoid implying Apple notarized an ad-hoc archive in release notes. A notarized
release requires a Developer ID Application signature, Hardened Runtime and an
Apple notarization ticket. [Apple's notarization guide](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution/)

## Update the Homebrew formula

The public repository also serves as the Homebrew tap. The formula pins the
release tag and its full source commit. After a stable GitHub release,
the **Homebrew formula update** workflow opens the PR and attempts a protected
merge once verification passes. If another workflow publishes a release with
`GITHUB_TOKEN`, dispatch **Homebrew formula update** from `main` with the
published tag as `release_tag`.

See [release preparation](releases.md#update-the-homebrew-formula) for the
GitHub App setup, retry instructions and manual approval path.

The PR checks build and test the formula on an Apple silicon Mac. They also
upload the verified ad-hoc ZIP, checksum list, and build receipt for the same
tag. After merging the formula PR, users can run `brew update` and
`brew upgrade panora`.

## CLI-only development

Install Apple's Command Line Tools. From the repository root, build the CLI
and its compiled Swift wallpaper helper.

```sh
mkdir -p bin
mise exec -- go build -o bin/panora ./cmd/panora
swiftc -sdk "$(xcrun --show-sdk-path)" tools/macos-wallpaper.swift -o bin/panora-macos-wallpaper
```

Keep the helper beside `panora`. With `go run`, put it on PATH. Choose
`desktop.backend = "macos"` for explicit selection. The stand-alone service
also uses this helper for Space events.

## Headless bundle preflight

After building, run the repository's developer check against the complete bundle:

```sh
mise exec -- python3 tools/check_macos_bundle.py /absolute/path/Panora.app
```

It copies the app into a private path with spaces, verifies that exact copy's
identity, native components and signature, then exercises its CLI with a fresh
zero-source paused profile. Startup, duplicate ownership, Stop, disconnected
status, restart/SIGTERM and invalid-settings preservation are bounded and isolated.
Only its own process groups and temporary files are removed. It does not install
or open the graphical client, register login startup or apply a wallpaper. It
verifies the menu agent's identity, signature and architecture without starting
it. The bundled background-only Space observer can run as an owned child. The macOS CI
job runs this preflight against the extracted release archive.

Use `--inspect-only` to check bundle metadata, component hashes, architecture and
signature. JSON output records the artifact identity, checks and limits. For a
native window trial with a fake wallpaper setter, follow the
[isolated native review](frontend.md#isolated-native-review). The
[macOS verification record](verification.md#macos) describes the scope of
installed desktop trials.
