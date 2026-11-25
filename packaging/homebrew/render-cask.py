"""Render the private Homebrew cask from a published Panora release."""

import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import tempfile


REPO = "vadim82/panora"
TEMPLATE = Path(__file__).with_name("panora.rb.in")
RECEIPT = "RELEASE-RECEIPT.macos-arm64.json"


def gh(*args):
    return subprocess.run(
        ["gh", *args], check=True, stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE, timeout=60,
    ).stdout


def render(version):
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
        raise ValueError("Choose a three-part release version, for example 0.4.0.")
    tag = f"v{version}"
    release = json.loads(gh("api", f"repos/{REPO}/releases/tags/{tag}"))
    if release["tag_name"] != tag or release["draft"] or release["prerelease"]:
        raise ValueError("Publish the matching stable GitHub release first.")
    revision = json.loads(gh("api", f"repos/{REPO}/commits/{tag}"))["sha"]
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("The release tag did not resolve to a commit.")

    assets = {asset["name"]: asset for asset in release["assets"]}
    raw = gh("release", "download", tag, "--repo", REPO, "--pattern", RECEIPT, "--output", "-")
    if assets[RECEIPT]["digest"] != f"sha256:{hashlib.sha256(raw).hexdigest()}":
        raise ValueError("The downloaded receipt does not match GitHub's asset checksum.")
    receipt = json.loads(raw)
    build = receipt["artifact"]["build"]
    recorded = receipt["release"]
    archive = f"panora-{tag}-{revision[:12]}-macos-arm64-adhoc.zip"
    url = f"https://github.com/{REPO}/releases/download/{tag}/{archive}"
    digest = recorded["sha256"]
    minimum = recorded["minimum_macos"]
    if (build["Architecture"] != "arm64" or build["Version"] != version
            or build["SourceRevision"] != revision or build["SourceDirty"] is not False
            or build["Signing"] != "ad-hoc; local use only"
            or build["BuildMacOS"] != minimum or not re.fullmatch(r"[0-9]+\.[0-9]+(?:\.[0-9]+)?", minimum)
            or receipt["artifact"]["signature"] != "verified locally"
            or receipt["mode"] != "headless-smoke" or not receipt["checks"]
            or receipt["temporary_profile_removed"] is not True
            or recorded["archive"] != archive or not re.fullmatch(r"[0-9a-f]{64}", digest)
            or assets[archive]["browser_download_url"] != url
            or assets[archive]["digest"] != f"sha256:{digest}"):
        raise ValueError("The arm64 archive and preflight receipt must match the published release tag.")

    return (TEMPLATE.read_text()
            .replace("@VERSION@", version)
            .replace("@SHA256@", digest)
            .replace("@URL@", url)
            .replace("@MINIMUM_MACOS@", minimum))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version")
    parser.add_argument("output", type=Path)
    options = parser.parse_args()
    try:
        cask = render(options.version)
        with tempfile.TemporaryDirectory(prefix=".panora-cask-", dir=options.output.parent) as directory:
            staged = Path(directory) / options.output.name
            staged.write_text(cask)
            staged.replace(options.output)
    except (OSError, subprocess.SubprocessError, KeyError, TypeError, ValueError) as error:
        parser.exit(1, f"Panora cask generation failed: {error}\n")
    print(f"Panora v{options.version}: {options.output}")


if __name__ == "__main__":
    main()
