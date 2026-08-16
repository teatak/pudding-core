import Foundation

private let maximumRequestBytes = 1024 * 1024
private let maximumResponseBytes = 1024 * 1024
private let maximumActionValueCharacters = 20_000

struct ProtocolRequest: Decodable, Equatable {
  let id: String
  let command: String
  let params: ProtocolParameters?

  func helperCommand() throws -> HelperCommand {
    guard !id.isEmpty, id.utf8.count <= 128 else {
      throw ArgumentError.invalidOption("id", id)
    }
    let params = params ?? ProtocolParameters()
    switch command {
    case "permissions":
      return .permissions(
        promptAccessibility: params.promptAccessibility ?? false,
        promptScreenRecording: params.promptScreenRecording ?? false
      )
    case "list_apps":
      return .listApps
    case "app_identity":
      return .applicationIdentity(bundleID: try bundleID(params.bundleID))
    case "use_app":
      return .useApp(
        bundleID: try bundleID(params.bundleID),
        foreground: params.foreground ?? false
      )
    case "quit_app":
      let pid = try required(params.pid, "pid")
      guard pid > 0 else {
        throw ArgumentError.invalidOption("pid", String(pid))
      }
      return .quitApp(
        bundleID: try bundleID(params.bundleID),
        pid: pid
      )
    case "observe":
      let bundleID = try bundleID(params.bundleID)
      let maxElements = params.maxElements ?? 200
      guard (1...1_000).contains(maxElements) else {
        throw ArgumentError.invalidOption("maxElements", String(maxElements))
      }
      return .observe(
        bundleID: bundleID,
        windowID: try positiveWindowID(params.windowID),
        maxElements: maxElements
      )
    case "observe_capture":
      let maxElements = params.maxElements ?? 200
      guard (1...1_000).contains(maxElements) else {
        throw ArgumentError.invalidOption("maxElements", String(maxElements))
      }
      return .observeCapture(
        bundleID: try bundleID(params.bundleID),
        windowID: try positiveWindowID(params.windowID),
        maxElements: maxElements,
        output: try required(params.output, "output")
      )
    case "act":
      let rawAction = try required(params.action, "action")
      guard let action = ElementAction(rawValue: rawAction) else {
        throw ArgumentError.invalidOption("action", rawAction)
      }
      if action == .setValue, params.value == nil {
        throw ArgumentError.missingOption("value")
      }
      if action != .setValue, params.value != nil {
        throw ArgumentError.invalidOption("value", "allowed only for set_value")
      }
      if let value = params.value,
        value.unicodeScalars.count > maximumActionValueCharacters
      {
        throw ArgumentError.invalidOption("value", "too long")
      }
      return .act(
        bundleID: try bundleID(params.bundleID),
        windowID: try positiveWindowID(params.windowID),
        elementID: try required(params.elementID, "elementID"),
        action: action,
        value: params.value
      )
    case "pointer":
      let rawAction = try required(params.action, "action")
      guard let action = PointerAction(rawValue: rawAction) else {
        throw ArgumentError.invalidOption("action", rawAction)
      }
      let x = try required(params.x, "x")
      let y = try required(params.y, "y")
      let captureWidth = try required(params.captureWidth, "captureWidth")
      let captureHeight = try required(params.captureHeight, "captureHeight")
      let scaleFactor = try required(params.scaleFactor, "scaleFactor")
      guard captureWidth > 0, captureHeight > 0, scaleFactor.isFinite, scaleFactor > 0 else {
        throw ArgumentError.invalidOption("capture", "dimensions and scale factor must be positive")
      }
      let button: PointerButton?
      if let rawButton = params.button {
        guard let parsed = PointerButton(rawValue: rawButton) else {
          throw ArgumentError.invalidOption("button", rawButton)
        }
        button = parsed
      } else {
        button = nil
      }
      let input = try PointerInput.validated(
        action: action,
        x: x,
        y: y,
        toX: params.toX,
        toY: params.toY,
        button: button,
        clickCount: params.clickCount,
        deltaX: params.deltaX,
        deltaY: params.deltaY
      )
      return .pointer(
        bundleID: try bundleID(params.bundleID),
        windowID: try positiveWindowID(params.windowID),
        input: input,
        captureWidth: captureWidth,
        captureHeight: captureHeight,
        scaleFactor: scaleFactor
      )
    default:
      throw ArgumentError.unknownCommand(command)
    }
  }

  private func required<T>(_ value: T?, _ option: String) throws -> T {
    guard let value else {
      throw ArgumentError.missingOption(option)
    }
    if let text = value as? String, text.isEmpty {
      throw ArgumentError.missingOption(option)
    }
    return value
  }

  private func positiveWindowID(_ value: UInt32?) throws -> UInt32 {
    let windowID = try required(value, "windowID")
    guard windowID > 0 else {
      throw ArgumentError.invalidOption("windowID", String(windowID))
    }
    return windowID
  }

  private func bundleID(_ value: String?) throws -> String {
    let bundleID = try required(value, "bundleID")
    guard bundleID.utf8.count <= 512 else {
      throw ArgumentError.invalidOption("bundleID", "too long")
    }
    return bundleID
  }
}

struct ProtocolParameters: Codable, Equatable {
  var promptAccessibility: Bool? = nil
  var promptScreenRecording: Bool? = nil
  var bundleID: String? = nil
  var foreground: Bool? = nil
  var maxElements: Int? = nil
  var windowID: UInt32? = nil
  var output: String? = nil
  var elementID: String? = nil
  var action: String? = nil
  var value: String? = nil
  var pid: Int32? = nil
  var x: Double? = nil
  var y: Double? = nil
  var toX: Double? = nil
  var toY: Double? = nil
  var button: String? = nil
  var clickCount: Int? = nil
  var deltaX: Int? = nil
  var deltaY: Int? = nil
  var captureWidth: Int? = nil
  var captureHeight: Int? = nil
  var scaleFactor: Double? = nil
}

private struct ProtocolSuccess: Encodable {
  let id: String
  let ok = true
  let result: AnyEncodable
}

private struct ProtocolFailure: Encodable {
  let id: String?
  let ok = false
  let error: ErrorDetail
}

final class ProtocolServer {
  private let runtime: HelperRuntime

  init(runtime: HelperRuntime) {
    self.runtime = runtime
  }

  func run() async {
    while let line = readLine(strippingNewline: true) {
      var requestID: String?
      do {
        guard line.utf8.count <= maximumRequestBytes else {
          throw ArgumentError.invalidOption("request", "too large")
        }
        let request = try JSONDecoder().decode(ProtocolRequest.self, from: Data(line.utf8))
        requestID = request.id.utf8.count <= 128 ? request.id : nil
        let result = try await runtime.execute(request.helperCommand())
        try writeJSON(ProtocolSuccess(id: request.id, result: result))
      } catch {
        do {
          try writeJSON(ProtocolFailure(id: requestID, error: errorDetail(for: error)))
        } catch {
          return
        }
      }
    }
  }
}

func writeJSON<T: Encodable>(_ value: T) throws {
  let data = try JSONEncoder.pudding.encode(value)
  guard data.count <= maximumResponseBytes else {
    throw HelperError.responseTooLarge
  }
  FileHandle.standardOutput.write(data)
  FileHandle.standardOutput.write(Data([0x0A]))
}

extension JSONEncoder {
  static var pudding: JSONEncoder {
    let encoder = JSONEncoder()
    encoder.dateEncodingStrategy = .iso8601
    encoder.outputFormatting = [.sortedKeys]
    return encoder
  }
}
