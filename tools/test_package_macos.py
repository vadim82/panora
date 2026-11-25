"""Packaging boundaries: immutable inputs, complete publication and safe replacement."""
import importlib.util
from pathlib import Path
import plistlib
import subprocess
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location(
    "package_macos", Path(__file__).resolve().parents[1] / "ui/package_macos.py")
package = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(package)


class MacPackageTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name) / "checkout"
        self.root.mkdir()
        for name in ("cmd", "internal", "ui/frontend", "tools", "docs/providers/examples/editor"):
            (self.root / name).mkdir(parents=True, exist_ok=True)
        for name, content in {
            "go.mod": "module example\n\ngo 1.27.1\n", "go.sum": "", "LICENSE": "project license",
            "internal/runtime.go": "original source", "tools/macos-wallpaper.swift": "helper",
            "ui/frontend/index.html": "client",
            "docs/providers/examples/editor/fixture-album.json": '{"layout":"album"}',
            "docs/providers/examples/editor/fixture-images.json": '{"layout":"images"}',
            "ui/package.json": '{"packageManager":"pnpm@12.16.1"}',
        }.items():
            (self.root / name).write_text(content)
        for name, content in {
            "masters/canyon-passage.svg": b"approved app master",
            "exports/canyon-passage/Panora-canyon-passage.icns": b"approved icon",
        }.items():
            path = self.root / "assets/branding/icons/2026-11-02" / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
        self.destination = Path(self.temporary.name) / "Applications with spaces/Panora.app"
        self.calls = []
        self.failure = None
        self.mutate_source = False
        for target, value in (("sys.platform", "darwin"), ("platform.machine", lambda: "arm64"),
                              ("require_tool", lambda name: "/tools/" + name), ("run", self.fake_run)):
            if "." in target:
                obj, attribute = target.split(".")
                patcher = patch.object(getattr(package, obj), attribute, value)
            else:
                patcher = patch.object(package, target, value)
            patcher.start()
            self.addCleanup(patcher.stop)

    def fake_run(self, args, **kwargs):
        self.calls.append((args, kwargs))
        if self.failure and self.failure(args):
            raise subprocess.CalledProcessError(1, args)
        name = Path(args[0]).name
        result = ""
        if args[1:] == ["--find", "swiftc"]:
            result = "/tools/swiftc"
        elif args[1:] == ["--show-sdk-path"]:
            result = "/SDK with spaces/MacOSX.sdk"
        elif args[1:] == ["--show-sdk-version"]:
            result = "26.6"
        elif args[1:] == ["version"]:
            result = "go version go1.27.1 darwin/arm64"
        elif args[1:] == ["--version"]:
            result = "v24.15.0" if name == "node" else "Swift compiler"
        elif name == "corepack" and args[-1] == "--version":
            result = "12.16.1"
        elif name == "corepack" and args[-1] == "build":
            frontend = Path(kwargs["cwd"])
            fixtures = frontend.parent / "docs/providers/examples/editor"
            for name in ("fixture-album.json", "fixture-images.json"):
                self.assertEqual((fixtures / name).read_bytes(),
                                 (self.root / "docs/providers/examples/editor" / name).read_bytes())
            assets = frontend / "frontend/dist"
            assets.mkdir(parents=True)
            (assets / "index.html").write_text("fresh bundled client")
        elif name == "node" and Path(args[1]).name == "bundled-notices.mjs":
            self.assertTrue(all(binary.is_file() for binary in args[2:4]))
            self.assertTrue(args[5].is_file())
            args[4].write_text("linked dependency notices")
        elif name == "sw_vers":
            result = "26.6.2"
        elif name == "git":
            result = "a" * 40 if args[1] == "rev-parse" else " M internal/runtime.go"
        elif args[1:3] == ["lipo", "-archs"]:
            result = "arm64"
        elif "-o" in args:
            binary = Path(args[args.index("-o") + 1])
            binary.write_text("complete binary")
            binary.chmod(0o755)
            if name == "go":
                snapshot = Path(kwargs["cwd"])
                if snapshot.name == "ui":
                    snapshot = snapshot.parent
                self.assertEqual((snapshot / "internal/runtime.go").read_text(), "original source")
                if self.mutate_source:
                    (self.root / "internal/runtime.go").write_text("edited during compilation")
        return subprocess.CompletedProcess(args, 0, stdout=result + "\n")

    def build(self):
        return package.build_bundle(self.root, self.destination, "0.1.0")

    def test_complete_bundle_uses_snapshot_and_signs_before_publication(self):
        self.mutate_source = True
        with patch.dict(package.os.environ, {"NODE_ENV": "production"}):
            self.build()
        with (self.destination / "Contents/Info.plist").open("rb") as source:
            info = plistlib.load(source)
        self.assertEqual(info["CFBundleExecutable"], "panora-client")
        self.assertEqual(info["CFBundleIdentifier"], package.IDENTIFIER)
        self.assertEqual(info["CFBundleIconFile"], "Panora.icns")
        self.assertEqual((self.destination / "Contents/Resources/Panora.icns").read_bytes(), b"approved icon")
        self.assertEqual((self.destination / "Contents/Resources/LICENSE").read_text(), "project license")
        self.assertEqual((self.destination / "Contents/Resources/THIRD-PARTY-NOTICES").read_text(),
                         "linked dependency notices")
        with (self.destination / "Contents/Resources/BuildInfo.plist").open("rb") as source:
            metadata = plistlib.load(source)
        self.assertEqual(metadata["SourceRevision"], "a" * 40)
        self.assertTrue(metadata["SourceDirty"])
        self.assertEqual(len(metadata["SourceSnapshotSHA256"]), 64)
        self.assertEqual(metadata["Node"], "v24.15.0")
        self.assertEqual(metadata["SDK"], "26.6")
        install_args, install_options = next((args, options) for args, options in self.calls
                                             if Path(args[0]).name == "corepack" and "install" in args)
        self.assertIn("--prod=false", install_args)
        self.assertEqual(install_options["env"]["NODE_ENV"], "production")
        frontend_build = next(index for index, (args, _) in enumerate(self.calls)
                              if Path(args[0]).name == "corepack" and args[-1] == "build")
        first_go_build = next(index for index, (args, _) in enumerate(self.calls)
                             if Path(args[0]).name == "go" and "-o" in args)
        self.assertLess(frontend_build, first_go_build)
        for name in package.BINARIES:
            self.assertTrue((self.destination / "Contents/MacOS" / name).is_file())
        observer = self.destination / "Contents/Helpers" / package.OBSERVER_BUNDLE
        with (observer / "Contents/Info.plist").open("rb") as source:
            observer_info = plistlib.load(source)
        self.assertEqual(observer_info["CFBundleIdentifier"], package.OBSERVER_IDENTIFIER)
        self.assertNotEqual(observer_info["CFBundleIdentifier"], info["CFBundleIdentifier"])
        self.assertTrue(observer_info["LSBackgroundOnly"])
        self.assertTrue((observer / "Contents/MacOS" / observer_info["CFBundleExecutable"]).is_file())
        menu = self.destination / "Contents/Helpers" / package.MENU_BUNDLE
        with (menu / "Contents/Info.plist").open("rb") as source:
            menu_info = plistlib.load(source)
        self.assertEqual(menu_info["CFBundleIdentifier"], package.MENU_IDENTIFIER)
        self.assertTrue(menu_info["LSUIElement"])
        self.assertTrue((menu / "Contents/MacOS" / menu_info["CFBundleExecutable"]).is_file())
        observer_signature = next(index for index, (args, _) in enumerate(self.calls)
                                  if Path(args[0]).name == "codesign" and args[-1].endswith("/Contents/Helpers/" + package.OBSERVER_BUNDLE)
                                  and "--sign" in args)
        app_signature = next(index for index, (args, _) in enumerate(self.calls)
                             if Path(args[0]).name == "codesign" and args[-1].endswith("/Panora.app")
                             and "--sign" in args)
        self.assertLess(observer_signature, app_signature)
        menu_signature = next(index for index, (args, _) in enumerate(self.calls)
                              if Path(args[0]).name == "codesign" and args[-1].endswith("/Contents/Helpers/" + package.MENU_BUNDLE)
                              and "--sign" in args)
        self.assertLess(menu_signature, app_signature)
        self.assertTrue(any("--verify" in args for args, _ in self.calls))
        self.assertFalse(list(self.destination.parent.glob(".panora-build-*")))

    def test_component_or_signature_failure_preserves_previous_bundle(self):
        self.build()
        marker = self.destination / "previous-build"
        marker.write_text("keep")
        for failure in (lambda args: Path(args[0]).name == "corepack" and args[-1] == "build",
                        lambda args: Path(args[0]).name == "node" and Path(args[1]).name == "bundled-notices.mjs",
                        lambda args: Path(args[0]).name == "swiftc" and "-o" in args,
                        lambda args: "--sign" in args and args[-1].endswith(package.OBSERVER_BUNDLE),
                        lambda args: "--sign" in args and args[-1].endswith(package.MENU_BUNDLE),
                        lambda args: "--verify" in args):
            self.failure = failure
            with self.assertRaises(subprocess.CalledProcessError):
                self.build()
            self.assertEqual(marker.read_text(), "keep")
            self.assertFalse(list(self.destination.parent.glob(".panora-build-*")))
        self.failure = None
        self.build()
        self.assertFalse(marker.exists())
        self.assertFalse(list(self.destination.parent.glob("*.previous-*")))

    def test_snapshot_excludes_stale_frontend_output(self):
        stale = self.root / "ui/frontend/dist"
        stale.mkdir()
        (stale / "index.html").write_text("stale client")
        snapshot = Path(self.temporary.name) / "snapshot"
        snapshot.mkdir()
        package.source_snapshot(self.root, snapshot)
        self.assertFalse((snapshot / "ui/frontend/dist").exists())

    def test_missing_binary_does_not_publish(self):
        original = self.fake_run

        def missing(args, **kwargs):
            result = original(args, **kwargs)
            if "-o" in args and Path(args[0]).name == "swiftc":
                Path(args[args.index("-o") + 1]).unlink()
            return result

        with patch.object(package, "run", missing), self.assertRaisesRegex(RuntimeError, "executable"):
            self.build()
        self.assertFalse(self.destination.exists())

    def test_unrelated_app_and_symlink_are_preserved(self):
        self.destination.mkdir(parents=True)
        (self.destination / "Contents").mkdir()
        with (self.destination / "Contents/Info.plist").open("wb") as target:
            plistlib.dump({"CFBundleIdentifier": "unrelated.app"}, target)
        with self.assertRaisesRegex(RuntimeError, "unrelated app"):
            self.build()
        alias = self.destination.with_name("Alias.app")
        alias.symlink_to(self.destination, target_is_directory=True)
        with self.assertRaisesRegex(RuntimeError, "non-symlink"):
            package.build_bundle(self.root, alias, "0.1.0")
        self.assertTrue(self.destination.is_dir())

    def test_publish_failure_restores_previous_bundle(self):
        self.destination.mkdir(parents=True)
        (self.destination / "original").write_text("keep")
        staged = Path(self.temporary.name) / "staged.app"
        staged.mkdir()
        rename = Path.rename

        def fail_staged(path, target):
            if path == staged:
                raise OSError("simulated publication failure")
            return rename(path, target)

        with patch.object(Path, "rename", fail_staged), self.assertRaises(OSError):
            package.publish(staged, self.destination)
        self.assertEqual((self.destination / "original").read_text(), "keep")


if __name__ == "__main__":
    unittest.main()
