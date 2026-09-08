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
          },
        instances: instances.map {
          ApplicationInstanceSnapshot(
            pid: $0.processIdentifier, appPath: $0.bundleURL?.resolvingSymlinksInPath().path,
            active: $0.isActive)
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
    let name =
      ValueSanitizer.bounded(
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

  func use(bundleID: String, foreground: Bool, appPath: String? = nil, pid: Int32? = nil)
    async throws -> UseApplicationSnapshot
  {
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
    if let pid, pid <= 0 { throw ArgumentError.invalidOption("pid", "must be positive") }
    let requestedURL: URL?
    if let appPath {
      guard appPath.hasPrefix("/") else {
        throw ArgumentError.invalidOption("appPath", "must be absolute")
      }
      let url = URL(fileURLWithPath: appPath).resolvingSymlinksInPath()
      guard url.pathExtension == "app", Bundle(url: url)?.bundleIdentifier == bundleID else {
        throw ArgumentError.invalidOption("appPath", "must be an app bundle matching appID")
      }
      requestedURL = url
    } else {
      requestedURL = nil
    }
    let matches = runningInstances.filter { app in
      (pid == nil || app.processIdentifier == pid)
        && (requestedURL == nil || app.bundleURL?.resolvingSymlinksInPath() == requestedURL)
    }
    guard matches.count <= 1 else {
      throw HelperError.ambiguousApplication(
        "\(bundleID): \(matches.map { String($0.processIdentifier) }.joined(separator: ", ")); choose appPath or pid from list_apps"
      )
    }
    if pid != nil, matches.isEmpty {
      throw HelperError.appNotFound("requested PID/path does not match \(bundleID)")
    }
    let selected = matches.first
    guard
      let url = requestedURL ?? selected?.bundleURL
        ?? NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleID)
    else {
      throw HelperError.applicationNotInstalled(bundleID)
    }
    guard CGPreflightScreenCaptureAccess() else {
      throw HelperError.permissionRequired("screen_recording")
    }
    do {
      let application: NSRunningApplication
      let newlyLaunched: Bool
      if let running = selected, !foreground || pid != nil {
        application = running
        newlyLaunched = false
      } else {
        let configuration = NSWorkspace.OpenConfiguration()
        configuration.activates = false
        configuration.addsToRecentItems = false
        configuration.createsNewApplicationInstance = requestedURL != nil && selected == nil
        do {
          application = try await NSWorkspace.shared.openApplication(
            at: url,
            configuration: configuration
          )
        } catch {
          throw HelperError.launchFailed(error.localizedDescription)
        }
        newlyLaunched = !runningPIDs.contains(application.processIdentifier)
      }
      guard AppPolicy.allows(bundleID: bundleID, pid: application.processIdentifier) else {
        throw HelperError.appNotAllowed(bundleID)
      }
      guard application.bundleIdentifier == bundleID,
        requestedURL == nil || application.bundleURL?.resolvingSymlinksInPath() == requestedURL,
        pid == nil || application.processIdentifier == pid
      else {
        throw HelperError.useFailed("launched application did not match the requested identity")
      }
      knownApplications[bundleID] =
        ValueSanitizer.bounded(
          application.localizedName ?? bundleID
        ) ?? bundleID
      let discovery:
        (
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
        try await ensureForeground(
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

  func revealWindow(_ target: CapturableWindowSnapshot) async throws {
    guard let bundleID = target.bundleID,
      let application = NSRunningApplication(processIdentifier: target.pid),
      !application.isTerminated, application.bundleIdentifier == bundleID,
      AppPolicy.allows(bundleID: bundleID, pid: target.pid)
    else { throw HelperError.windowNotFound(target.windowID) }
    try await ensureForeground(pid: target.pid, bundleID: bundleID, targetWindows: [target])
  }

  private func ensureForeground(
    pid: pid_t,
    bundleID: String,
    targetWindows: [CapturableWindowSnapshot]
  ) async throws {
    try await ForegroundPolicy.ensure(
      bundleID: bundleID,
      isActive: { NSWorkspace.shared.frontmostApplication?.processIdentifier == pid },
      activate: {
        NSRunningApplication(processIdentifier: pid)?.activate(options: [.activateAllWindows])
          ?? false
      },
      windowsReady: { self.windowsAreAboveOtherApplications(pid: pid, targetWindows: targetWindows) },
      raiseWindows: { try self.raiseWindows(pid: pid, targetWindows: targetWindows) }
    )
  }

  private func raiseWindows(pid: pid_t, targetWindows: [CapturableWindowSnapshot]) throws -> [String] {
    guard AXIsProcessTrusted() else { throw HelperError.permissionRequired("accessibility") }
    let application = AXUIElementCreateApplication(pid)
    var rawWindows: CFTypeRef?
    let readError = AXUIElementCopyAttributeValue(
      application, kAXWindowsAttribute as CFString, &rawWindows)
    guard readError == .success, let windows = rawWindows as? [AXUIElement] else {
      return ["AXWindows returned \(readError.rawValue)"]
    }
    let accessibility = AccessibilityService()
    let targets = windows.filter { window in
      targetWindows.contains { accessibility.matchesWindow(window, target: $0) }
    }
    guard !targets.isEmpty else { return ["no AX window matches the requested window IDs"] }
    var errors: [String] = []
    for (index, window) in targets.reversed().enumerated() {
      let error = AXUIElementPerformAction(window, kAXRaiseAction as CFString)
      if error != .success {
        errors.append("AXRaise window[\(index)] returned \(error.rawValue)")
      }
    }
    return errors
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
      guard
        let targetIndex = ordered.firstIndex(where: {
          ($0[kCGWindowNumber as String] as? NSNumber)?.uint32Value == target.windowID
            && ($0[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value == pid
        }),
        let targetBounds = ordered[targetIndex][kCGWindowBounds as String] as? NSDictionary,
        let targetFrame = CGRect(dictionaryRepresentation: targetBounds as CFDictionary)
      else {
        return false
      }
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
      guard
        let enumerator = FileManager.default.enumerator(
          at: root,
          includingPropertiesForKeys: [.isApplicationKey],
          options: [.skipsHiddenFiles, .skipsPackageDescendants]
        )
      else { continue }
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
