import Foundation

// A foreground request is satisfied by live state, not by an AXRaise return code.
// Only an explicit lifecycle/reveal request uses this policy; actions never activate apps.
enum ForegroundPolicy {
  static func ensure(
    bundleID: String,
    isActive: () -> Bool,
    activate: () -> Bool,
    windowsReady: () -> Bool,
    raiseWindows: () throws -> [String],
    wait: () async throws -> Void = { try await Task.sleep(for: .milliseconds(25)) }
  ) async throws {
    if !isActive() {
      guard activate() else {
        throw HelperError.activationFailed("application rejected activation: \(bundleID)")
      }
      for _ in 0..<200 {
        if isActive() { break }
        try await wait()
      }
      guard isActive() else {
        throw HelperError.activationFailed("application did not become foreground: \(bundleID)")
      }
    }
    if windowsReady(), isActive() { return }
    guard isActive() else {
      throw HelperError.activationFailed(
        "foreground changed before window raise: \(bundleID); stopped without reactivating")
    }
    let errors = try raiseWindows()
    for attempt in 0...40 {
      guard isActive() else {
        throw HelperError.activationFailed(
          "foreground changed while showing windows: \(bundleID); stopped without reactivating")
      }
      if windowsReady() { return }
      if attempt < 40 { try await wait() }
    }
    let detail = errors.isEmpty ? "AXRaise returned success" : errors.joined(separator: "; ")
    throw HelperError.windowRaiseFailed(
      "target windows are still missing or covered: \(bundleID); \(detail)")
  }
}
