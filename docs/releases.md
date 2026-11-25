# Release preparation

Build and inspect an Arch package from a recorded revision using the
[Arch build guide](arch.md#build-from-a-recorded-revision). Verify the source
archive, package contents, third-party notices and checksums, and test the
package in an isolated environment before distribution.

See [macOS development](macos-development.md#prepare-a-macos-release-archive)
for archive preparation. Public ad-hoc previews document the supported
architecture, minimum macOS version, checksum, and Gatekeeper behavior in
the release notes. Notarized releases require Developer ID signing.

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

## Publish signed pacman updates

Publishing a stable GitHub release starts **Publish pacman repository**.
It checks the package name, checksum and source commit against the release,
signs the package and repository indexes, then deploys the signed files
to Panora's GitHub Pages site. The `gh-pages` branch retains the published
files. If the workflow needs a retry, run it from `main` with the same
published `vX.Y.Z` tag. An older release cannot replace a newer index.

The workflow uses only a dedicated signing subkey in the `pacman-signing`
environment. Keep the primary key offline. Before rotating the subkey, add
its public certificate to `arch/panora-signing-key.asc` on `gh-pages` and
allow existing users time to reimport the updated certificate. Replace the
`PANORA_PACMAN_SIGNING_SUBKEY` environment secret and
`PANORA_PACMAN_SIGNING_FINGERPRINT` environment variable together. Verify
the new subkey by dispatching the workflow against the current release.
