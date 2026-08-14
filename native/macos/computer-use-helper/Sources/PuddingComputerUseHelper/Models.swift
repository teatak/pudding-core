import Foundation

struct PermissionSnapshot: Codable, Equatable {
  let accessibility: Bool
  let screenRecording: Bool
}

struct ListAppsOutput: Codable, Equatable {
  let permissions: PermissionSnapshot
  let apps: [RunningApplicationSnapshot]
  let capturableWindows: [CapturableWindowSnapshot]
}

struct RunningApplicationSnapshot: Codable, Equatable {
  let bundleID: String
  let name: String
  let pid: Int32
  let active: Bool
  let controllable: Bool
  let windows: [ApplicationWindowSnapshot]
}

struct ApplicationIdentitySnapshot: Codable, Equatable {
  let bundleID: String
  let name: String
  let iconPNGBase64: String?
}

struct ApplicationWindowSnapshot: Codable, Equatable {
  let index: Int
  let windowID: UInt32?
  let title: String?
  let frame: FrameSnapshot?
}

struct ObservationSnapshot: Codable, Equatable {
  let bundleID: String
  let windowID: UInt32?
  let name: String
  let pid: Int32
  let observedAt: Date
  let truncated: Bool
  let windows: [ApplicationWindowSnapshot]
  let elements: [ObservedElementSnapshot]
}

struct ObservedElementSnapshot: Codable, Equatable {
  let elementID: String
  let windowIndex: Int
  let windowID: UInt32?
  let role: String?
  let subrole: String?
  let label: String?
  let description: String?
  let value: String?
  let secure: Bool
  let enabled: Bool?
  let focused: Bool?
  let frame: FrameSnapshot?
  let actions: [String]
}

struct FrameSnapshot: Codable, Equatable {
  let x: Double
  let y: Double
  let width: Double
  let height: Double
}

struct CapturableWindowSnapshot: Codable, Equatable {
  let windowID: UInt32
  let bundleID: String?
  let applicationName: String?
  let title: String?
  let frame: FrameSnapshot
}

struct CaptureSnapshot: Codable, Equatable {
  let windowID: UInt32
  let output: String
  let width: Int
  let height: Int
  let scaleFactor: Double
}

struct UseApplicationSnapshot: Codable, Equatable {
  let bundleID: String
  let name: String
  let pid: Int32
  let newlyLaunched: Bool
}

struct QuitApplicationSnapshot: Codable, Equatable {
  let bundleID: String
  let pid: Int32
  let closed: Bool
}

struct ActionSnapshot: Codable, Equatable {
  let bundleID: String
  let elementID: String
  let action: ElementAction
  let completed: Bool
}

enum ActionOutcome: String, Codable, Equatable {
  case notStarted = "not_started"
  case unknown
}

struct ErrorDetail: Codable, Equatable {
  let code: String
  let message: String
  let retryable: Bool
  let outcome: ActionOutcome
}

struct ErrorSnapshot: Codable, Equatable {
  let ok: Bool
  let code: String
  let message: String
  let retryable: Bool
  let outcome: ActionOutcome
}

enum HelperError: Error, LocalizedError {
  case permissionRequired(String)
  case appNotFound(String)
  case appNotAllowed(String)
  case windowNotFound(UInt32)
  case elementNotFound(String)
  case elementNotActionable(String)
  case ambiguousElement(String)
  case actionFailed(String)
  case captureFailed(String)
  case applicationNotInstalled(String)
  case useFailed(String)

  var code: String {
    switch self {
    case .permissionRequired:
      return "computer_permission_required"
    case .appNotFound:
      return "computer_app_not_found"
    case .appNotAllowed:
      return "computer_action_blocked"
    case .windowNotFound:
      return "computer_window_not_found"
    case .elementNotFound:
      return "computer_element_not_found"
    case .elementNotActionable, .ambiguousElement:
      return "computer_element_not_actionable"
    case .actionFailed:
      return "computer_action_failed"
    case .captureFailed:
      return "computer_capture_failed"
    case .applicationNotInstalled:
      return "computer_app_not_installed"
    case .useFailed:
      return "computer_use_failed"
    }
  }

  var errorDescription: String? {
    switch self {
    case .permissionRequired(let permission):
      return "permission required: \(permission)"
    case .appNotFound(let bundleID):
      return "application not found: \(bundleID)"
    case .appNotAllowed(let bundleID):
      return "application is not allowed: \(bundleID)"
    case .windowNotFound(let windowID):
      return "window not found: \(windowID)"
    case .elementNotFound(let elementID):
      return "element not found: \(elementID)"
    case .elementNotActionable(let reason):
      return "element is not actionable: \(reason)"
    case .ambiguousElement(let reason):
      return "element is ambiguous: \(reason)"
    case .actionFailed(let reason):
      return "action failed: \(reason)"
    case .captureFailed(let reason):
      return "capture failed: \(reason)"
    case .applicationNotInstalled(let bundleID):
      return "application is not installed: \(bundleID)"
    case .useFailed(let reason):
      return "application launch failed: \(reason)"
    }
  }

  var outcome: ActionOutcome {
    switch self {
    case .actionFailed, .useFailed:
      return .unknown
    default:
      return .notStarted
    }
  }
}

func errorDetail(for error: Error) -> ErrorDetail {
  let helperError = error as? HelperError
  return ErrorDetail(
    code: helperError?.code ?? "computer_invalid_request",
    message: error.localizedDescription,
    retryable: false,
    outcome: helperError?.outcome ?? .notStarted
  )
}

enum ValueSanitizer {
  static func string(_ value: Any?, secure: Bool, limit: Int = 500) -> String? {
    guard !secure else { return nil }
    let raw: String
    switch value {
    case let text as String:
      raw = text
    case let number as NSNumber:
      raw = number.stringValue
    default:
      return nil
    }
    guard raw.count > limit else { return raw }
    return String(raw.prefix(limit))
  }
}
