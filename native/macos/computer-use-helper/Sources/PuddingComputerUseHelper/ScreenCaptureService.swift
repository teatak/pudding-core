import AppKit
import Foundation
@preconcurrency import ScreenCaptureKit

struct ScreenCaptureApplication {
  let bundleID: String
  let name: String
  let pid: pid_t
}

struct ScreenCaptureInventory {
  let applications: [ScreenCaptureApplication]
  let windows: [CapturableWindowSnapshot]
}

final class ScreenCaptureService {
  func inventory() async throws -> ScreenCaptureInventory {
    guard CGPreflightScreenCaptureAccess() else {
      throw HelperError.permissionRequired("screen_recording")
    }
    let content = try await SCShareableContent.excludingDesktopWindows(
      true,
      onScreenWindowsOnly: true
    )
    let applications = content.applications.map { application in
      ScreenCaptureApplication(
        bundleID: application.bundleIdentifier,
        name: application.applicationName,
        pid: application.processID
      )
    }
    let windows = content.windows.map(windowSnapshot)
    return ScreenCaptureInventory(applications: applications, windows: windows)
  }

  func window(bundleID: String, windowID: UInt32) async throws -> CapturableWindowSnapshot? {
    guard CGPreflightScreenCaptureAccess() else {
      return nil
    }
    let content = try await SCShareableContent.excludingDesktopWindows(
      true,
      onScreenWindowsOnly: true
    )
    guard let window = content.windows.first(where: {
      $0.windowID == windowID && $0.owningApplication?.bundleIdentifier == bundleID
    }) else {
      return nil
    }
    return windowSnapshot(window)
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

  private func backingScaleFactor(for frame: CGRect) -> Double {
    let target = NSScreen.screens.max { left, right in
      left.frame.intersection(frame).area < right.frame.intersection(frame).area
    }
    return Double(target?.backingScaleFactor ?? NSScreen.main?.backingScaleFactor ?? 1)
  }

  private func windowSnapshot(_ window: SCWindow) -> CapturableWindowSnapshot {
    CapturableWindowSnapshot(
      windowID: window.windowID,
      bundleID: window.owningApplication?.bundleIdentifier,
      applicationName: window.owningApplication?.applicationName,
      title: window.title,
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
