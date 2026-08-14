import AppKit
import Foundation

final class ApplicationLifecycleService {
  func identity(bundleID: String) throws -> ApplicationIdentitySnapshot {
    guard let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleID) else {
      throw HelperError.applicationNotInstalled(bundleID)
    }
    let bundle = Bundle(url: url)
    let name =
      (bundle?.object(forInfoDictionaryKey: "CFBundleDisplayName") as? String)
      ?? (bundle?.object(forInfoDictionaryKey: "CFBundleName") as? String)
      ?? url.deletingPathExtension().lastPathComponent
    return ApplicationIdentitySnapshot(
      bundleID: bundleID,
      name: name,
      iconPNGBase64: applicationIconPNGBase64(bundle: bundle)
    )
  }

  func use(bundleID: String) async throws -> UseApplicationSnapshot {
    guard AppPolicy.allows(bundleID: bundleID) else {
      throw HelperError.appNotAllowed(bundleID)
    }
    let runningPIDs = Set(runningApplications(bundleID: bundleID).map(\.processIdentifier))
    for pid in runningPIDs {
      guard AppPolicy.allows(bundleID: bundleID, pid: pid) else {
        throw HelperError.appNotAllowed(bundleID)
      }
    }
    guard let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleID) else {
      throw HelperError.applicationNotInstalled(bundleID)
    }
    let configuration = NSWorkspace.OpenConfiguration()
    configuration.activates = true
    configuration.addsToRecentItems = false
    do {
      let application = try await NSWorkspace.shared.openApplication(
        at: url,
        configuration: configuration
      )
      guard AppPolicy.allows(bundleID: bundleID, pid: application.processIdentifier) else {
        throw HelperError.appNotAllowed(bundleID)
      }
      return snapshot(
        application,
        newlyLaunched: !runningPIDs.contains(application.processIdentifier)
      )
    } catch let error as HelperError {
      throw error
    } catch {
      throw HelperError.useFailed(error.localizedDescription)
    }
  }

  func quit(bundleID: String, pid: pid_t) async throws -> QuitApplicationSnapshot {
    guard AppPolicy.allows(bundleID: bundleID, pid: pid) else {
      throw HelperError.appNotAllowed(bundleID)
    }
    guard let running = NSRunningApplication(processIdentifier: pid) else {
      return QuitApplicationSnapshot(bundleID: bundleID, pid: pid, closed: true)
    }
    guard running.bundleIdentifier == bundleID else {
      throw HelperError.appNotFound(bundleID)
    }
    if running.isTerminated {
      return QuitApplicationSnapshot(bundleID: bundleID, pid: pid, closed: true)
    }
    guard running.terminate() else {
      throw HelperError.actionFailed("application rejected the normal quit request")
    }
    for _ in 0..<10 {
      if applicationIsClosed(bundleID: bundleID, pid: pid) {
        return QuitApplicationSnapshot(bundleID: bundleID, pid: pid, closed: true)
      }
      try await Task.sleep(for: .milliseconds(100))
    }
    return QuitApplicationSnapshot(
      bundleID: bundleID,
      pid: pid,
      closed: applicationIsClosed(bundleID: bundleID, pid: pid)
    )
  }

  private func runningApplications(bundleID: String) -> [NSRunningApplication] {
    NSRunningApplication.runningApplications(withBundleIdentifier: bundleID)
      .filter { !$0.isTerminated }
  }

  private func applicationIconPNGBase64(bundle: Bundle?) -> String? {
    guard
      let bundle,
      let resourceURL = bundle.resourceURL,
      let iconFile = bundle.object(forInfoDictionaryKey: "CFBundleIconFile") as? String,
      !iconFile.isEmpty
    else {
      return nil
    }
    var iconURL = resourceURL.appendingPathComponent(iconFile)
    if iconURL.pathExtension.isEmpty {
      iconURL.appendPathExtension("icns")
    }
    guard let source = NSImage(contentsOf: iconURL) else {
      return nil
    }
    let size = NSSize(width: 64, height: 64)
    let rendered = NSImage(size: size)
    rendered.lockFocus()
    NSGraphicsContext.current?.imageInterpolation = .high
    source.draw(
      in: NSRect(origin: .zero, size: size),
      from: .zero,
      operation: .copy,
      fraction: 1
    )
    rendered.unlockFocus()
    guard
      let tiff = rendered.tiffRepresentation,
      let bitmap = NSBitmapImageRep(data: tiff),
      let png = bitmap.representation(using: .png, properties: [:])
    else {
      return nil
    }
    return png.base64EncodedString()
  }

  private func applicationIsClosed(bundleID: String, pid: pid_t) -> Bool {
    guard let current = NSRunningApplication(processIdentifier: pid) else {
      return true
    }
    return current.isTerminated || current.bundleIdentifier != bundleID
  }

  private func snapshot(
    _ application: NSRunningApplication,
    newlyLaunched: Bool
  ) -> UseApplicationSnapshot {
    UseApplicationSnapshot(
      bundleID: application.bundleIdentifier ?? "",
      name: application.localizedName ?? application.bundleIdentifier ?? "Application",
      pid: application.processIdentifier,
      newlyLaunched: newlyLaunched
    )
  }
}
