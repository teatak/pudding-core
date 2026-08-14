import AppKit
import Foundation

final class ApplicationLifecycleService {
  func launch(bundleID: String) async throws -> LaunchApplicationSnapshot {
    guard AppPolicy.allows(bundleID: bundleID) else {
      throw HelperError.appNotAllowed(bundleID)
    }
    if let running = runningApplication(bundleID: bundleID) {
      guard AppPolicy.allows(bundleID: bundleID, pid: running.processIdentifier) else {
        throw HelperError.appNotAllowed(bundleID)
      }
      return snapshot(running, newlyLaunched: false)
    }
    guard let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleID) else {
      throw HelperError.applicationNotInstalled(bundleID)
    }
    let configuration = NSWorkspace.OpenConfiguration()
    configuration.activates = false
    configuration.addsToRecentItems = false
    do {
      let launched = try await NSWorkspace.shared.openApplication(
        at: url,
        configuration: configuration
      )
      guard AppPolicy.allows(bundleID: bundleID, pid: launched.processIdentifier) else {
        throw HelperError.appNotAllowed(bundleID)
      }
      return snapshot(launched, newlyLaunched: true)
    } catch let error as HelperError {
      throw error
    } catch {
      throw HelperError.launchFailed(error.localizedDescription)
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

  private func runningApplication(bundleID: String) -> NSRunningApplication? {
    NSRunningApplication.runningApplications(withBundleIdentifier: bundleID)
      .first(where: { !$0.isTerminated })
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
  ) -> LaunchApplicationSnapshot {
    LaunchApplicationSnapshot(
      bundleID: application.bundleIdentifier ?? "",
      name: application.localizedName ?? application.bundleIdentifier ?? "Application",
      pid: application.processIdentifier,
      newlyLaunched: newlyLaunched
    )
  }
}
