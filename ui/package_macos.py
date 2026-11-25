"""Build a self-contained, locally signed Panora.app from one source snapshot."""

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import shutil
import subprocess
import sys
import tempfile
import uuid

ROOT = Path(__file__).resolve().parents[1]
IDENTIFIER = "org.panora.app"
BINARIES = ("panora-client", "panora", "panora-macos-wallpaper")
OBSERVER_BUNDLE = "Panora Space Observer.app"
OBSERVER_IDENTIFIER = "org.panora.space-observer"
MENU_BUNDLE = "Panora Menu.app"
MENU_IDENTIFIER = "org.panora.menu"
MENU_EXECUTABLE = "panora-menu"


def run(args, **kwargs):
    return subprocess.run(args, check=True, timeout=600, **kwargs)


def output(args, **kwargs):
    return run(args, capture_output=True, text=True, **kwargs).stdout.strip()


def require_tool(name):
    path = shutil.which(name)
    if not path:
        raise RuntimeError(f"Missing {name}; run mise install, use mise exec --, and install Xcode Command Line Tools.")
    return str(Path(path).absolute())


def source_snapshot(root, destination):
    """Both Go modules and the helper compile against the same private copy."""
    for name in ("LICENSE", "go.mod", "go.sum"):
        shutil.copy2(root / name, destination / name)
    for name in ("cmd", "internal", "ui"):
        shutil.copytree(root / name, destination / name,
                        ignore=shutil.ignore_patterns("node_modules", "dist", "__pycache__", ".DS_Store"))
    # Include the fixtures checked by TypeScript and the selected bundle artwork.
    extra_files = (
        "assets/branding/icons/2026-11-02/masters/canyon-passage.svg",
        "assets/branding/icons/2026-11-02/exports/canyon-passage/Panora-canyon-passage.icns",
        "docs/providers/examples/editor/fixture-album.json",
        "docs/providers/examples/editor/fixture-images.json",
        "tools/macos-wallpaper.swift",
    )
    for name in extra_files:
        target = destination / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(root / name, target)
    digest = hashlib.sha256()
    for path in sorted(destination.rglob("*")):
        if path.is_file():
            relative = path.relative_to(destination).as_posix().encode()
            data = path.read_bytes()
            digest.update(len(relative).to_bytes(8, "big") + relative)
            digest.update(len(data).to_bytes(8, "big") + data)
    return digest.hexdigest()


def publish(staged, destination):
    """Leave the previous complete bundle in place until all build checks pass."""
    backup = destination.with_name(f".{destination.name}.previous-{uuid.uuid4().hex}")
    had_previous = destination.exists()
    if had_previous:
        destination.rename(backup)
    try:
        staged.rename(destination)
    except OSError:
        if had_previous:
            # A failed restore leaves the backup outside the temporary build tree.
            backup.rename(destination)
        raise
    if had_previous:
        shutil.rmtree(backup)


def build_bundle(root, destination, version):
    if sys.platform != "darwin":
        raise RuntimeError("This build requires macOS and Xcode Command Line Tools.")
    if not re.fullmatch(r"[0-9]+(?:\.[0-9]+){0,2}", version):
        raise RuntimeError("Version must contain one to three numeric components, for example 0.1.0.")
    architecture = {"arm64": "arm64", "x86_64": "amd64"}.get(platform.machine())
    if architecture is None:
        raise RuntimeError(f"Unsupported native architecture: {platform.machine()}")
    go, git, xcrun, codesign, node, corepack = map(
        require_tool, ("go", "git", "xcrun", "codesign", "node", "corepack"))
    frontend_package = json.loads((root / "ui/package.json").read_text())
    package_manager = frontend_package.get("packageManager", "")
    if not re.fullmatch(r"pnpm@[0-9]+\.[0-9]+\.[0-9]+", package_manager):
        raise RuntimeError("The frontend must pin a pnpm packageManager version.")
    node_version = output([node, "--version"])
    pnpm_version = output([corepack, package_manager, "--version"])
    swift = output([xcrun, "--find", "swiftc"])
    sdk = output([xcrun, "--show-sdk-path"])
    sdk_version = output([xcrun, "--show-sdk-version"])
    expected_go = re.search(r"^go (\S+)$", (root / "go.mod").read_text(), re.MULTILINE).group(1)
    go_version = output([go, "version"], cwd=root)
    if f" go{expected_go} " not in go_version:
        raise RuntimeError(f"Expected Go {expected_go}; run mise install and build through mise exec --.")
    system_version = output(["/usr/bin/sw_vers", "-productVersion"])
    revision = output([git, "rev-parse", "HEAD"], cwd=root)
    dirty = bool(output([git, "status", "--porcelain", "--untracked-files=normal"], cwd=root))
    swift_version = output([swift, "--version"])
    destination = destination.absolute()
    if destination.suffix != ".app" or destination.is_symlink():
        raise RuntimeError("Output must be a non-symlink .app path.")
    destination.parent.mkdir(parents=True, exist_ok=True)
    # Fail instead of racing a second packager or replacing an unrelated app.
    with (destination.parent / f".{destination.name}.build.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise RuntimeError(f"Another build is publishing {destination}.") from error
        if destination.exists():
            with (destination / "Contents/Info.plist").open("rb") as info:
                if plistlib.load(info).get("CFBundleIdentifier") != IDENTIFIER:
                    raise RuntimeError(f"Refusing to replace an unrelated app: {destination}")
        with tempfile.TemporaryDirectory(prefix=".panora-build-", dir=destination.parent) as temporary:
            workspace = Path(temporary)
            snapshot = workspace / "source"
            snapshot.mkdir()
            source_hash = source_snapshot(root, snapshot)
            bundle = workspace / "Panora.app"
            executables = bundle / "Contents/MacOS"
            resources = bundle / "Contents/Resources"
            executables.mkdir(parents=True)
            resources.mkdir()
            shutil.copy2(snapshot / "assets/branding/icons/2026-11-02/exports/canyon-passage/Panora-canyon-passage.icns",
                         resources / "Panora.icns")
            env = dict(os.environ, GOOS="darwin", GOARCH=architecture, CGO_ENABLED="1",
                       GOWORK="off", GOFLAGS="", GOTOOLCHAIN="local", SDKROOT=sdk)
            go_args = [go, "build", "-mod=readonly", "-trimpath", "-buildvcs=false"]
            frontend_directory = snapshot / "ui"
            run([corepack, package_manager, "install", "--frozen-lockfile", "--prod=false"],
                cwd=frontend_directory, env=env)
            run([corepack, package_manager, "build"], cwd=frontend_directory, env=env)
            if not (frontend_directory / "frontend/dist/index.html").is_file():
                raise RuntimeError("Frontend build did not produce embedded assets.")
            identity = (f"-X main.builtVersion={version} -X main.builtRevision={revision} "
                        f"-X main.builtDirty={str(dirty).lower()}")
            run([*go_args, "-tags", "wails,production", "-ldflags", identity,
                 "-o", str(executables / BINARIES[0]), "."],
                cwd=frontend_directory, env=env)
            run([*go_args, "-o", str(executables / BINARIES[1]), "./cmd/panora"], cwd=snapshot, env=env)
            menu = bundle / "Contents/Helpers" / MENU_BUNDLE
            menu_executable = menu / "Contents/MacOS" / MENU_EXECUTABLE
            menu_executable.parent.mkdir(parents=True)
            run([*go_args, "-o", str(menu_executable), "./cmd/menu-bar"], cwd=frontend_directory, env=env)
            with (menu / "Contents/Info.plist").open("wb") as target:
                plistlib.dump({
                    "CFBundleExecutable": MENU_EXECUTABLE, "CFBundleIdentifier": MENU_IDENTIFIER,
                    "CFBundleName": "Panora Menu", "CFBundlePackageType": "APPL",
                    "CFBundleVersion": version, "CFBundleShortVersionString": version,
                    "LSUIElement": True, "NSPrincipalClass": "NSApplication",
                    "LSMinimumSystemVersion": system_version,
                }, target)
            shutil.copy2(snapshot / "LICENSE", resources / "LICENSE")
            run([node, str(frontend_directory / "tools/bundled-notices.mjs"),
                 executables / BINARIES[1], executables / BINARIES[0], resources / "THIRD-PARTY-NOTICES",
                 menu_executable],
                cwd=frontend_directory, env=env)
            run([swift, "-sdk", sdk, str(snapshot / "tools/macos-wallpaper.swift"),
                 "-o", str(executables / BINARIES[2])])
            observer = bundle / "Contents/Helpers" / OBSERVER_BUNDLE
            observer_executable = observer / "Contents/MacOS" / BINARIES[2]
            observer_executable.parent.mkdir(parents=True)
            if not (executables / BINARIES[2]).is_file():
                raise RuntimeError(f"Build did not produce an executable {BINARIES[2]}.")
            shutil.copy2(executables / BINARIES[2], observer_executable)
            with (observer / "Contents/Info.plist").open("wb") as target:
                plistlib.dump({
                    "CFBundleExecutable": BINARIES[2], "CFBundleIdentifier": OBSERVER_IDENTIFIER,
                    "CFBundleName": "Panora Space Observer", "CFBundlePackageType": "APPL",
                    "CFBundleVersion": version, "CFBundleShortVersionString": version,
                    "LSBackgroundOnly": True, "NSPrincipalClass": "NSApplication",
                    "LSMinimumSystemVersion": system_version,
                }, target)
            info = {
                "CFBundleExecutable": BINARIES[0], "CFBundleIdentifier": IDENTIFIER,
                "CFBundleName": "Panora", "CFBundleDisplayName": "Panora",
                "CFBundleIconFile": "Panora.icns",
                "CFBundlePackageType": "APPL", "CFBundleVersion": version,
                "CFBundleShortVersionString": version, "NSHighResolutionCapable": True,
                "NSPrincipalClass": "NSApplication", "LSMinimumSystemVersion": system_version,
            }
            with (bundle / "Contents/Info.plist").open("wb") as target:
                plistlib.dump(info, target)
            build_info = {
                "Version": version, "SourceRevision": revision, "SourceDirty": dirty,
                "SourceSnapshotSHA256": source_hash, "Architecture": platform.machine(),
                "BuildMacOS": system_version, "SDK": sdk_version, "Go": go_version, "Swift": swift_version,
                "Node": node_version, "Pnpm": pnpm_version,
                "Signing": "ad-hoc; local use only",
            }
            with (resources / "BuildInfo.plist").open("wb") as target:
                plistlib.dump(build_info, target)
            for binary in [*(executables / name for name in BINARIES), menu_executable]:
                if not binary.is_file() or not os.access(binary, os.X_OK):
                    raise RuntimeError(f"Build did not produce an executable {binary.name}.")
                arches = output([xcrun, "lipo", "-archs", str(binary)]).split()
                if arches != [platform.machine()]:
                    raise RuntimeError(f"Unexpected architecture for {binary.name}: {arches}")
                run([codesign, "--force", "--sign", "-", str(binary)])
            run([codesign, "--force", "--sign", "-", str(observer_executable)])
            run([codesign, "--force", "--sign", "-", str(observer)])
            run([codesign, "--force", "--sign", "-", str(menu)])
            run([codesign, "--force", "--sign", "-", str(bundle)])
            run([codesign, "--verify", "--deep", "--strict", str(bundle)])
            publish(bundle, destination)
    return destination


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / "build/Panora.app")
    parser.add_argument("--version", default="0.1.0")
    args = parser.parse_args()
    try:
        bundle = build_bundle(ROOT, args.output, args.version)
    except (RuntimeError, OSError, subprocess.SubprocessError) as error:
        parser.exit(1, f"Panora packaging failed: {error}\n")
    print(bundle)
    print("Local ad-hoc signed build. Open Panora.app to start its user-session service; login startup is opt-in.")


if __name__ == "__main__":
    main()
