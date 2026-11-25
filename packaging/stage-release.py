"""Stage verified Arch and macOS assets for the same Panora release commit."""

import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile


ROOT = Path(__file__).resolve().parents[1]


def verify_checksum(path, checksums):
    with path.open("rb") as file:
        digest = hashlib.file_digest(file, "sha256").hexdigest()
    entries = [line for line in checksums if line.endswith(f"  {path.name}")]
    if entries != [f"{digest}  {path.name}"]:
        raise RuntimeError(f"Checksum verification failed for {path.name}.")
    return entries[0] + "\n"


def arch_assets(arch, revision, version, staged):
    receipt = (arch / "RELEASE-RECEIPT.txt").read_text().splitlines()
    recorded = [line.removeprefix("Revision: ") for line in receipt if line.startswith("Revision: ")]
    if recorded != [revision]:
        raise RuntimeError("The Arch receipt does not match the selected source revision.")
    names = [line.removeprefix("Artifact: ") for line in receipt if line.startswith("Artifact: ")]
    package_versions = rf"(?:{re.escape(version)}|r\d+\.{revision[:12]})"
    if len(names) != 1 or not re.fullmatch(rf"panora-{package_versions}-\d+-x86_64\.pkg\.tar\.zst", names[0]):
        raise RuntimeError("The Arch receipt must name one x86-64 package for the selected commit.")
    checksums = (arch / "SHA256SUMS").read_text().splitlines()
    source_line = verify_checksum(arch / f"panora-{revision}.tar.gz", checksums)
    recipe = arch / "PKGBUILD"
    recipe_line = verify_checksum(recipe, checksums)
    release_labels = [line for line in recipe.read_text().splitlines() if line.startswith("_release_version=")]
    if release_labels != [f"_release_version='{version}'"]:
        raise RuntimeError("The Arch recipe was not built with the selected release version.")
    package = staged / names[0]
    shutil.copyfile(arch / package.name, package)
    package_line = verify_checksum(package, checksums)
    if any(receipt.count(line.rstrip("\n")) != 1 for line in (source_line, recipe_line, package_line)):
        raise RuntimeError("The Arch checksums do not match the build receipt.")
    (staged / "SHA256SUMS").write_text(package_line)


def macos_assets(macos, revision, version, staged):
    receipts = list(macos.glob("RELEASE-RECEIPT.macos-*.json"))
    if len(receipts) != 1:
        raise RuntimeError("Select one verified macOS release output directory.")
    receipt = json.loads(receipts[0].read_text())
    build = receipt["artifact"]["build"]
    arch = build["Architecture"]
    if (arch not in {"arm64", "x86_64"} or build["Version"] != version
            or build["SourceRevision"] != revision or build["SourceDirty"] is not False
            or receipt["mode"] != "headless-smoke" or not receipt["checks"]
            or receipt["temporary_profile_removed"] is not True
            or receipt["artifact"]["signature"] != "verified locally"
            or build["Signing"] != "ad-hoc; local use only"
            or build["BuildMacOS"] != receipt["release"]["minimum_macos"]):
        raise RuntimeError("The macOS receipt does not match the selected release and verified build.")
    name = f"panora-v{version}-{revision[:12]}-macos-{arch}-adhoc.zip"
    if receipt["release"]["archive"] != name or receipts[0].name != f"RELEASE-RECEIPT.macos-{arch}.json":
        raise RuntimeError("The macOS receipt does not identify the expected release archive.")
    checksum_name = f"SHA256SUMS.macos-{arch}"
    checksums = (macos / checksum_name).read_text().splitlines()
    archive = staged / name
    shutil.copyfile(macos / name, archive)
    checksum_line = verify_checksum(archive, checksums)
    if checksum_line.split()[0] != receipt["release"]["sha256"]:
        raise RuntimeError("The macOS archive checksum does not match the preflight receipt.")
    (staged / checksum_name).write_text(checksum_line)
    (staged / receipts[0].name).write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")


def stage_release(root, revision, version, arch, macos, destination):
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
        raise RuntimeError("Choose a three-part release version, for example 0.4.0.")
    revision = subprocess.run(
        ["git", "rev-parse", "--verify", "--end-of-options", f"{revision}^{{commit}}"],
        cwd=root, check=True, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
        text=True, timeout=10,
    ).stdout.strip()
    destination = destination.absolute()
    if destination.exists() or destination.is_symlink():
        raise RuntimeError("Select a new staging directory.")
    with tempfile.TemporaryDirectory(prefix=".panora-release-assets-", dir=destination.parent) as temporary:
        staged = Path(temporary) / "verified"
        staged.mkdir()
        arch_assets(arch, revision, version, staged)
        macos_assets(macos, revision, version, staged)
        destination.mkdir()
        staged.rename(destination)
    return revision


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("revision")
    parser.add_argument("version")
    parser.add_argument("arch", type=Path)
    parser.add_argument("macos", type=Path)
    parser.add_argument("output", type=Path)
    options = parser.parse_args()
    try:
        revision = stage_release(ROOT, options.revision, options.version, options.arch, options.macos, options.output)
    except (RuntimeError, OSError, subprocess.SubprocessError, KeyError, TypeError, ValueError) as error:
        parser.exit(1, f"Panora release staging failed: {error}\n")
    print(f"Panora v{options.version} from {revision}: {options.output}")


if __name__ == "__main__":
    main()
