import ApplicationServices
import CoreGraphics
import Foundation

enum ComputerPermissions {
  static func snapshot(
    promptAccessibility: Bool,
    promptScreenRecording: Bool
  ) -> PermissionSnapshot {
    // kAXTrustedCheckOptionPrompt is imported as mutable C global and is
    // rejected by Swift 6 strict concurrency. Its documented CFString
    // value is stable and avoids reading shared mutable state here.
    let accessibilityOptions =
      [
        "AXTrustedCheckOptionPrompt": promptAccessibility
      ] as CFDictionary
    let accessibility = AXIsProcessTrustedWithOptions(accessibilityOptions)
    let screenRecording =
      promptScreenRecording
      ? CGRequestScreenCaptureAccess()
      : CGPreflightScreenCaptureAccess()
    return PermissionSnapshot(
      accessibility: accessibility,
      screenRecording: screenRecording
    )
  }
}
