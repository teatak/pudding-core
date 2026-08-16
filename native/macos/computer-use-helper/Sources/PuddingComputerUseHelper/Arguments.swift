import Foundation

enum HelperCommand: Equatable {
  case serve
  case permissions(promptAccessibility: Bool, promptScreenRecording: Bool)
  case listApps
  case applicationIdentity(bundleID: String)
  case observe(bundleID: String, windowID: UInt32, maxElements: Int)
  case observeCapture(bundleID: String, windowID: UInt32, maxElements: Int, output: String)
  case useApp(bundleID: String, foreground: Bool)
  case quitApp(bundleID: String, pid: Int32)
  case act(
    bundleID: String,
    windowID: UInt32,
    elementID: String,
    action: ElementAction,
    value: String?
  )
  case pointer(
    bundleID: String,
    windowID: UInt32,
    input: PointerInput,
    captureWidth: Int,
    captureHeight: Int,
    scaleFactor: Double
  )
}

enum PointerAction: String, Codable, Equatable {
  case click
  case drag
  case scroll
}

enum PointerButton: String, Codable, Equatable {
  case left
  case right
}

struct PointerInput: Equatable {
  let action: PointerAction
  let x: Double
  let y: Double
  let toX: Double?
  let toY: Double?
  let button: PointerButton?
  let clickCount: Int?
  let deltaX: Int?
  let deltaY: Int?

  static func validated(
    action: PointerAction,
    x: Double,
    y: Double,
    toX: Double?,
    toY: Double?,
    button: PointerButton?,
    clickCount: Int?,
    deltaX: Int?,
    deltaY: Int?
  ) throws -> PointerInput {
    guard x.isFinite, y.isFinite, x >= 0, y >= 0 else {
      throw ArgumentError.invalidOption("coordinates", "must be finite and non-negative")
    }
    switch action {
    case .click:
      let resolvedButton = button ?? .left
      let resolvedCount = clickCount ?? 1
      guard toX == nil, toY == nil, deltaX == nil, deltaY == nil else {
        throw ArgumentError.invalidOption("action", "click accepts only button and clickCount")
      }
      guard resolvedCount == 1 || (resolvedCount == 2 && resolvedButton == .left) else {
        throw ArgumentError.invalidOption("clickCount", "must be 1, or 2 for the left button")
      }
      return PointerInput(
        action: action, x: x, y: y, toX: nil, toY: nil,
        button: resolvedButton, clickCount: resolvedCount, deltaX: nil, deltaY: nil)
    case .drag:
      guard let toX, let toY, toX.isFinite, toY.isFinite, toX >= 0, toY >= 0 else {
        throw ArgumentError.invalidOption("drag", "finite non-negative toX and toY are required")
      }
      guard button == nil, clickCount == nil, deltaX == nil, deltaY == nil else {
        throw ArgumentError.invalidOption("action", "drag accepts only start and end coordinates")
      }
      return PointerInput(
        action: action, x: x, y: y, toX: toX, toY: toY,
        button: nil, clickCount: nil, deltaX: nil, deltaY: nil)
    case .scroll:
      let resolvedX = deltaX ?? 0
      let resolvedY = deltaY ?? 0
      guard toX == nil, toY == nil, button == nil, clickCount == nil else {
        throw ArgumentError.invalidOption("action", "scroll accepts only coordinates and deltas")
      }
      guard resolvedX != 0 || resolvedY != 0 else {
        throw ArgumentError.invalidOption("scroll", "deltaX or deltaY must be non-zero")
      }
      guard resolvedX >= -5_000, resolvedX <= 5_000,
        resolvedY >= -5_000, resolvedY <= 5_000
      else {
        throw ArgumentError.invalidOption("scroll", "deltas must be between -5000 and 5000")
      }
      return PointerInput(
        action: action, x: x, y: y, toX: nil, toY: nil,
        button: nil, clickCount: nil, deltaX: resolvedX, deltaY: resolvedY)
    }
  }
}

enum ElementAction: String, Codable, Equatable {
  case press
  case setValue = "set_value"
  case select
  case submit
}

enum ArgumentError: Error, LocalizedError, Equatable {
  case missingCommand
  case unknownCommand(String)
  case missingOption(String)
  case invalidOption(String, String)
  case unknownOption(String)

  var errorDescription: String? {
    switch self {
    case .missingCommand:
      return "command is required"
    case .unknownCommand(let command):
      return "unknown command: \(command)"
    case .missingOption(let option):
      return "missing required option: \(option)"
    case .invalidOption(let option, let value):
      return "invalid value for \(option): \(value)"
    case .unknownOption(let option):
      return "unknown option: \(option)"
    }
  }
}

struct ArgumentParser {
  static func parse(_ arguments: [String]) throws -> HelperCommand {
    guard let command = arguments.first else {
      throw ArgumentError.missingCommand
    }
    var cursor = OptionCursor(Array(arguments.dropFirst()))
    switch command {
    case "serve":
      try cursor.requireEmpty()
      return .serve
    case "permissions":
      var promptAccessibility = false
      var promptScreenRecording = false
      while let option = cursor.next() {
        switch option {
        case "--prompt-accessibility":
          promptAccessibility = true
        case "--prompt-screen-recording":
          promptScreenRecording = true
        default:
          throw ArgumentError.unknownOption(option)
        }
      }
      return .permissions(
        promptAccessibility: promptAccessibility,
        promptScreenRecording: promptScreenRecording
      )
    case "list-apps":
      try cursor.requireEmpty()
      return .listApps
    case "app-identity":
      var bundleID: String?
      while let option = cursor.next() {
        switch option {
        case "--bundle-id":
          bundleID = try cursor.requireValue(option)
        default:
          throw ArgumentError.unknownOption(option)
        }
      }
      return .applicationIdentity(bundleID: try require(bundleID, "--bundle-id"))
    case "observe":
      var bundleID: String?
      var windowID: UInt32?
      var maxElements = 200
      while let option = cursor.next() {
        switch option {
        case "--bundle-id":
          bundleID = try cursor.requireValue(option)
        case "--window-id":
          let raw = try cursor.requireValue(option)
          guard let parsed = UInt32(raw), parsed > 0 else {
            throw ArgumentError.invalidOption(option, raw)
          }
          windowID = parsed
        case "--max-elements":
          let raw = try cursor.requireValue(option)
          guard let parsed = Int(raw), (1...1_000).contains(parsed) else {
            throw ArgumentError.invalidOption(option, raw)
          }
          maxElements = parsed
        default:
          throw ArgumentError.unknownOption(option)
        }
      }
      return .observe(
        bundleID: try require(bundleID, "--bundle-id"),
        windowID: try require(windowID, "--window-id"),
        maxElements: maxElements
      )
    case "observe-capture":
      var bundleID: String?
      var windowID: UInt32?
      var maxElements = 200
      var output: String?
      while let option = cursor.next() {
        switch option {
        case "--bundle-id":
          bundleID = try cursor.requireValue(option)
        case "--window-id":
          let raw = try cursor.requireValue(option)
          guard let parsed = UInt32(raw), parsed > 0 else {
            throw ArgumentError.invalidOption(option, raw)
          }
          windowID = parsed
        case "--max-elements":
          let raw = try cursor.requireValue(option)
          guard let parsed = Int(raw), (1...1_000).contains(parsed) else {
            throw ArgumentError.invalidOption(option, raw)
          }
          maxElements = parsed
        case "--output":
          output = try cursor.requireValue(option)
        default:
          throw ArgumentError.unknownOption(option)
        }
      }
      return .observeCapture(
        bundleID: try require(bundleID, "--bundle-id"),
        windowID: try require(windowID, "--window-id"),
        maxElements: maxElements,
        output: try require(output, "--output")
      )
    case "use-app":
      var bundleID: String?
      var foreground = false
      while let option = cursor.next() {
        switch option {
        case "--bundle-id":
          bundleID = try cursor.requireValue(option)
        case "--foreground":
          foreground = true
        default:
          throw ArgumentError.unknownOption(option)
        }
      }
      return .useApp(
        bundleID: try require(bundleID, "--bundle-id"),
        foreground: foreground
      )
    case "quit-app":
      var bundleID: String?
      var pid: Int32?
      while let option = cursor.next() {
        switch option {
        case "--bundle-id":
          bundleID = try cursor.requireValue(option)
        case "--pid":
          let raw = try cursor.requireValue(option)
          guard let parsed = Int32(raw), parsed > 0 else {
            throw ArgumentError.invalidOption(option, raw)
          }
          pid = parsed
        default:
          throw ArgumentError.unknownOption(option)
        }
      }
      return .quitApp(
        bundleID: try require(bundleID, "--bundle-id"),
        pid: try require(pid, "--pid")
      )
    case "act":
      var bundleID: String?
      var windowID: UInt32?
      var elementID: String?
      var action: ElementAction?
      var value: String?
      while let option = cursor.next() {
        switch option {
        case "--bundle-id":
          bundleID = try cursor.requireValue(option)
        case "--window-id":
          let raw = try cursor.requireValue(option)
          guard let parsed = UInt32(raw), parsed > 0 else {
            throw ArgumentError.invalidOption(option, raw)
          }
          windowID = parsed
        case "--element-id":
          elementID = try cursor.requireValue(option)
        case "--action":
          let raw = try cursor.requireValue(option)
          guard let parsed = ElementAction(rawValue: raw) else {
            throw ArgumentError.invalidOption(option, raw)
          }
          action = parsed
        case "--value":
          value = try cursor.requireValue(option)
        default:
          throw ArgumentError.unknownOption(option)
        }
      }
      let resolvedAction = try require(action, "--action")
      if resolvedAction == .setValue, value == nil {
        throw ArgumentError.missingOption("--value")
      }
      if resolvedAction != .setValue, value != nil {
        throw ArgumentError.invalidOption("--value", "allowed only for set_value")
      }
      return .act(
        bundleID: try require(bundleID, "--bundle-id"),
        windowID: try require(windowID, "--window-id"),
        elementID: try require(elementID, "--element-id"),
        action: resolvedAction,
        value: value
      )
    case "pointer":
      var bundleID: String?
      var windowID: UInt32?
      var action: PointerAction?
      var x: Double?
      var y: Double?
      var toX: Double?
      var toY: Double?
      var button: PointerButton?
      var clickCount: Int?
      var deltaX: Int?
      var deltaY: Int?
      var captureWidth: Int?
      var captureHeight: Int?
      var scaleFactor: Double?
      while let option = cursor.next() {
        let raw: String
        switch option {
        case "--bundle-id":
          bundleID = try cursor.requireValue(option)
          continue
        case "--window-id":
          raw = try cursor.requireValue(option)
          guard let parsed = UInt32(raw), parsed > 0 else {
            throw ArgumentError.invalidOption(option, raw)
          }
          windowID = parsed
          continue
        case "--action":
          raw = try cursor.requireValue(option)
          guard let parsed = PointerAction(rawValue: raw) else {
            throw ArgumentError.invalidOption(option, raw)
          }
          action = parsed
        case "--button":
          raw = try cursor.requireValue(option)
          guard let parsed = PointerButton(rawValue: raw) else {
            throw ArgumentError.invalidOption(option, raw)
          }
          button = parsed
        case "--x", "--y", "--to-x", "--to-y", "--scale-factor":
          raw = try cursor.requireValue(option)
          guard let parsed = Double(raw), parsed.isFinite, parsed >= 0 else {
            throw ArgumentError.invalidOption(option, raw)
          }
          if option == "--x" { x = parsed }
          if option == "--y" { y = parsed }
          if option == "--to-x" { toX = parsed }
          if option == "--to-y" { toY = parsed }
          if option == "--scale-factor" { scaleFactor = parsed }
        case "--click-count":
          raw = try cursor.requireValue(option)
          guard let parsed = Int(raw) else {
            throw ArgumentError.invalidOption(option, raw)
          }
          clickCount = parsed
        case "--delta-x", "--delta-y":
          raw = try cursor.requireValue(option)
          guard let parsed = Int(raw) else {
            throw ArgumentError.invalidOption(option, raw)
          }
          if option == "--delta-x" { deltaX = parsed }
          if option == "--delta-y" { deltaY = parsed }
        case "--capture-width", "--capture-height":
          raw = try cursor.requireValue(option)
          guard let parsed = Int(raw), parsed > 0 else {
            throw ArgumentError.invalidOption(option, raw)
          }
          if option == "--capture-width" { captureWidth = parsed }
          if option == "--capture-height" { captureHeight = parsed }
        default:
          throw ArgumentError.unknownOption(option)
        }
      }
      let resolvedScaleFactor = try require(scaleFactor, "--scale-factor")
      guard resolvedScaleFactor > 0 else {
        throw ArgumentError.invalidOption("--scale-factor", String(resolvedScaleFactor))
      }
      let input = try PointerInput.validated(
        action: try require(action, "--action"),
        x: try require(x, "--x"),
        y: try require(y, "--y"),
        toX: toX,
        toY: toY,
        button: button,
        clickCount: clickCount,
        deltaX: deltaX,
        deltaY: deltaY
      )
      return .pointer(
        bundleID: try require(bundleID, "--bundle-id"),
        windowID: try require(windowID, "--window-id"),
        input: input,
        captureWidth: try require(captureWidth, "--capture-width"),
        captureHeight: try require(captureHeight, "--capture-height"),
        scaleFactor: resolvedScaleFactor
      )
    default:
      throw ArgumentError.unknownCommand(command)
    }
  }

  private static func require<T>(_ value: T?, _ option: String) throws -> T {
    guard let value else {
      throw ArgumentError.missingOption(option)
    }
    return value
  }
}

private struct OptionCursor {
  private let arguments: [String]
  private var index = 0

  init(_ arguments: [String]) {
    self.arguments = arguments
  }

  mutating func next() -> String? {
    guard index < arguments.count else { return nil }
    defer { index += 1 }
    return arguments[index]
  }

  mutating func requireValue(_ option: String) throws -> String {
    guard let value = next(), !value.hasPrefix("--") else {
      throw ArgumentError.missingOption(option)
    }
    return value
  }

  mutating func requireEmpty() throws {
    if let option = next() {
      throw ArgumentError.unknownOption(option)
    }
  }
}
