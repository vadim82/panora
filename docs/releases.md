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
draft job refuses to reuse an existing release or tag. For a rehearsal,
select **Build and stage artifacts without creating a GitHub draft** and
download the staged files from the workflow run.

Review the draft notes and test the packages in isolated environments before
publishing. The macOS ZIP is an ad-hoc signed preview. Its receipt and draft
notes record the minimum macOS version, and Gatekeeper may block the download.
Publishing a stable release starts the Homebrew formula update.

## Update the Homebrew formula

The **Homebrew formula update** workflow opens a formula PR for the newest
published stable release. The PR changes only the release tag and pinned source
commit. The workflow reuses an existing PR on retry and checks the exact PR
head against the generated change. After the Go workflow and the Homebrew
install and archive checks pass, it attempts a normal protected merge. GitHub
still enforces branch protection, review requirements and conversation
resolution. The Actions run summary links to the PR and reports why a check,
review or older version blocked the merge.

To enable unattended updates, [register and install a private GitHub App](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/registering-a-github-app)
on this repository only. Grant it repository **Contents** and **Pull requests**
write permissions. It does not need a webhook, extra repositories or any
branch-protection exemption. Set the repository Actions variables
`PANORA_HOMEBREW_APP_CLIENT_ID` and `PANORA_HOMEBREW_APP_SLUG` (without
`[bot]`) and the Actions secret `PANORA_HOMEBREW_APP_PRIVATE_KEY`. The App
opens the PR so GitHub starts its ordinary checks. It also performs the
protected merge so GitHub starts the Go workflow on the resulting main commit.

Without the App credential, the workflow still opens the formula PR. GitHub
requires a maintainer to approve the workflow runs and merge the verified PR.
To retry, run **Homebrew formula update** from `main` with the same published
`vX.Y.Z` tag. The hourly recheck also handles reviews and conversations that
were still open when CI completed.

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
