// Build: xcrun swiftc -sdk "$(xcrun --show-sdk-path)" tools/macos-wallpaper.swift -o bin/panora-macos-wallpaper
import AppKit
import ImageIO

func fail(_ message: String) -> Never {
    FileHandle.standardError.write(Data((message + "\n").utf8))
    exit(1)
}

struct PixelSize {
    let width: Int
    let height: Int
}

func backingPixelSize(_ screen: NSScreen) -> PixelSize {
    let size = screen.convertRectToBacking(screen.frame).size
    guard size.width.isFinite, size.height.isFinite, size.width > 0,
          size.height > 0, size.width <= 32768, size.height <= 32768 else {
        fail("invalid display backing geometry")
    }
    return PixelSize(width: Int(size.width.rounded()), height: Int(size.height.rounded()))
}

func imagePixelSize(_ url: URL) -> PixelSize? {
    // Read metadata without decoding pixels. Orientations 5–8 swap the axes.
    let options = [kCGImageSourceShouldCache: false] as CFDictionary
    guard let source = CGImageSourceCreateWithURL(url as CFURL, options),
          let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, options) as? [CFString: Any],
          let width = properties[kCGImagePropertyPixelWidth] as? Int,
          let height = properties[kCGImagePropertyPixelHeight] as? Int,
          width > 0, height > 0 else {
        return nil
    }
    let orientation = properties[kCGImagePropertyOrientation] as? Int ?? 1
    return (5...8).contains(orientation) ? PixelSize(width: height, height: width) : PixelSize(width: width, height: height)
}

func fitWithoutEnlargement(_ image: PixelSize, within screen: PixelSize) -> NSImageScaling {
    // NSWorkspace does not support NSImageScaling.scaleProportionallyDown.
    // Its supported proportional fit can only shrink if either axis is larger.
    if image.width <= screen.width && image.height <= screen.height {
        return .scaleNone
    }
    return .scaleProportionallyUpOrDown
}

// A command-line Swift entry point executes AppKit desktop calls on the main thread.
let args = Array(CommandLine.arguments.dropFirst())
guard args == ["probe"] || args == ["geometry"] || args == ["watch-spaces"] || (args.count == 4 && args[0] == "set") else {
    fail("usage: panora-macos-wallpaper probe | geometry | watch-spaces | set ABSOLUTE_PATH contain|crop true|false")
}
if args == ["watch-spaces"] {
    // Real workspace events require AppKit initialization; a Foundation run loop
    // alone receives synthetic notifications but misses actual Space changes.
    // The packaged observer has its own background-only bundle so AppKit cannot
    // register it as Panora and intercept Finder/Dock reopen events.
    if Bundle.main.bundleURL.pathExtension == "app" && Bundle.main.bundleIdentifier != "org.panora.space-observer" {
        fail("the Space observer must run from its dedicated helper bundle")
    }
    let app = NSApplication.shared
    app.setActivationPolicy(.prohibited)
    let workspace = NSWorkspace.shared
    var pending: DispatchWorkItem?
    let observer = workspace.notificationCenter.addObserver(
        forName: NSWorkspace.activeSpaceDidChangeNotification, object: nil, queue: .main
    ) { _ in
        pending?.cancel()
        let event = DispatchWorkItem {
            FileHandle.standardOutput.write(Data("changed\n".utf8))
        }
        pending = event
        // Give nearby notifications a short window to coalesce before refresh.
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.025, execute: event)
    }
    // The reader bounds startup separately from the observer's service lifetime.
    FileHandle.standardOutput.write(Data("ready\n".utf8))
    withExtendedLifetime(observer) { app.run() }
    exit(0)
}
let workspace = NSWorkspace.shared
let screens = NSScreen.screens
guard !screens.isEmpty else { fail("no connected screens in this desktop session") }
if args == ["geometry"] {
    // Read-only backing-store pixels, already oriented in AppKit screen space.
    // This is the full frame, including areas occupied by the menu bar and Dock.
    guard screens.count <= 32 else { fail("display count exceeds 32") }
    let displays: [[String: Any]] = screens.enumerated().map { index, screen in
        let size = backingPixelSize(screen)
        let identifier = (screen.deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? NSNumber)?.stringValue ?? String(index)
        return ["name": "\(screen.localizedName) (\(identifier))",
                "width": size.width, "height": size.height,
                "scale": Double(screen.backingScaleFactor), "transform": 0]
    }
    do {
        let data = try JSONSerialization.data(withJSONObject: displays, options: [.sortedKeys])
        FileHandle.standardOutput.write(data)
        FileHandle.standardOutput.write(Data("\n".utf8))
    } catch { fail("could not encode display geometry") }
    exit(0)
}
if args == ["probe"] {
    // Read-only: query the desktop service without setting or refreshing anything.
    guard screens.allSatisfy({ workspace.desktopImageURL(for: $0) != nil }) else {
        fail("desktop image service is unavailable for a connected screen")
    }
    print("ready")
    exit(0)
}
guard args[1].hasPrefix("/"), ["contain", "crop"].contains(args[2]),
      ["true", "false"].contains(args[3]), !(args[2] == "crop" && args[3] == "true") else {
    fail("invalid wallpaper path or display options")
}
let url = URL(fileURLWithPath: args[1])
guard FileManager.default.isReadableFile(atPath: url.path) else {
    fail("wallpaper image is not readable")
}
let crop = args[2] == "crop"
let imageSize: PixelSize?
if crop || args[3] == "true" {
    imageSize = nil
} else {
    guard let size = imagePixelSize(url) else { fail("could not read wallpaper image dimensions") }
    imageSize = size
}
let displayOptions: [[NSWorkspace.DesktopImageOptionKey: Any]] = screens.map { screen in
    let scale = imageSize.map { fitWithoutEnlargement($0, within: backingPixelSize(screen)) } ?? .scaleProportionallyUpOrDown
    return [.imageScaling: scale.rawValue, .allowClipping: crop, .fillColor: NSColor.black]
}
for (index, (screen, options)) in zip(screens, displayOptions).enumerated() {
    do {
        try workspace.setDesktopImageURL(url, for: screen, options: options)
    } catch {
        // Earlier screens may already have changed; this operation is not atomic.
        fail("screen \(index + 1) failed (\(index) earlier screens may already have changed): \(error.localizedDescription)")
    }
}
print("ok")
