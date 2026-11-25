"""Native observer identity and debounce checks; no windows or wallpaper calls."""
import json
from pathlib import Path
import plistlib
import shutil
import subprocess
import sys
import tempfile
import unittest
import uuid


@unittest.skipUnless(sys.platform == "darwin", "requires AppKit")
class MacSpaceHelperTest(unittest.TestCase):
    def test_background_observer_identity_and_coalescing(self):
        root = Path(__file__).resolve().parents[1]
        source = (root / "tools/macos-wallpaper.swift").read_text()
        # Inject only synthetic input and a bounded exit into the production
        # observer, retaining its initialization, bundle guard and callback.
        anchor = '    // The reader bounds startup separately'
        self.assertEqual(source.count(anchor), 1)
        source = source.replace(anchor, '''    let identity: [String: Any] = [
        "bundle": NSRunningApplication.current.bundleIdentifier ?? "",
        "policy": NSRunningApplication.current.activationPolicy.rawValue,
        "runningBundles": workspace.runningApplications.filter {
            $0.processIdentifier == ProcessInfo.processInfo.processIdentifier
        }.compactMap { $0.bundleIdentifier }
    ]
    let identityJSON = String(data: try! JSONSerialization.data(withJSONObject: identity), encoding: .utf8)!
    FileHandle.standardError.write(Data(("\\nPANORA_TEST_IDENTITY=" + identityJSON + "\\n").utf8))
    DispatchQueue.main.async {
        for _ in 0..<3 {
            workspace.notificationCenter.post(name: NSWorkspace.activeSpaceDidChangeNotification, object: nil)
        }
    }
    DispatchQueue.main.asyncAfter(deadline: .now() + 0.75) { exit(0) }
''' + anchor)
        with tempfile.TemporaryDirectory(prefix="panora-observer-test-") as temporary:
            directory = Path(temporary)
            swift = directory / "helper.swift"
            swift.write_text(source)
            binary = directory / "helper"
            subprocess.run(["xcrun", "swiftc", str(swift), "-o", str(binary)],
                           check=True, capture_output=True, timeout=60)
            parent = directory / "Test Host.app"
            observer = parent / "Contents/Helpers/Panora Space Observer.app"
            identities = ((parent, "org.panora.test-host." + uuid.uuid4().hex),
                          (observer, "org.panora.space-observer"))
            for app, identifier in identities:
                executable = app / "Contents/MacOS/helper"
                executable.parent.mkdir(parents=True)
                shutil.copy2(binary, executable)
                with (app / "Contents/Info.plist").open("wb") as target:
                    plistlib.dump({"CFBundleExecutable": "helper", "CFBundleIdentifier": identifier,
                                   "CFBundlePackageType": "APPL", "LSBackgroundOnly": True}, target)
                subprocess.run(["codesign", "--force", "--sign", "-", str(app)],
                               check=True, capture_output=True, timeout=10)
            # The ordinary companion must not register as its containing app.
            refused = subprocess.run([str(parent / "Contents/MacOS/helper"), "watch-spaces"],
                                     capture_output=True, text=True, timeout=5)
            self.assertNotEqual(refused.returncode, 0)
            self.assertIn("dedicated helper bundle", refused.stderr)
            result = subprocess.run([str(observer / "Contents/MacOS/helper"), "watch-spaces"],
                                    check=True, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.stdout, "ready\nchanged\n")
            identity_line = next(line for line in result.stderr.splitlines()
                                 if line.startswith("PANORA_TEST_IDENTITY="))
            identity = json.loads(identity_line.split("=", 1)[1])
            self.assertEqual(identity["bundle"], "org.panora.space-observer")
            self.assertEqual(identity["policy"], 2)  # NSApplicationActivationPolicy.prohibited
            self.assertTrue(all(item == "org.panora.space-observer" for item in identity["runningBundles"]))


if __name__ == "__main__":
    unittest.main()
