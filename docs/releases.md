# Release preparation

Build and inspect an Arch package from a recorded revision using the
[Arch build guide](arch.md#build-from-a-recorded-revision). Verify the source
archive, package contents, third-party notices and checksums, and test the
package in an isolated environment before distribution.

Public macOS distribution requires Developer ID signing and notarization; see
[macOS development](macos-development.md#prepare-a-macos-release-archive).

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
whose parent exists. Staging groups the verified artifacts locally. An ad-hoc
macOS archive is for local testing.
