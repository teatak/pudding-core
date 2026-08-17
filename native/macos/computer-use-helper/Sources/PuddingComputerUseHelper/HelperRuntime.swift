import Foundation

final class HelperRuntime {
  private let accessibility = AccessibilityService()
  private let screenCapture = ScreenCaptureService()
  private let applicationLifecycle = ApplicationLifecycleService()
  private let pointer = PointerService()

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
      return AnyEncodable(ListAppsOutput(apps: applicationLifecycle.listApplications()))
    case .applicationIdentity(let bundleID):
      return AnyEncodable(try applicationLifecycle.identity(bundleID: bundleID))
    case .useApp(let bundleID, let foreground):
      return AnyEncodable(
        try await applicationLifecycle.use(bundleID: bundleID, foreground: foreground)
      )
    case .quitApp(let bundleID, let pid):
      return AnyEncodable(try await applicationLifecycle.quit(bundleID: bundleID, pid: pid))
    case .observe(let bundleID, let windowID, let maxElements):
      let targetWindow = try await screenCapture.window(
        bundleID: bundleID,
        windowID: windowID
      )
      return try await withWindowActivity(
        bundleID: bundleID,
        windowID: targetWindow.windowID,
        operation: {
          try accessibility.observe(
            bundleID: bundleID,
            windowID: targetWindow.windowID,
            targetWindow: targetWindow,
            maxElements: maxElements
          )
        })
    case .observeCapture(let bundleID, let windowID, let maxElements, let output):
      let targetWindow = try await screenCapture.window(
        bundleID: bundleID,
        windowID: windowID
      )
      return try await withWindowActivity(
        bundleID: bundleID,
        windowID: targetWindow.windowID,
        operation: {
          let observation = try accessibility.observe(
            bundleID: bundleID,
            windowID: targetWindow.windowID,
            targetWindow: targetWindow,
            maxElements: maxElements
          )
          let capture = try await screenCapture.capture(
            bundleID: bundleID,
            windowID: targetWindow.windowID,
            output: output
          )
          return ObservationCaptureSnapshot(
            observation: observation,
            capture: capture
          )
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
    case .pointer(
      let bundleID,
      let windowID,
      let input,
      let captureWidth,
      let captureHeight,
      let scaleFactor
    ):
      return try await withWindowActivity(
        bundleID: bundleID,
        windowID: windowID,
        operation: {
          let targetWindow = try await screenCapture.window(
            bundleID: bundleID,
            windowID: windowID
          )
          let currentScaleFactor = screenCapture.backingScaleFactor(
            for: CGRect(
              x: targetWindow.frame.x,
              y: targetWindow.frame.y,
              width: targetWindow.frame.width,
              height: targetWindow.frame.height
            ))
          let start = try PointerCoordinatePolicy.globalPoint(
            frame: targetWindow.frame,
            currentScaleFactor: currentScaleFactor,
            x: input.x,
            y: input.y,
            captureWidth: captureWidth,
            captureHeight: captureHeight,
            captureScaleFactor: scaleFactor
          )
          let end: CGPoint?
          if let toX = input.toX, let toY = input.toY {
            end = try PointerCoordinatePolicy.globalPoint(
              frame: targetWindow.frame,
              currentScaleFactor: currentScaleFactor,
              x: toX,
              y: toY,
              captureWidth: captureWidth,
              captureHeight: captureHeight,
              captureScaleFactor: scaleFactor
            )
          } else {
            end = nil
          }
          return try pointer.perform(
            bundleID: bundleID,
            pid: targetWindow.pid,
            windowID: windowID,
            input: input,
            start: start,
            end: end
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
