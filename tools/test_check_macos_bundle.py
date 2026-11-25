"""Offline fixtures for bundle preflight, process bounds and private cleanup."""

import os
from pathlib import Path
import plistlib
import signal
import sys
import tempfile
import unittest
from unittest.mock import patch

import check_macos_bundle as check


FAKE_CLI = r'''
import json, os, signal, socket, sys, time, uuid
from pathlib import Path
args = sys.argv[1:]
def flag(name): return args[args.index(name) + 1]
def emit(value): print(json.dumps(dict(version=1, **value)), flush=True)
root = Path(os.environ['XDG_STATE_HOME'])
record = root / 'fixture-status.json'
if args == ['--help']: print('Fixture help'); sys.exit(0)
if args[:2] == ['config', 'check']: emit({'valid': True}); sys.exit(0)
sock = Path(flag('--socket'))
if args[0] == 'serve':
    if b'version = [' in Path(flag('--config')).read_bytes(): sys.exit(1)
    if sock.exists(): sys.exit(1)
    Path(flag('--state-file')).touch()
    server = socket.socket(socket.AF_UNIX)
    record.write_text(json.dumps({'instance_id': str(uuid.uuid4()), 'pid': os.getpid()}))
    server.bind(str(sock)); server.listen()
    stopped = False
    def stop(*unused):
        global stopped
        stopped = True
    signal.signal(signal.SIGTERM, stop)
    while not stopped: time.sleep(.01)
    server.close(); sock.unlink(); sys.exit(0)
if not sock.exists(): sys.exit(1)
current = json.loads(record.read_text())
if args[0] == 'stop':
    os.kill(current['pid'], signal.SIGTERM)
    emit({'stopping': True}); sys.exit(0)
if args[0] == 'status':
    emit({'instance_id': current['instance_id'], 'paused': True, 'setup_required': True,
          'history_length': 0, 'providers': []}); sys.exit(0)
sys.exit(2)
'''


class PreflightTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='pn-unit-', dir='/tmp')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.bundle = self.root / 'Original app/Panora.app'
        (self.bundle / 'Contents/MacOS').mkdir(parents=True)
        (self.bundle / check.OBSERVER / 'Contents/MacOS').mkdir(parents=True)
        (self.bundle / check.MENU / 'Contents/MacOS').mkdir(parents=True)
        (self.bundle / 'Contents/Resources').mkdir()
        for name in check.BINARIES:
            path = self.bundle / 'Contents/MacOS' / name
            path.write_text(f'#!{sys.executable}\n' + (FAKE_CLI if name == 'panora' else 'pass\n'))
            path.chmod(0o755)
        observer = self.bundle / check.OBSERVER / 'Contents/MacOS/panora-macos-wallpaper'
        observer.write_text('unused observer fixture')
        observer.chmod(0o755)
        menu = self.bundle / check.MENU / 'Contents/MacOS/panora-menu'
        menu.write_text('unused menu fixture')
        menu.chmod(0o755)
        self.write_plist('Contents/Info.plist', {
            'CFBundleIdentifier': 'org.panora.app', 'CFBundleExecutable': 'panora-client',
            'CFBundleShortVersionString': '0.1.0'})
        self.write_plist(check.OBSERVER / 'Contents/Info.plist', {
            'CFBundleIdentifier': 'org.panora.space-observer',
            'CFBundleExecutable': 'panora-macos-wallpaper', 'LSBackgroundOnly': True})
        self.write_plist(check.MENU / 'Contents/Info.plist', {
            'CFBundleIdentifier': 'org.panora.menu', 'CFBundleExecutable': 'panora-menu',
            'LSUIElement': True})
        self.build = {'Version': '0.1.0', 'SourceRevision': 'a' * 40, 'SourceDirty': False,
                      'SourceSnapshotSHA256': 'b' * 64, 'Architecture': check.platform.machine()}
        self.write_plist('Contents/Resources/BuildInfo.plist', self.build)

    def write_plist(self, relative, value):
        with (self.bundle / relative).open('wb') as target:
            plistlib.dump(value, target)

    def inspection_command(self, args, env, **unused):
        if args[0] == '/usr/bin/xcrun':
            return 0, check.platform.machine()
        if args[0] == '/usr/bin/codesign':
            return 0, ''
        self.fail(f'Inspection unexpectedly executed {args}')

    def test_inventory_validates_native_components_and_records_build_identity(self):
        with patch.object(check, 'command', side_effect=self.inspection_command) as run:
            result = check.inspect_bundle(self.bundle, {})
        self.assertEqual(result['build']['SourceRevision'], 'a' * 40)
        self.assertEqual(len(result['component_sha256']), 5)
        self.assertTrue(all(len(value) == 64 for value in result['component_sha256'].values()))
        self.assertEqual(run.call_count, 6)

    def test_inconsistent_identity_or_escaping_component_is_rejected_before_execution(self):
        with patch.object(check, 'command') as run:
            self.build['Architecture'] = 'other-machine'
            self.write_plist('Contents/Resources/BuildInfo.plist', self.build)
            with self.assertRaisesRegex(check.CheckError, 'identity'):
                check.inspect_bundle(self.bundle, {})
            run.assert_not_called()
        self.build['Architecture'] = check.platform.machine()
        self.write_plist('Contents/Resources/BuildInfo.plist', self.build)
        binary = self.bundle / 'Contents/MacOS/panora-client'
        binary.unlink()
        binary.symlink_to(sys.executable)
        with patch.object(check, 'command') as run:
            with self.assertRaisesRegex(check.CheckError, 'symlink'):
                check.inspect_bundle(self.bundle, {})
            run.assert_not_called()

    def test_signature_failure_prevents_service_start(self):
        original = self.inspection_command
        def reject(args, env, **kwargs):
            if args[0] == '/usr/bin/codesign':
                raise check.CheckError('Signature rejected.')
            return original(args, env, **kwargs)
        with patch.object(check.sys, 'platform', 'darwin'), \
                patch.object(check, 'command', side_effect=reject), \
                patch.object(check, 'smoke_service') as smoke:
            with self.assertRaisesRegex(check.CheckError, 'Signature rejected'):
                check.preflight(self.bundle)
            smoke.assert_not_called()

    def test_copy_time_component_tamper_is_checked_before_execution(self):
        original_copy = check.shutil.copytree
        copies = []
        def tamper(source, target, *args, **kwargs):
            result = original_copy(source, target, *args, **kwargs)
            if Path(source) == self.bundle:
                copies.append(Path(target))
                (Path(target) / 'Contents/MacOS/panora').write_text('tampered component')
            return result
        def verify(args, env, **kwargs):
            if args[0] == '/usr/bin/codesign':
                copied = Path(args[-1])
                self.assertEqual(copied, copies[0])
                self.assertNotEqual(copied, self.bundle)
                if (copied / 'Contents/MacOS/panora').read_text() == 'tampered component':
                    raise check.CheckError('Copied signature rejected.')
            return self.inspection_command(args, env, **kwargs)
        with patch.object(check.sys, 'platform', 'darwin'), \
                patch.object(check.shutil, 'copytree', side_effect=tamper), \
                patch.object(check, 'command', side_effect=verify), \
                patch.object(check, 'smoke_service') as smoke:
            with self.assertRaisesRegex(check.CheckError, 'Copied signature rejected'):
                check.preflight(self.bundle)
            smoke.assert_not_called()
        self.assertFalse(copies[0].exists())

    def test_private_environment_does_not_forward_user_paths_or_credentials(self):
        with patch.dict(os.environ, {'PANORA_SECRET': 'do not forward', 'HOME': '/unrelated'}):
            env = check.private_environment(self.root)
        self.assertNotIn('PANORA_SECRET', env)
        self.assertEqual(env['PATH'], '/usr/bin:/bin')
        self.assertTrue(all(Path(env[key]).is_relative_to(self.root) for key in
                            ('HOME', 'TMPDIR', 'XDG_CONFIG_HOME', 'XDG_STATE_HOME', 'XDG_CACHE_HOME', 'XDG_RUNTIME_DIR')))
        self.assertTrue(all(Path(env[key]).stat().st_mode & 0o777 == 0o700 for key in
                            ('HOME', 'TMPDIR', 'XDG_CONFIG_HOME', 'XDG_STATE_HOME', 'XDG_CACHE_HOME', 'XDG_RUNTIME_DIR')))

    def test_inspection_only_never_starts_a_service(self):
        with patch.object(check.sys, 'platform', 'darwin'), \
                patch.object(check, 'command', side_effect=self.inspection_command), \
                patch.object(check, 'smoke_service') as smoke:
            report = check.preflight(self.bundle, inspect_only=True)
        self.assertEqual(report['mode'], 'inspect-only')
        self.assertEqual(report['checks'], [])
        self.assertTrue(report['temporary_profile_removed'])
        smoke.assert_not_called()

    def test_headless_smoke_runs_from_a_copy_and_cleans_private_profile(self):
        inspected_roots = []
        def inspect(bundle, env):
            inspected_roots.append(Path(env['HOME']).parent)
            return {'fixture': True}
        with patch.object(check.sys, 'platform', 'darwin'), patch.object(check, 'inspect_bundle', side_effect=inspect):
            result = check.preflight(self.bundle)
        self.assertEqual(len(result['checks']), 6)
        self.assertFalse(inspected_roots[0].exists())
        self.assertFalse((self.bundle / 'Contents/MacOS/state.db').exists())

    def test_failed_status_reaps_service_and_removes_temporary_profile(self):
        inspected_roots = []
        pids = []
        original = check.idle_status
        def inspect(bundle, env):
            inspected_roots.append(Path(env['HOME']).parent)
            return {}
        def fail(cli, socket, env):
            original(cli, socket, env)
            import json
            pids.append(json.loads((Path(env['XDG_STATE_HOME']) / 'fixture-status.json').read_text())['pid'])
            raise check.CheckError('Simulated readiness failure.')
        with patch.object(check.sys, 'platform', 'darwin'), \
                patch.object(check, 'inspect_bundle', side_effect=inspect), \
                patch.object(check, 'idle_status', side_effect=fail):
            with self.assertRaisesRegex(check.CheckError, 'readiness failure'):
                check.preflight(self.bundle)
        self.assertFalse(inspected_roots[0].exists())
        with self.assertRaises(ProcessLookupError):
            os.kill(pids[0], 0)

    def test_timeout_and_output_flood_are_bounded_and_reaped(self):
        for program, timeout, message in [
            ('import time; time.sleep(20)', .1, 'time limit'),
            ('import sys,time; sys.stdout.write("x" * 200000); sys.stdout.flush(); time.sleep(20)', 3, 'output limit')
        ]:
            with self.subTest(message=message):
                with check.owned_process([sys.executable, '-c', program], dict(os.environ)) as child:
                    with self.assertRaisesRegex(check.CheckError, message):
                        child.wait(timeout)
                self.assertIsNotNone(child.process.poll())
                self.assertLessEqual(len(child.output), check.OUTPUT_LIMIT)
                self.assertFalse(child.reader.is_alive())

    def test_orphaned_output_holder_is_rejected_and_its_group_is_cleaned(self):
        program = 'import subprocess,sys; subprocess.Popen([sys.executable, "-c", "import time; time.sleep(20)"])'
        with check.owned_process([sys.executable, '-c', program], dict(os.environ)) as child:
            with self.assertRaisesRegex(check.CheckError, 'holding its output open'):
                child.wait()
        self.assertFalse(child.reader.is_alive())
        self.assertEqual(child.process.returncode, 0)

    def test_darwin_escalation_accepts_only_gone_or_zombie_groups(self):
        for code, output, exited in (
            (1, "", True),
            (0, "Z\nZ+", True),
            (0, "Z\nS", False),
            (1, "ps: group lookup failed", False),
        ):
            with self.subTest(code=code, output=output):
                child = check.OwnedProcess([sys.executable, '-c', 'pass'], dict(os.environ))
                try:
                    child.wait()
                    with patch.object(child, '_signal', side_effect=[None, PermissionError]), \
                            patch.object(check.sys, 'platform', 'darwin'), \
                            patch.object(check, 'command', return_value=(code, output)) as query:
                        if exited:
                            child.close()
                            self.assertTrue(child.process.stdout.closed)
                        else:
                            with self.assertRaises(PermissionError):
                                child.close()
                    query.assert_called_once_with(
                        ["/bin/ps", "-x", "-g", child.process.pid, "-o", "state="],
                        {"LC_ALL": "C"}, success=False, timeout=2)
                finally:
                    child.process.stdout.close()

    def test_cleanup_escalates_when_owned_service_ignores_sigterm(self):
        ready = self.root / 'handler-ready'
        program = 'import signal,time,sys; from pathlib import Path; signal.signal(signal.SIGTERM, signal.SIG_IGN); Path(sys.argv[1]).touch(); time.sleep(20)'
        with check.owned_process([sys.executable, '-c', program, ready], dict(os.environ)) as child:
            import time
            deadline = time.monotonic() + 2
            while not ready.exists() and time.monotonic() < deadline:
                time.sleep(.01)
            self.assertTrue(ready.exists())
        self.assertEqual(child.process.returncode, -signal.SIGKILL)
        self.assertFalse(child.reader.is_alive())


if __name__ == '__main__':
    unittest.main()
