# Release preparation

Build and inspect an Arch package from a recorded revision using the
[Arch build guide](arch.md#build-from-a-recorded-revision). Verify the source
archive, package contents, third-party notices and checksums, and test the
package in an isolated environment before distribution.

See [macOS development](macos-development.md#prepare-a-macos-release-archive)
for archive preparation. Public ad-hoc previews document the supported
architecture, minimum macOS version, checksum, and Gatekeeper behavior in
the release notes. Notarized releases require Developer ID signing.

## Draft a release on GitHub

Once the **Go** workflow passes on `main`, open **Actions → Draft release →
Run workflow**. Select `main` and enter the version as `X.Y.Z`. The workflow
builds the same commit for Arch x86-64 and Apple silicon, checks both receipts,
and attaches the packages, source archive, and checksums to one draft. The
workflow stops if a release or tag already uses that version.

Review the draft notes and test the packages in isolated environments before
publishing. The macOS ZIP is an ad-hoc signed preview. Its receipt and draft
notes record the minimum macOS version, and Gatekeeper may block the download.
Once the stable release is published, the Homebrew workflow prepares a patch
for the formula.

## Stage a shared release

Build the Arch package and macOS archive from the same Git commit and with
the same release version. The Arch build reads an exact `vX.Y.Z` tag on that
commit. For an untagged commit, set `PANORA_RELEASE_VERSION=X.Y.Z` when building
the package. Staging checks the version in the checksummed Arch recipe and
the macOS receipt.

```sh
mise exec -- task release:stage REVISION=HEAD VERSION=0.4.0 \
  ARCH_OUTPUT="$PWD/build/arch-release" MACOS_OUTPUT="$PWD/build/macos-release" \
  OUTPUT="$PWD/build/staged-release"
```

Use the revision and version from your builds and choose a new staging directory
whose parent exists. Staging groups the verified artifacts locally.
