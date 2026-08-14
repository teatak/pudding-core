import Foundation

enum HelperCommand: Equatable {
  case serve
  case permissions(promptAccessibility: Bool, promptScreenRecording: Bool)
  case listApps
  case observe(bundleID: String, windowID: UInt32?, maxElements: Int)
  case capture(bundleID: String, windowID: UInt32, output: String)
  case launchApp(bundleID: String)
  case quitApp(bundleID: String, pid: Int32)
  case act(
    bundleID: String,
    windowID: UInt32,
    elementID: String,
    action: ElementAction,
    value: String?
  )
}

enum ElementAction: String, Codable, Equatable {
  case press
  case setValue = "set_value"
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
        windowID: windowID,
        maxElements: maxElements
      )
    case "capture":
      var bundleID: String?
      var windowID: UInt32?
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
        case "--output":
          output = try cursor.requireValue(option)
        default:
          throw ArgumentError.unknownOption(option)
        }
      }
      return .capture(
        bundleID: try require(bundleID, "--bundle-id"),
        windowID: try require(windowID, "--window-id"),
        output: try require(output, "--output")
      )
    case "launch-app":
      var bundleID: String?
      while let option = cursor.next() {
        switch option {
        case "--bundle-id":
          bundleID = try cursor.requireValue(option)
        default:
          throw ArgumentError.unknownOption(option)
        }
      }
      return .launchApp(bundleID: try require(bundleID, "--bundle-id"))
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
      if resolvedAction == .press, value != nil {
        throw ArgumentError.invalidOption("--value", "not allowed for press")
      }
      return .act(
        bundleID: try require(bundleID, "--bundle-id"),
        windowID: try require(windowID, "--window-id"),
        elementID: try require(elementID, "--element-id"),
        action: resolvedAction,
        value: value
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
