"""Ensure release staging requires matching, intact artifacts from both platforms."""

import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location(
    "stage_release", Path(__file__).resolve().parents[1] / "packaging/stage-release.py")
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)
REVISION = "a" * 40
VERSION = "0.4.0"
PACKAGE = f"panora-{VERSION}-1-x86_64.pkg.tar.zst"
ARCHIVE = f"panora-v{VERSION}-{REVISION[:12]}-macos-arm64-adhoc.zip"


def checksum(path):
    return f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n"


class StageReleaseTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        self.arch, self.macos, self.destination = (root / name for name in ("Arch", "Mac", "Release assets"))
        self.arch.mkdir()
        self.macos.mkdir()
        package = self.arch / PACKAGE
        archive = self.macos / ARCHIVE
        source = self.arch / f"panora-{REVISION}.tar.gz"
        for name in (package, archive, source):
            name.write_text(f"Example {name.name}")
        self.write_arch_recipe(VERSION)
        (self.macos / "SHA256SUMS.macos-arm64").write_text(checksum(archive))
        self.mac_receipt = self.macos / "RELEASE-RECEIPT.macos-arm64.json"
        self.mac_receipt.write_text(json.dumps({
            "mode": "headless-smoke", "checks": ["paused startup"], "temporary_profile_removed": True,
            "artifact": {
                "signature": "verified locally",
                "build": {"Architecture": "arm64", "SourceRevision": REVISION, "SourceDirty": False,
                          "Version": VERSION, "Signing": "ad-hoc; local use only", "BuildMacOS": "27.2.1"},
            },
            "release": {"archive": ARCHIVE, "sha256": checksum(archive).split()[0], "minimum_macos": "27.2.1"},
        }))

    def write_arch_recipe(self, version):
        (self.arch / "PKGBUILD").write_text(f"_release_version='{version}'\n")
        arch_checksums = self.arch_checksums()
        (self.arch / "RELEASE-RECEIPT.txt").write_text(
            f"Revision: {REVISION}\nArtifact: {PACKAGE}\n" + arch_checksums
        )
        (self.arch / "SHA256SUMS").write_text(arch_checksums)

    def arch_checksums(self):
        return "".join(checksum(self.arch / name) for name in (PACKAGE, f"panora-{REVISION}.tar.gz", "PKGBUILD"))

    def stage(self):
        resolved = subprocess.CompletedProcess([], 0, stdout=f"{REVISION}\n")
        with patch.object(release.subprocess, "run", return_value=resolved):
            return release.stage_release(release.ROOT, "HEAD", VERSION, self.arch, self.macos, self.destination)

    def test_stages_both_assets_without_unpublished_source_files(self):
        self.assertEqual(self.stage(), REVISION)
        self.assertEqual({path.name for path in self.destination.iterdir()}, {
            PACKAGE, ARCHIVE, "SHA256SUMS", "SHA256SUMS.macos-arm64", self.mac_receipt.name,
        })
        self.assertEqual((self.destination / "SHA256SUMS").read_text(), checksum(self.arch / PACKAGE))

    def test_arch_checksum_list_must_match_original_receipt(self):
        package = self.arch / PACKAGE
        package.write_text("Different bytes under the same package name")
        (self.arch / "SHA256SUMS").write_text(self.arch_checksums())
        with self.assertRaisesRegex(RuntimeError, "Arch checksums"):
            self.stage()
        self.assertFalse(self.destination.exists())

    def test_arch_release_label_must_match(self):
        for version in ("", "0.4.1"):
            with self.subTest(version=version):
                self.write_arch_recipe(version)
                with self.assertRaisesRegex(RuntimeError, "selected release version"):
                    self.stage()
                self.assertFalse(self.destination.exists())

    def test_revision_mismatch_leaves_no_staged_assets(self):
        self.mac_receipt.write_text(self.mac_receipt.read_text().replace(f'"{REVISION}"', f'"{"b" * 40}"'))
        with self.assertRaisesRegex(RuntimeError, "macOS receipt"):
            self.stage()
        self.assertFalse(self.destination.exists())

    def test_changed_archive_leaves_no_staged_assets(self):
        (self.macos / ARCHIVE).write_text("changed after preflight")
        with self.assertRaisesRegex(RuntimeError, "Checksum verification failed"):
            self.stage()
        self.assertFalse(self.destination.exists())

    def test_missing_macos_artifact_leaves_no_staged_assets(self):
        self.mac_receipt.unlink()
        with self.assertRaisesRegex(RuntimeError, "verified macOS release"):
            self.stage()
        self.assertFalse(self.destination.exists())


if __name__ == "__main__":
    unittest.main()
