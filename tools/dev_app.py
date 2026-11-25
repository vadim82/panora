"""Run the native development window with a checkout-local service/profile."""

from contextlib import ExitStack
import fcntl
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time


class Interrupted(Exception):
    def __init__(self, signum):
        self.signum = signum


def status(binary, socket, env):
    try:
        result = subprocess.run(
            [str(binary), "status", "--socket", str(socket), "--json"],
            env=env, capture_output=True, text=True, timeout=2,
        )
        value = json.loads(result.stdout) if result.returncode == 0 else {}
        return isinstance(value, dict) and value.get("version") == 1 and bool(value.get("instance_id"))
    except (subprocess.TimeoutExpired, json.JSONDecodeError):
        return False


def signal_group(pid, signum):
    try:
        try:
            os.killpg(pid, signum)
        except PermissionError:
            if sys.platform != "darwin":
                raise
            # Darwin can report EPERM while the last group member is a zombie.
            # Retry after reaping has had a chance to remove that group.
            time.sleep(0.05)
            os.killpg(pid, signum)
    except ProcessLookupError:
        pass


def stop(process):
    # Signal the owned group even if its leader already exited.
    signal_group(process.pid, signal.SIGTERM)
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        pass
    signal_group(process.pid, signal.SIGKILL)
    process.wait(timeout=5)


def launch(root):
    binary, native = root / "build/panora", root / "build/panora-ui"
    if not binary.is_file() or not native.is_file():
        raise RuntimeError("Build the native executables first with task dev:build.")
    profile = root / "build/dev"
    profile.mkdir(mode=0o700, parents=True, exist_ok=True)
    if profile.is_symlink() or profile.stat().st_mode & 0o077:
        raise RuntimeError("build/dev must be a private directory (mode 0700), not a symlink.")
    with (profile / "launch.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise RuntimeError("A task dev session is already running in this checkout.") from exc
        env = dict(os.environ, XDG_CONFIG_HOME=str(profile / "config"),
                   XDG_STATE_HOME=str(profile / "state"), XDG_CACHE_HOME=str(profile / "cache"))
        run = profile / "run"
        run.mkdir(mode=0o700, exist_ok=True)
        socket = run / "control.sock"
        with ((profile / "service.log").open("ab") as service_log,
              (profile / "ui.log").open("ab") as ui_log, ExitStack() as children):
            if status(binary, socket, env):
                print("Reusing the running development service; its state is kept.", flush=True)
            else:
                service = subprocess.Popen(
                    [str(binary), "serve", "--socket", str(socket), "--paused"],
                    cwd=root, env=env, stdin=subprocess.DEVNULL,
                    stdout=service_log, stderr=subprocess.STDOUT, start_new_session=True,
                )
                children.callback(stop, service)
                deadline = time.monotonic() + 15
                while True:
                    if service.poll() is not None:
                        raise RuntimeError("Development service exited; inspect build/dev/service.log.")
                    if status(binary, socket, env):
                        break
                    if time.monotonic() >= deadline:
                        raise RuntimeError("Development service was not ready; inspect build/dev/service.log.")
                    time.sleep(0.1)
                print("Development service started paused.", flush=True)
            print("Opening Panora. Logs and retained session data: build/dev/", flush=True)
            window = subprocess.Popen(
                [str(native), "--socket", str(socket)], cwd=root,
                stdin=subprocess.DEVNULL, stdout=ui_log, stderr=subprocess.STDOUT,
                start_new_session=True,
            )
            children.callback(stop, window)
            result = window.wait()
            if result != 0:
                print("Native window exited unsuccessfully; inspect build/dev/ui.log.", file=sys.stderr)
            return result


def main():
    def interrupted(signum, _frame):
        raise Interrupted(signum)
    for signum in (signal.SIGINT, signal.SIGTERM):
        signal.signal(signum, interrupted)
    try:
        return launch(Path(__file__).resolve().parents[1])
    except Interrupted as exc:
        return 128 + exc.signum
    except (OSError, RuntimeError, subprocess.SubprocessError) as exc:
        print(f"Panora development launch: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
