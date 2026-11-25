"""Exercise development process ownership with real fake children, never a desktop."""

import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import unittest


SOURCE = Path(__file__).with_name("dev_app.py")
FAKE = r'''#!/usr/bin/env python3
import json,os,signal,subprocess,sys,time
from pathlib import Path
root=Path(__file__).resolve().parents[1]
events=root/'events'
def record(value):
 with events.open('a') as out:out.write(json.dumps(value)+'\n')
socket=Path(sys.argv[sys.argv.index('--socket')+1])
if Path(sys.argv[0]).name=='panora-ui':
 record({'event':'ui','args':sys.argv[1:]})
 if os.environ.get('FAKE_UI_CHILD'):
  child=subprocess.Popen([sys.executable,'-c','import time; time.sleep(60)'])
  (root/'child.pid').write_text(str(child.pid))
 def finish(sig,frame):record({'event':'ui-stopped'});sys.exit(0)
 signal.signal(signal.SIGTERM,finish)
 if os.environ.get('FAKE_UI_WAIT'):
  while True:time.sleep(.05)
 sys.exit(int(os.environ.get('FAKE_UI_EXIT','0')))
if sys.argv[1]=='status':
 if socket.exists():print(json.dumps({'version':1,'instance_id':'fixture-service'}));sys.exit(0)
 sys.exit(1)
record({'event':'serve','args':sys.argv[1:],'config':os.environ['XDG_CONFIG_HOME'],
        'state':os.environ['XDG_STATE_HOME'],'cache':os.environ['XDG_CACHE_HOME']})
if os.environ.get('FAKE_SERVE_FAIL'):sys.exit(3)
config=Path(os.environ['XDG_CONFIG_HOME'])/'panora/config.toml'
config.parent.mkdir(parents=True,exist_ok=True)
if not config.exists():config.write_text('version=1\n')
socket.parent.mkdir(parents=True,exist_ok=True);socket.write_text('fixture')
def finish(sig,frame):
 socket.unlink(missing_ok=True);record({'event':'serve-stopped'});sys.exit(0)
signal.signal(signal.SIGTERM,finish)
while True:time.sleep(.05)
'''


class DevelopmentLaunchTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="panora-dev-launch-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve() / "checkout with spaces"
        (self.root / "tools").mkdir(parents=True)
        (self.root / "build").mkdir()
        shutil.copy2(SOURCE, self.root / "tools/dev_app.py")
        for name in ("panora", "panora-ui"):
            path = self.root / "build" / name
            path.write_text(FAKE)
            path.chmod(0o755)
        self.env = dict(os.environ)

    def events(self):
        path = self.root / "events"
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def start(self, **env):
        process = subprocess.Popen(
            [sys.executable, str(self.root / "tools/dev_app.py")],
            env=dict(self.env, **env), stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
        )
        self.addCleanup(self.finish, process)
        return process

    def finish(self, process):
        if process.poll() is None:
            process.terminate()
        try:
            process.communicate(timeout=8)
        except subprocess.TimeoutExpired:
            process.kill()
            process.communicate(timeout=3)

    def wait_event(self, name):
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            matches = [event for event in self.events() if event["event"] == name]
            if matches:
                return matches[-1]
            time.sleep(0.02)
        self.fail(f"No {name} event: {self.events()}")

    def test_owned_service_is_paused_isolated_and_stopped_after_window(self):
        saved = self.root / "build/dev/config/panora/config.toml"
        saved.parent.mkdir(mode=0o700, parents=True)
        (self.root / "build/dev").chmod(0o700)
        saved.write_text("preserve this existing configuration\n")
        process = self.start()
        stdout, stderr = process.communicate(timeout=8)
        self.assertEqual(process.returncode, 0, stderr)
        self.assertIn("started paused", stdout)
        serve = self.wait_event("serve")
        self.assertIn("--paused", serve["args"])
        for field in ("config", "state", "cache"):
            self.assertTrue(Path(serve[field]).is_relative_to(self.root / "build/dev"))
        self.assertEqual(saved.read_text(), "preserve this existing configuration\n")
        self.wait_event("serve-stopped")
        self.assertFalse((self.root / "build/dev/run/control.sock").exists())

    def test_existing_unowned_service_is_reused_and_never_stopped(self):
        profile = self.root / "build/dev"
        profile.mkdir(mode=0o700)
        run = profile / "run"
        run.mkdir(mode=0o700)
        socket = run / "control.sock"
        socket.write_text("existing fixture service")
        process = self.start()
        stdout, stderr = process.communicate(timeout=8)
        self.assertEqual(process.returncode, 0, stderr)
        self.assertIn("Reusing", stdout)
        self.assertEqual([e["event"] for e in self.events()], ["ui"])
        self.assertEqual(socket.read_text(), "existing fixture service")

    def test_service_start_failure_never_opens_window(self):
        process = self.start(FAKE_SERVE_FAIL="1")
        _, stderr = process.communicate(timeout=8)
        self.assertEqual(process.returncode, 1)
        self.assertIn("service.log", stderr)
        self.assertNotIn("ui", [e["event"] for e in self.events()])

    def test_window_failure_still_stops_owned_service(self):
        process = self.start(FAKE_UI_EXIT="7")
        _, stderr = process.communicate(timeout=8)
        self.assertEqual(process.returncode, 7)
        self.assertIn("ui.log", stderr)
        self.wait_event("serve-stopped")

    def test_window_exit_cleans_its_surviving_process_group_child(self):
        process = self.start(FAKE_UI_CHILD="1")
        _, stderr = process.communicate(timeout=8)
        self.assertEqual(process.returncode, 0, stderr)
        pid = int((self.root / "child.pid").read_text())
        def cleanup():
            try:
                os.kill(pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
        self.addCleanup(cleanup)
        deadline = time.monotonic() + 2
        while time.monotonic() < deadline:
            state = subprocess.run(["ps", "-o", "stat=", "-p", str(pid)],
                                   capture_output=True, text=True, check=False).stdout.strip()
            if not state or state.startswith("Z"):
                return
            time.sleep(0.02)
        self.fail("An owned native-window descendant remained running after its parent exited.")

    def test_concurrent_launch_rejected_and_interrupt_cleans_owned_children(self):
        first = self.start(FAKE_UI_WAIT="1")
        self.wait_event("ui")
        second = self.start()
        _, stderr = second.communicate(timeout=8)
        self.assertEqual(second.returncode, 1)
        self.assertIn("already running", stderr)
        self.assertEqual(sum(e["event"] == "serve" for e in self.events()), 1)
        first.send_signal(signal.SIGTERM)
        first.communicate(timeout=8)
        self.assertEqual(first.returncode, 143)
        self.wait_event("ui-stopped")
        self.wait_event("serve-stopped")


if __name__ == "__main__":
    unittest.main()
