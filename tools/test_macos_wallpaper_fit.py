"""Check the production ImageIO and Fit decision without touching AppKit desktops."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


@unittest.skipUnless(sys.platform == "darwin", "requires ImageIO and AppKit")
class MacWallpaperFitTest(unittest.TestCase):
    def test_original_size_uses_center_and_either_oversized_axis_uses_fit(self):
        root = Path(__file__).resolve().parents[1]
        source = (root / "tools/macos-wallpaper.swift").read_text()
        anchor = "// A command-line Swift entry point"
        self.assertEqual(source.count(anchor), 1)
        # Exercise the same metadata reader and sizing decision as the setter,
        # with real PNG/JPEG files and simulated backing dimensions.
        source = source.replace(anchor, '''
let directory = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
let context = CGContext(data: nil, width: 32, height: 64, bitsPerComponent: 8,
                        bytesPerRow: 0, space: CGColorSpaceCreateDeviceRGB(),
                        bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue)!
let fixture = context.makeImage()!
func writeFixture(_ name: String, type: String, orientation: Int) -> URL {
    let url = directory.appendingPathComponent(name)
    let output = CGImageDestinationCreateWithURL(url as CFURL, type as CFString, 1, nil)!
    CGImageDestinationAddImage(output, fixture, [kCGImagePropertyOrientation: orientation] as CFDictionary)
    precondition(CGImageDestinationFinalize(output))
    return url
}

let portrait = imagePixelSize(writeFixture("portrait.png", type: "public.png", orientation: 1))!
precondition(portrait.width == 32 && portrait.height == 64)
precondition(fitWithoutEnlargement(portrait, within: PixelSize(width: 64, height: 128)) == .scaleNone)
precondition(fitWithoutEnlargement(portrait, within: PixelSize(width: 32, height: 64)) == .scaleNone)
precondition(fitWithoutEnlargement(portrait, within: PixelSize(width: 31, height: 64)) == .scaleProportionallyUpOrDown)
precondition(fitWithoutEnlargement(portrait, within: PixelSize(width: 64, height: 63)) == .scaleProportionallyUpOrDown)

let sideways = imagePixelSize(writeFixture("sideways.jpg", type: "public.jpeg", orientation: 6))!
precondition(sideways.width == 64 && sideways.height == 32)
precondition(fitWithoutEnlargement(sideways, within: PixelSize(width: 32, height: 64)) == .scaleProportionallyUpOrDown)
precondition(imagePixelSize(directory.appendingPathComponent("missing.png")) == nil)
exit(0)

''' + anchor)
        with tempfile.TemporaryDirectory(prefix="panora-fit-test-") as temporary:
            directory = Path(temporary)
            swift = directory / "helper.swift"
            swift.write_text(source)
            binary = directory / "helper"
            subprocess.run(["xcrun", "swiftc", str(swift), "-o", str(binary)],
                           check=True, capture_output=True, timeout=60)
            subprocess.run([str(binary), str(directory)], check=True,
                           capture_output=True, timeout=5)


if __name__ == "__main__":
    unittest.main()
