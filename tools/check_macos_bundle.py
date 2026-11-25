"""Check a packaged Mac app with an isolated, headless, zero-source service.

This does not launch the graphical client, register login startup, select images,
probe wallpaper rendering or establish installed-app/desktop acceptance.
"""

import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time

BINARIES = ("panora-client", "panora", "panora-macos-wallpaper")
OBSERVER = Path("Contents/Helpers/Panora Space Observer.app")
MENU = Path("Contents/Helpers/Panora Menu.app")
OUTPUT_LIMIT = 128 * 1024
COMMAND_TIMEOUT = 10
ZERO_SOURCE_CONFIG = b"version = 1\nproviders = []\n"


class CheckError(RuntimeError):
    pass


class OwnedProcess:
    """Bound output and reap only this check's private process group."""

    def __init__(self, args, env):
        self.process = subprocess.Popen(
            [str(arg) for arg in args], env=env, cwd="/", stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, start_new_session=True)
        self.output = bytearray()
        self.exceeded = False
        self.reader = threading.Thread(target=self._read, daemon=True)
        self.reader.start()

    def _signal(self, value):
        try:
            os.killpg(self.process.pid, value)
        except ProcessLookupError:
            pass

    def _read(self):
        while chunk := self.process.stdout.read(8192):
            room = OUTPUT_LIMIT - len(self.output)
            self.output.extend(chunk[:room])
            if len(chunk) > room:
                self.exceeded = True
                self._signal(signal.SIGKILL)
                break

    def wait(self, timeout=COMMAND_TIMEOUT):
        try:
            code = self.process.wait(timeout=timeout)
        except subprocess.TimeoutExpired as error:
            raise CheckError("A preflight process exceeded its time limit.") from error
        self.reader.join(timeout=1)
        if self.exceeded:
            raise CheckError("A preflight process exceeded its output limit.")
        if self.reader.is_alive():
            raise CheckError("A preflight process left a child holding its output open.")
        return code, self.output.decode("utf-8", errors="replace")

    def close(self):
        self._signal(signal.SIGTERM)
        try:
            self.process.wait(timeout=2)
        except subprocess.TimeoutExpired:
            pass
        # Descendants can outlive a normally exited parent. The group belongs
        # exclusively to this invocation because it was started in a new session.
        self._signal(signal.SIGKILL)
        self.process.wait(timeout=2)
        self.reader.join(timeout=2)
        if self.reader.is_alive():
            raise CheckError("Could not join a preflight output reader.")
        self.process.stdout.close()


@contextmanager
def owned_process(args, env):
    child = OwnedProcess(args, env)
    try:
        yield child
    finally:
        child.close()


def command(args, env, success=True, timeout=COMMAND_TIMEOUT):
    with owned_process(args, env) as child:
        code, output = child.wait(timeout)
    if success and code != 0:
        raise CheckError(f"Preflight command {Path(args[0]).name} failed (exit {code}).")
    return code, output


def json_command(args, env):
    _, output = command(args, env)
    try:
        result = json.loads(output)
    except (ValueError, TypeError) as error:
        raise CheckError("A preflight command did not return JSON.") from error
    if not isinstance(result, dict) or result.get("version") != 1:
        raise CheckError("A preflight command returned an unsupported response.")
    return result


def private_environment(root):
    """Do not inherit user paths, providers, credentials or shell configuration."""
    env = {"PATH": "/usr/bin:/bin", "LANG": "C", "NO_COLOR": "1"}
    for key, directory in (("HOME", "Private home"), ("XDG_CONFIG_HOME", "Private config"),
                           ("XDG_STATE_HOME", "Private state"), ("XDG_CACHE_HOME", "Private cache"),
                           ("XDG_RUNTIME_DIR", "Private runtime"), ("TMPDIR", "Private temp")):
        path = root / directory
        path.mkdir(mode=0o700)
        env[key] = str(path)
    return env


def sha256(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def regular_component(bundle, relative):
    path = bundle / relative
    for component in (path, *path.parents):
        if component.is_symlink():
            raise CheckError(f"Bundle component must not be a symlink: {relative}")
        if component == bundle:
            break
    if not path.is_file():
        raise CheckError(f"Missing bundle component: {relative}")
    return path


def load_plist(bundle, relative):
    path = regular_component(bundle, relative)
    try:
        with path.open("rb") as source:
            result = plistlib.load(source)
    except (ValueError, plistlib.InvalidFileException) as error:
        raise CheckError(f"Invalid bundle property list: {relative}") from error
    if not isinstance(result, dict):
        raise CheckError(f"Invalid bundle property list: {relative}")
    return result


def inspect_bundle(bundle, env):
    if bundle.is_symlink() or bundle.suffix != ".app" or not bundle.is_dir():
        raise CheckError("Select an existing non-symlink Panora.app bundle.")
    info = load_plist(bundle, "Contents/Info.plist")
    observer = load_plist(bundle, OBSERVER / "Contents/Info.plist")
    menu = load_plist(bundle, MENU / "Contents/Info.plist")
    build = load_plist(bundle, "Contents/Resources/BuildInfo.plist")
    if info.get("CFBundleIdentifier") != "org.panora.app" or info.get("CFBundleExecutable") != "panora-client":
        raise CheckError("The selected bundle is not the expected Panora client.")
    if (observer.get("CFBundleIdentifier") != "org.panora.space-observer"
            or observer.get("CFBundleExecutable") != "panora-macos-wallpaper"
            or observer.get("LSBackgroundOnly") is not True):
        raise CheckError("The dedicated background-only Space observer is invalid.")
    if (menu.get("CFBundleIdentifier") != "org.panora.menu"
            or menu.get("CFBundleExecutable") != "panora-menu" or menu.get("LSUIElement") is not True):
        raise CheckError("The dedicated Panora menu agent is invalid.")
    if (not re.fullmatch(r"[a-f0-9]{40}", str(build.get("SourceRevision", "")))
            or not re.fullmatch(r"[a-f0-9]{64}", str(build.get("SourceSnapshotSHA256", "")))
            or not isinstance(build.get("SourceDirty"), bool)
            or build.get("Version") != info.get("CFBundleShortVersionString")
            or build.get("Architecture") != platform.machine()):
        raise CheckError("Bundle build identity is missing, inconsistent or for another architecture.")
    components = [Path("Contents/MacOS") / name for name in BINARIES]
    components.append(OBSERVER / "Contents/MacOS/panora-macos-wallpaper")
    components.append(MENU / "Contents/MacOS/panora-menu")
    hashes = {}
    for relative in components:
        path = regular_component(bundle, relative)
        if not os.access(path, os.X_OK):
            raise CheckError(f"Bundle component is not executable: {relative}")
        _, arches = command(["/usr/bin/xcrun", "lipo", "-archs", path], env)
        if arches.split() != [platform.machine()]:
            raise CheckError(f"Unexpected native architecture: {relative}")
        hashes[str(relative)] = sha256(path)
    command(["/usr/bin/codesign", "--verify", "--deep", "--strict", bundle], env)
    return {"build": build, "component_sha256": hashes, "signature": "verified locally"}


def idle_status(cli, socket, env):
    status = json_command([cli, "status", "--socket", socket, "--json"], env)
    if (status.get("setup_required") is not True or status.get("paused") is not True
            or status.get("current") is not None or status.get("next_at") is not None
            or status.get("history_length") != 0 or status.get("providers")
            or status.get("refreshing") or not status.get("instance_id")):
        raise CheckError("The isolated service is not idle with zero sources.")
    return status


def await_idle(child, cli, socket, env):
    deadline = time.monotonic() + COMMAND_TIMEOUT
    while time.monotonic() < deadline:
        if child.process.poll() is not None or child.exceeded:
            raise CheckError("The isolated service exited before becoming ready.")
        if socket.exists():
            return idle_status(cli, socket, env)
        time.sleep(0.05)
    raise CheckError("The isolated service did not publish its private socket in time.")


def smoke_service(bundle, root, env):
    cli = bundle / "Contents/MacOS/panora"
    config = Path(env["XDG_CONFIG_HOME"]) / "preflight.toml"
    config.write_bytes(ZERO_SOURCE_CONFIG)
    config.chmod(0o600)
    state = Path(env["XDG_STATE_HOME"]) / "state.db"
    socket = root / "control.sock"
    args = [cli, "serve", "--config", config, "--state-file", state,
            "--socket", socket, "--paused"]
    original = sha256(config)
    checks = []
    command([cli, "--help"], env)
    json_command([cli, "config", "check", "--config", config, "--json"], env)
    with owned_process(args, env) as child:
        first = await_idle(child, cli, socket, env)
        duplicate, _ = command(args, env, success=False)
        if duplicate == 0 or idle_status(cli, socket, env)["instance_id"] != first["instance_id"]:
            raise CheckError("Duplicate startup did not preserve the original owner.")
        checks.extend(["zero-source paused startup", "duplicate owner rejected"])
        stop = json_command([cli, "stop", "--socket", socket, "--json"], env)
        if stop.get("stopping") is not True:
            raise CheckError("The isolated service did not acknowledge Stop.")
        if child.wait()[0] != 0 or socket.exists():
            raise CheckError("Stop did not join the isolated service and remove its socket.")
    checks.append("graceful Stop and socket cleanup")
    missing, _ = command([cli, "status", "--socket", socket, "--json"], env, success=False)
    if missing == 0 or socket.exists():
        raise CheckError("A disconnected status read unexpectedly started a service.")
    checks.append("disconnected status remains read-only")
    with owned_process(args, env) as child:
        second = await_idle(child, cli, socket, env)
        if second["instance_id"] == first["instance_id"] or sha256(config) != original:
            raise CheckError("Restart did not retain settings with a new service identity.")
        child.process.send_signal(signal.SIGTERM)
        if child.wait()[0] != 0 or socket.exists():
            raise CheckError("SIGTERM did not join the isolated service and remove its socket.")
    checks.append("restart retains idle state and settings; SIGTERM cleans socket")
    invalid = config.with_name("invalid.toml")
    invalid.write_bytes(b"version = [\npreflight-invalid-marker")
    invalid.chmod(0o600)
    before = sha256(invalid)
    bad_socket, bad_state = root / "invalid.sock", state.with_name("invalid.db")
    rejected, _ = command([cli, "serve", "--config", invalid, "--state-file", bad_state,
                           "--socket", bad_socket, "--paused"], env, success=False)
    if rejected == 0 or sha256(invalid) != before or bad_socket.exists() or bad_state.exists():
        raise CheckError("Invalid settings were not preserved before service startup.")
    checks.append("invalid settings preserved without starting service")
    if (Path(env["HOME"]) / "Library/LaunchAgents").exists():
        raise CheckError("Headless preflight unexpectedly created a login registration directory.")
    return checks


def preflight(bundle, inspect_only=False):
    if sys.platform != "darwin":
        raise CheckError("Bundle preflight requires macOS.")
    bundle = bundle.absolute()
    with tempfile.TemporaryDirectory(prefix="pn-check-", dir="/tmp") as temporary:
        root = Path(temporary)
        env = private_environment(root)
        checks = []
        if inspect_only:
            identity = inspect_bundle(bundle, env)
        else:
            if bundle.is_symlink() or bundle.suffix != ".app" or not bundle.is_dir():
                raise CheckError("Select an existing non-symlink Panora.app bundle.")
            copy = root / "Applications with spaces/Panora.app"
            copy.parent.mkdir(mode=0o700)
            shutil.copytree(bundle, copy, symlinks=True)
            # Hash, validate and execute the same private copy. Source replacement
            # during copying must not leave evidence about different bytes.
            identity = inspect_bundle(copy, env)
            checks = smoke_service(copy, root, env)
    return {"version": 1, "mode": "inspect-only" if inspect_only else "headless-smoke", "host": {"macos": platform.mac_ver()[0], "architecture": platform.machine()},
            "artifact": identity, "checks": checks, "temporary_profile_removed": True,
            "limits": ["Headless explicit zero-source CLI profile, not Finder/client acceptance.",
                       "Dedicated background-only Space observer may run; no image is selected or applied.",
                       "The signed menu agent is inspected without launching it.",
                       "No provider network, login/session, rendering or native accessibility acceptance."]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("bundle", type=Path, help="built Panora.app to inspect and copy into a private trial")
    parser.add_argument("--inspect-only", action="store_true", help="verify bundle metadata/signatures without starting its service")
    args = parser.parse_args()
    try:
        result = preflight(args.bundle, args.inspect_only)
    except (CheckError, OSError, subprocess.SubprocessError) as error:
        parser.exit(1, f"Panora bundle preflight failed: {error}\n")
    print(json.dumps(result, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
