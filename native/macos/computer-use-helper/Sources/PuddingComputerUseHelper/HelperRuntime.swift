import Foundation

final class HelperRuntime {
  private let accessibility = AccessibilityService()
  private let screenCapture = ScreenCaptureService()
  private let applicationLifecycle = ApplicationLifecycleService()

  func execute(_ command: HelperCommand) async throws -> AnyEncodable {
    switch command {
    case .serve:
      throw ArgumentError.unknownCommand("serve")
    case .permissions(let promptAccessibility, let promptScreenRecording):
      return AnyEncodable(
        ComputerPermissions.snapshot(
          promptAccessibility: promptAccessibility,
          promptScreenRecording: promptScreenRecording
        ))
    case .listApps:
      let permissions = ComputerPermissions.snapshot(
        promptAccessibility: false,
        promptScreenRecording: false
      )
      let inventory =
        permissions.screenRecording
        ? try await screenCapture.inventory()
        : nil
      return AnyEncodable(
        ListAppsOutput(
          permissions: permissions,
          apps: accessibility.listApplications(current: inventory?.applications),
          capturableWindows: inventory?.windows ?? []
        ))
    case .launchApp(let bundleID):
      return AnyEncodable(try await applicationLifecycle.launch(bundleID: bundleID))
    case .quitApp(let bundleID, let pid):
      return AnyEncodable(try await applicationLifecycle.quit(bundleID: bundleID, pid: pid))
    case .observe(let bundleID, let windowID, let maxElements):
      let targetWindow = try await screenCapture.window(
        bundleID: bundleID,
        windowID: windowID ?? 0
      )
      guard let resolvedWindowID = targetWindow?.windowID ?? windowID else {
        return AnyEncodable(
          try accessibility.observe(
            bundleID: bundleID,
            windowID: nil,
            targetWindow: nil,
            maxElements: maxElements
          ))
      }
      return try await withWindowActivity(
        bundleID: bundleID,
        windowID: resolvedWindowID,
        operation: {
          try accessibility.observe(
            bundleID: bundleID,
            windowID: windowID,
            targetWindow: targetWindow,
            maxElements: maxElements
          )
        })
    case .capture(let bundleID, let windowID, let output):
      return try await withWindowActivity(
        bundleID: bundleID,
        windowID: windowID,
        operation: {
          try await screenCapture.capture(bundleID: bundleID, windowID: windowID, output: output)
        })
    case .act(let bundleID, let windowID, let elementID, let action, let value):
      let targetWindow = try await screenCapture.window(bundleID: bundleID, windowID: windowID)
      return try await withWindowActivity(
        bundleID: bundleID,
        windowID: windowID,
        operation: {
          try accessibility.act(
            bundleID: bundleID,
            windowID: windowID,
            targetWindow: targetWindow,
            elementID: elementID,
            action: action,
            value: value
          )
        })
    }
  }

  private func withWindowActivity<T: Encodable>(
    bundleID: String,
    windowID: UInt32,
    operation: () async throws -> T
  ) async throws -> AnyEncodable {
    let activityID = await WindowActivityIndicator.shared.begin(
      bundleID: bundleID,
      windowID: windowID
    )
    do {
      let result = try await operation()
      await WindowActivityIndicator.shared.finish(activityID)
      return AnyEncodable(result)
    } catch {
      await WindowActivityIndicator.shared.finish(activityID)
      throw error
    }
  }
}

struct AnyEncodable: Encodable {
  private let encodeValue: (Encoder) throws -> Void

  init<T: Encodable>(_ value: T) {
    encodeValue = value.encode(to:)
  }

  func encode(to encoder: Encoder) throws {
    try encodeValue(encoder)
  }
}
