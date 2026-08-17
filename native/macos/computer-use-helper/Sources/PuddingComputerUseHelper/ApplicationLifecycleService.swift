import AppKit
import ApplicationServices
import Foundation

final class ApplicationLifecycleService {
  private let screenCapture = ScreenCaptureService()
  private var knownApplications: [String: String] = [:]

  func listApplications() -> [ApplicationSnapshot] {
    var discovered: [String: String] = [:]
    for url in installedApplicationURLs() {
      guard let bundle = Bundle(url: url), let bundleID = bundle.bundleIdentifier,
        !bundleID.isEmpty, isUserFacingApplication(bundle)
      else { continue }
      if discovered[bundleID] == nil {
        discovered[bundleID] = ValueSanitizer.bounded(applicationName(bundle: bundle, url: url))
      }
    }

    for application in runningGUIApplications() {
      guard let bundleID = application.bundleIdentifier, discovered[bundleID] == nil else {
        continue
      }
      discovered[bundleID] = ValueSanitizer.bounded(application.localizedName ?? bundleID)
    }
    for (bundleID, name) in knownApplications where discovered[bundleID] == nil {
      discovered[bundleID] = name
    }

    return discovered.map { bundleID, name in
      let instances = runningApplications(bundleID: bundleID)
        .filter { $0.activationPolicy == .regular }
      return ApplicationSnapshot(
        bundleID: bundleID,
        name: name,
        running: !instances.isEmpty,
        active: instances.contains(where: \.isActive),
        controllable: AppPolicy.allows(bundleID: bundleID)
          && instances.allSatisfy {
            AppPolicy.allows(bundleID: bundleID, pid: $0.processIdentifier)
          }
      )
    }.sorted {
      if $0.active != $1.active { return $0.active }
      if $0.running != $1.running { return $0.running }
      return $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending
    }
  }

  func identity(bundleID: String) throws -> ApplicationIdentitySnapshot {
    guard let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleID) else {
      throw HelperError.applicationNotInstalled(bundleID)
    }
    let bundle = Bundle(url: url)
    let name = ValueSanitizer.bounded(
      (bundle?.object(forInfoDictionaryKey: "CFBundleDisplayName") as? String)
      ?? (bundle?.object(forInfoDictionaryKey: "CFBundleName") as? String)
      ?? url.deletingPathExtension().lastPathComponent
    ) ?? bundleID
    knownApplications[bundleID] = name
    return ApplicationIdentitySnapshot(
      bundleID: bundleID,
      name: name,
      iconPNGBase64: applicationIconPNGBase64(bundle: bundle)
    )
  }

  func use(bundleID: String, foreground: Bool) async throws -> UseApplicationSnapshot {
    guard AppPolicy.allows(bundleID: bundleID) else {
      throw HelperError.appNotAllowed(bundleID)
    }
    let runningInstances = runningApplications(bundleID: bundleID)
      .filter { $0.activationPolicy == .regular }
    let runningPIDs = Set(runningInstances.map(\.processIdentifier))
    for pid in runningPIDs {
      guard AppPolicy.allows(bundleID: bundleID, pid: pid) else {
        throw HelperError.appNotAllowed(bundleID)
      }
    }
    guard let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleID) else {
      throw HelperError.applicationNotInstalled(bundleID)
    }
    guard CGPreflightScreenCaptureAccess() else {
      throw HelperError.permissionRequired("screen_recording")
    }
    do {
      let application: NSRunningApplication
      let newlyLaunched: Bool
      if let running = preferredRunningApplication(runningInstances), !foreground {
        application = running
        newlyLaunched = false
      } else {
        let configuration = NSWorkspace.OpenConfiguration()
        configuration.activates = false
        configuration.addsToRecentItems = false
        application = try await NSWorkspace.shared.openApplication(
          at: url,
          configuration: configuration
        )
        newlyLaunched = !runningPIDs.contains(application.processIdentifier)
      }
      guard AppPolicy.allows(bundleID: bundleID, pid: application.processIdentifier) else {
        throw HelperError.appNotAllowed(bundleID)
      }
      knownApplications[bundleID] = ValueSanitizer.bounded(
        application.localizedName ?? bundleID
      ) ?? bundleID
      let discovery: (
        status: WindowDiscoveryStatus,
        error: ErrorDetail?,
        windows: [CapturableWindowSnapshot]
      )
      do {
        let windows = try await screenCapture.waitForWindows(
          bundleID: bundleID,
          pid: application.processIdentifier
        )
        discovery = (windows.isEmpty ? .none : .ready, nil, windows)
      } catch {
        discovery = (.failed, errorDetail(for: error), [])
      }
      if foreground {
        try await activate(
          pid: application.processIdentifier,
          bundleID: bundleID
        )
        try await raiseWindows(
          pid: application.processIdentifier,
          bundleID: bundleID,
          targetWindows: discovery.windows
        )
      }
      return snapshot(
        application,
        newlyLaunched: newlyLaunched,
        windowStatus: discovery.status,
        windowError: discovery.error,
        windows: discovery.windows
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

  private func runningGUIApplications() -> [NSRunningApplication] {
    NSWorkspace.shared.runningApplications.filter {
      !$0.isTerminated && $0.activationPolicy == .regular && $0.bundleIdentifier != nil
    }
  }

  private func preferredRunningApplication(
    _ applications: [NSRunningApplication]
  ) -> NSRunningApplication? {
    applications.first(where: \.isActive)
      ?? applications.min { $0.processIdentifier < $1.processIdentifier }
  }

  private func activate(pid: pid_t, bundleID: String) async throws {
    guard let application = NSRunningApplication(processIdentifier: pid),
      application.activate(options: [.activateAllWindows])
    else {
      throw HelperError.useFailed("application rejected foreground activation: \(bundleID)")
    }
    for _ in 0..<200 {
      if NSRunningApplication(processIdentifier: pid)?.isActive == true { return }
      try await Task.sleep(for: .milliseconds(25))
    }
    throw HelperError.useFailed("application did not become foreground: \(bundleID)")
  }

  private func raiseWindows(
    pid: pid_t,
    bundleID: String,
    targetWindows: [CapturableWindowSnapshot]
  ) async throws {
    let application = AXUIElementCreateApplication(pid)
    var rawWindows: CFTypeRef?
    guard AXUIElementCopyAttributeValue(
      application, kAXWindowsAttribute as CFString, &rawWindows
    ) == .success, let windows = rawWindows as? [AXUIElement] else {
      throw HelperError.useFailed("application windows could not be raised: \(bundleID)")
    }
    var raised = false
    for window in windows.reversed() {
      if AXUIElementPerformAction(window, kAXRaiseAction as CFString) == .success {
        raised = true
      }
    }
    guard windows.isEmpty || raised else {
      throw HelperError.useFailed("application rejected window raise: \(bundleID)")
    }
    for _ in 0..<40 {
      if windowsAreAboveOtherApplications(pid: pid, targetWindows: targetWindows) { return }
      try await Task.sleep(for: .milliseconds(25))
    }
    throw HelperError.useFailed("application windows did not become foreground: \(bundleID)")
  }

  private func windowsAreAboveOtherApplications(
    pid: pid_t,
    targetWindows: [CapturableWindowSnapshot]
  ) -> Bool {
    guard !targetWindows.isEmpty else { return true }
    guard
      let ordered = CGWindowListCopyWindowInfo(
        [.optionOnScreenOnly, .excludeDesktopElements], CGWindowID(0)
      ) as? [[String: Any]]
    else {
      return false
    }
    for target in targetWindows {
      guard let targetIndex = ordered.firstIndex(where: {
        ($0[kCGWindowNumber as String] as? NSNumber)?.uint32Value == target.windowID
      }) else {
        return false
      }
      let targetFrame = CGRect(
        x: target.frame.x, y: target.frame.y,
        width: target.frame.width, height: target.frame.height
      )
      for candidate in ordered[..<targetIndex] {
        guard
          (candidate[kCGWindowLayer as String] as? NSNumber)?.intValue == 0,
          (candidate[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value != pid,
          let boundsDictionary = candidate[kCGWindowBounds as String] as? NSDictionary,
          let bounds = CGRect(dictionaryRepresentation: boundsDictionary as CFDictionary)
        else {
          continue
        }
        if bounds.intersects(targetFrame) { return false }
      }
    }
    return true
  }

  private func installedApplicationURLs() -> [URL] {
    let roots = [
      URL(fileURLWithPath: "/Applications", isDirectory: true),
      URL(fileURLWithPath: "/System/Applications", isDirectory: true),
      URL(fileURLWithPath: "/System/Library/CoreServices", isDirectory: true),
      FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(
        "Applications",
        isDirectory: true
      ),
    ]
    var urls: [URL] = []
    for root in roots where FileManager.default.fileExists(atPath: root.path) {
      guard let enumerator = FileManager.default.enumerator(
        at: root,
        includingPropertiesForKeys: [.isApplicationKey],
        options: [.skipsHiddenFiles, .skipsPackageDescendants]
      ) else { continue }
      for case let url as URL in enumerator where url.pathExtension.lowercased() == "app" {
        urls.append(url)
      }
    }
    return urls
  }

  private func isUserFacingApplication(_ bundle: Bundle) -> Bool {
    let packageType = bundle.object(forInfoDictionaryKey: "CFBundlePackageType") as? String
    let backgroundOnly = bundle.object(forInfoDictionaryKey: "LSBackgroundOnly") as? Bool ?? false
    let agent = bundle.object(forInfoDictionaryKey: "LSUIElement") as? Bool ?? false
    return (packageType == nil || packageType == "APPL") && !backgroundOnly && !agent
  }

  private func applicationName(bundle: Bundle, url: URL) -> String {
    (bundle.object(forInfoDictionaryKey: "CFBundleDisplayName") as? String)
      ?? (bundle.object(forInfoDictionaryKey: "CFBundleName") as? String)
      ?? url.deletingPathExtension().lastPathComponent
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
    newlyLaunched: Bool,
    windowStatus: WindowDiscoveryStatus,
    windowError: ErrorDetail?,
    windows: [CapturableWindowSnapshot]
  ) -> UseApplicationSnapshot {
    UseApplicationSnapshot(
      bundleID: application.bundleIdentifier ?? "",
      name: ValueSanitizer.bounded(
        application.localizedName ?? application.bundleIdentifier ?? "Application"
      ) ?? "Application",
      pid: application.processIdentifier,
      newlyLaunched: newlyLaunched,
      windowStatus: windowStatus,
      windowError: windowError,
      windows: windows
    )
  }
}
