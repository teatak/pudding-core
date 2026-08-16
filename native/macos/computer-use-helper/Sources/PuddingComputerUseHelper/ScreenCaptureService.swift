import AppKit
import Foundation
@preconcurrency import ScreenCaptureKit

final class ScreenCaptureService {
  func window(bundleID: String, windowID: UInt32) async throws -> CapturableWindowSnapshot {
    guard CGPreflightScreenCaptureAccess() else {
      throw HelperError.permissionRequired("screen_recording")
    }
    let content = try await SCShareableContent.excludingDesktopWindows(
      true,
      onScreenWindowsOnly: true
    )
    guard let window = content.windows.first(where: {
      $0.windowID == windowID && $0.owningApplication?.bundleIdentifier == bundleID
    }) else {
      throw HelperError.windowNotFound(windowID)
    }
    return windowSnapshot(window)
  }

  func waitForWindows(bundleID: String, pid: pid_t) async throws -> [CapturableWindowSnapshot] {
    guard CGPreflightScreenCaptureAccess() else {
      return []
    }
    let clock = ContinuousClock()
    let deadline = clock.now.advanced(by: .seconds(3))
    var previousWindowIDs: [UInt32] = []
    while true {
      let content = try await SCShareableContent.excludingDesktopWindows(
        true,
        onScreenWindowsOnly: true
      )
      let windows = content.windows
        .filter { window in
          window.owningApplication?.bundleIdentifier == bundleID
            && window.owningApplication?.processID == pid
        }
        .map(windowSnapshot)
      let windowIDs = windows.map(\.windowID).sorted()
      if !windowIDs.isEmpty, windowIDs == previousWindowIDs {
        return windows
      }
      if clock.now >= deadline {
        return windows
      }
      previousWindowIDs = windowIDs
      try await Task.sleep(for: .milliseconds(100))
    }
  }

  func capture(bundleID: String, windowID: UInt32, output: String) async throws -> CaptureSnapshot {
    guard CGPreflightScreenCaptureAccess() else {
      throw HelperError.permissionRequired("screen_recording")
    }
    let content = try await SCShareableContent.excludingDesktopWindows(
      true,
      onScreenWindowsOnly: true
    )
    guard let window = content.windows.first(where: { $0.windowID == windowID }) else {
      throw HelperError.windowNotFound(windowID)
    }
    guard let owner = window.owningApplication,
      owner.bundleIdentifier == bundleID
    else {
      throw HelperError.windowNotFound(windowID)
    }
    guard AppPolicy.allows(bundleID: bundleID, pid: owner.processID) else {
      throw HelperError.appNotAllowed(bundleID)
    }

    let scaleFactor = backingScaleFactor(for: window.frame)
    let configuration = SCStreamConfiguration()
    configuration.width = max(1, Int(window.frame.width * scaleFactor))
    configuration.height = max(1, Int(window.frame.height * scaleFactor))
    configuration.showsCursor = false
    configuration.capturesAudio = false
    let filter = SCContentFilter(desktopIndependentWindow: window)

    let image: CGImage
    do {
      image = try await SCScreenshotManager.captureImage(
        contentFilter: filter,
        configuration: configuration
      )
    } catch {
      throw HelperError.captureFailed(error.localizedDescription)
    }

    let representation = NSBitmapImageRep(cgImage: image)
    guard let png = representation.representation(using: .png, properties: [:]) else {
      throw HelperError.captureFailed("PNG encoding failed")
    }
    let outputURL = URL(fileURLWithPath: output).standardizedFileURL
    do {
      try FileManager.default.createDirectory(
        at: outputURL.deletingLastPathComponent(),
        withIntermediateDirectories: true
      )
      try png.write(to: outputURL, options: .atomic)
    } catch {
      throw HelperError.captureFailed(error.localizedDescription)
    }
    return CaptureSnapshot(
      windowID: windowID,
      output: outputURL.path,
      width: image.width,
      height: image.height,
      scaleFactor: scaleFactor
    )
  }

  func backingScaleFactor(for frame: CGRect) -> Double {
    let target = NSScreen.screens.max { left, right in
      left.frame.intersection(frame).area < right.frame.intersection(frame).area
    }
    return Double(target?.backingScaleFactor ?? NSScreen.main?.backingScaleFactor ?? 1)
  }

  private func windowSnapshot(_ window: SCWindow) -> CapturableWindowSnapshot {
    CapturableWindowSnapshot(
      windowID: window.windowID,
      pid: window.owningApplication?.processID ?? 0,
      bundleID: window.owningApplication?.bundleIdentifier,
      applicationName: window.owningApplication?.applicationName,
      title: ValueSanitizer.bounded(window.title),
      frame: FrameSnapshot(
        x: window.frame.origin.x,
        y: window.frame.origin.y,
        width: window.frame.width,
        height: window.frame.height
      )
    )
  }
}

extension CGRect {
  fileprivate var area: CGFloat {
    guard !isNull, !isEmpty else { return 0 }
    return width * height
  }
}
