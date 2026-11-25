"""Build and verify a local-use Panora macOS ZIP from a selected Git commit."""

import argparse
import hashlib
import json
from pathlib import Path
import platform
import plistlib
import re
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
DITTO = "/usr/bin/ditto"


def command(args, *, cwd, **options):
    return subprocess.run(args, cwd=cwd, check=True, stdin=subprocess.DEVNULL, timeout=1800, **options)


def output(args, *, cwd):
    return command(args, cwd=cwd, stdout=subprocess.PIPE, text=True).stdout.strip()


def build_release(root, revision, version, destination):
    arch = platform.machine()
    if sys.platform != "darwin" or arch not in {"arm64", "x86_64"}:
        raise RuntimeError("Build on an Apple silicon or Intel Mac with Xcode Command Line Tools.")
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
        raise RuntimeError("Choose a three-part app version, for example 0.4.0.")
    revision = output(["git", "rev-parse", "--verify", "--end-of-options", f"{revision}^{{commit}}"], cwd=root)
    destination = destination.absolute()
    if destination.exists() or destination.is_symlink():
        raise RuntimeError("Select a new output directory.")

    platform_name = f"macos-{arch}"
    archive_name = f"panora-v{version}-{revision[:12]}-{platform_name}-adhoc.zip"
    with tempfile.TemporaryDirectory(prefix=".panora-release-", dir=destination.parent) as temporary:
        workspace = Path(temporary)
        source = workspace / "source"
        staged = workspace / "verified"
        staged.mkdir()
        bundle = workspace / "build/Panora.app"
        archive = staged / archive_name
        git = ["git", "-c", "core.hooksPath=/dev/null", "worktree"]
        command([*git, "add", "--quiet", "--detach", source, revision], cwd=root)
        try:
            command([sys.executable, "ui/package_macos.py", "--output", bundle, "--version", version], cwd=source)
            command([DITTO, "-c", "-k", "--sequesterRsrc", "--keepParent", bundle, archive], cwd=source)
            unpacked = workspace / "round trip"
            command([DITTO, "-x", "-k", archive, unpacked], cwd=source)
            extracted = unpacked / "Panora.app"
            receipt = json.loads(output([sys.executable, "tools/check_macos_bundle.py", extracted], cwd=source))
        finally:
            command([*git, "remove", "--force", source], cwd=root)

        if (receipt["mode"] != "headless-smoke" or not receipt["checks"]
                or receipt["temporary_profile_removed"] is not True):
            raise RuntimeError("The extracted archive did not complete the isolated headless preflight.")
        contents = extracted / "Contents"
        with (contents / "Info.plist").open("rb") as file:
            minimum = plistlib.load(file)["LSMinimumSystemVersion"]
        build = receipt["artifact"]["build"]
        if (build["SourceRevision"] != revision or build["SourceDirty"] is not False
                or build["Version"] != version or build["Architecture"] != arch
                or build["BuildMacOS"] != minimum or build["Signing"] != "ad-hoc; local use only"):
            raise RuntimeError("The verified archive did not match the selected build identity.")
        for name in ("LICENSE", "THIRD-PARTY-NOTICES"):
            notice = contents / "Resources" / name
            if not notice.is_file() or not notice.read_text().strip():
                raise RuntimeError(f"The verified archive is missing {name}.")

        with archive.open("rb") as file:
            digest = hashlib.file_digest(file, "sha256").hexdigest()
        (staged / f"SHA256SUMS.{platform_name}").write_text(f"{digest}  {archive_name}\n")
        receipt["release"] = {"archive": archive_name, "sha256": digest, "minimum_macos": minimum}
        (staged / f"RELEASE-RECEIPT.{platform_name}.json").write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")
        # Reserve the new destination without replacing another build's files.
        destination.mkdir()
        staged.rename(destination)
    return destination / archive_name


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("revision")
    parser.add_argument("version")
    parser.add_argument("output", type=Path)
    options = parser.parse_args()
    try:
        archive = build_release(ROOT, options.revision, options.version, options.output)
    except (RuntimeError, OSError, subprocess.SubprocessError, KeyError, ValueError) as error:
        parser.exit(1, f"Panora macOS release failed: {error}\n")
    print(archive)
    print("Ad-hoc signature for local use. See the release receipt for the revision, architecture and minimum macOS.")


if __name__ == "__main__":
    main()
