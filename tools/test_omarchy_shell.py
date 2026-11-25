"""Read-only shell projection/model and headless subprocess-boundary regressions."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
PLUGIN = ROOT / 'integrations/omarchy/io.github.vadim82.panora'


class OmarchyShellTest(unittest.TestCase):
    def test_model_contract(self):
        self.assertIsNotNone(shutil.which('node'), 'Node is required by the frontend toolchain')
        subprocess.run(['node', str(ROOT / 'integrations/omarchy/tests/model.test.mjs')], check=True, timeout=20)

    @unittest.skipUnless(shutil.which('qs'), 'Quickshell is optional outside Omarchy; native shell checks run on the target session')
    def test_process_output_bounds_and_action_identity(self):
        for scenario in ['oversized', 'stderr-oversized', 'timeout', 'incomplete', 'instance-change', 'action-malformed', 'quit-desktop']:
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory(prefix='panora-shell-test-') as temporary:
                base = Path(temporary)
                executable = base / 'panora'
                executable.write_text('''#!/usr/bin/env python3
import json,os,sys,signal,time
from pathlib import Path
scenario=os.environ['PANORA_SHELL_TEST_SCENARIO']
signal.signal(signal.SIGTERM,signal.SIG_IGN)
countfile=Path(os.environ['PANORA_SHELL_TEST_COUNT'])
if sys.argv[1]=='shell':
 count=int(countfile.read_text())+1 if countfile.exists() else 1
 countfile.write_text(str(count))
 if scenario=='oversized':
  for _ in range(1000):sys.stdout.write('x'*10000);sys.stdout.flush()
 elif scenario=='stderr-oversized':
  for _ in range(1000):sys.stderr.write('x'*10000);sys.stderr.flush()
 elif scenario=='timeout':time.sleep(30)
 elif scenario=='incomplete':sys.stdout.write('{"version":1')
 else:print(json.dumps({'version':1,'connected':True,'instance_id':'two' if scenario=='instance-change' and count>1 else 'one','paused':True,'busy':False,'uncertain':False,'setup_required':False,'can_previous':False,'warning':False}))
elif scenario=='quit-desktop':
 Path(os.environ['PANORA_SHELL_TEST_ACTIONS']).write_text(json.dumps([Path(sys.argv[0]).name,*sys.argv[1:]]))
 print(json.dumps({'version':1,'stopping':True,'instance_id':'one'}))
else:
 Path(os.environ['PANORA_SHELL_TEST_ACTIONS']).write_text(json.dumps(sys.argv[1:]))
 print('{not-json')
''')
                executable.chmod(0o700)
                ui_executable = base / 'panora-ui'
                ui_executable.symlink_to(executable)
                harness = base / 'harness'
                harness.mkdir()
                (harness / 'Panora').mkdir()
                for name in ['Controller.qml', 'Model.js']:
                    shutil.copy2(PLUGIN / name, harness / 'Panora' / name)
                source = (ROOT / 'integrations/omarchy/tests/ControllerHarness.qml').read_text().replace('import "../io.github.vadim82.panora"', 'import "Panora"')
                (harness / 'shell.qml').write_text(source)
                runtime = base / 'runtime'
                runtime.mkdir(mode=0o700)
                env = dict(os.environ, XDG_RUNTIME_DIR=str(runtime), QT_QPA_PLATFORM='offscreen', QSG_RHI_BACKEND='software',
                           PANORA_SHELL_TEST_SCENARIO=scenario,
                           PANORA_SHELL_TEST_EXECUTABLE=str(executable),
                           PANORA_SHELL_TEST_UI_EXECUTABLE=str(ui_executable),
                           PANORA_SHELL_TEST_COUNT=str(base / 'count'),
                           PANORA_SHELL_TEST_ACTIONS=str(base / 'actions'))
                result = subprocess.run(['qs', '-p', str(harness / 'shell.qml'), '--no-color'], env=env, text=True, capture_output=True, timeout=12)
                lines = [line.split('PANORA_TEST ', 1)[1] for line in (result.stdout + result.stderr).splitlines() if 'PANORA_TEST ' in line]
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertTrue(lines, result.stdout + result.stderr)
                self.assertTrue(json.loads(lines[-1])['pass'], result.stdout + result.stderr)
                if scenario == 'instance-change':
                    self.assertFalse((base / 'actions').exists(), 'stale service action was sent')
                if scenario == 'action-malformed':
                    self.assertEqual(json.loads((base / 'actions').read_text()), ['next', '--json', '--instance', 'one'])
                    self.assertLessEqual(int((base / 'count').read_text()), 3, 'action was replayed')
                if scenario == 'quit-desktop':
                    self.assertEqual(json.loads((base / 'actions').read_text()), ['panora-ui', '--quit', '--instance', 'one'])

    @unittest.skipUnless(shutil.which('qs') and Path('/usr/share/omarchy/shell/Ui').is_dir(), 'Omarchy shared UI kit is optional outside the target session')
    def test_popup_keyboard_with_shared_kit(self):
        with tempfile.TemporaryDirectory(prefix='panora-popup-test-') as temporary:
            base = Path(temporary)
            for name in ['Ui', 'Commons']:
                (base / name).symlink_to(Path('/usr/share/omarchy/shell') / name, target_is_directory=True)
            shutil.copytree(PLUGIN, base / 'Panora')
            shutil.copy2(ROOT / 'integrations/omarchy/tests/PopupHarness.qml', base / 'shell.qml')
            runtime = base / 'runtime'
            runtime.mkdir(mode=0o700)
            env = dict(os.environ, XDG_RUNTIME_DIR=str(runtime), QT_QPA_PLATFORM='offscreen', QSG_RHI_BACKEND='software')
            result = subprocess.run(['qs', '-p', str(base), '--no-color'], env=env, text=True, capture_output=True, timeout=12)
            output = result.stdout + result.stderr
            self.assertEqual(result.returncode, 0, output)
            self.assertIn('"pass":true', output, output)
            self.assertNotIn('FAIL!', output)
            self.assertNotIn('TypeError:', output)

    def test_marketplace_manifest_points_to_the_packaged_widget(self):
        packaged = json.loads((PLUGIN / 'manifest.json').read_text())
        widget = PLUGIN / packaged['entryPoints']['barWidget']
        packaged['entryPoints']['barWidget'] = widget.relative_to(ROOT).as_posix()
        marketplace = json.loads((ROOT / 'manifest.json').read_text())
        self.assertEqual(marketplace, packaged)
        self.assertTrue(widget.is_file())

    def test_manifest_and_plain_text(self):
        manifest = json.loads((PLUGIN / 'manifest.json').read_text())
        self.assertEqual(manifest['id'], PLUGIN.name)
        self.assertEqual(manifest['kinds'], ['bar-widget'])
        widget = PLUGIN / manifest['entryPoints']['barWidget']
        self.assertIn(f'moduleName: "{manifest["id"]}"', widget.read_text())
        self.assertIn('textFormat: Text.PlainText', (PLUGIN / 'PlainText.qml').read_text())
        for path in PLUGIN.glob('*.qml'):
            self.assertNotIn('Text.AutoText', path.read_text())
            self.assertNotIn('Text.RichText', path.read_text())


if __name__ == '__main__':
    unittest.main()
