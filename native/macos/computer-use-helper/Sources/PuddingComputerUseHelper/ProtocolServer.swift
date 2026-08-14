import Foundation

private let maximumRequestBytes = 1024 * 1024

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
      return .applicationIdentity(bundleID: try required(params.bundleID, "bundleID"))
    case "use_app":
      return .useApp(bundleID: try required(params.bundleID, "bundleID"))
    case "quit_app":
      let pid = try required(params.pid, "pid")
      guard pid > 0 else {
        throw ArgumentError.invalidOption("pid", String(pid))
      }
      return .quitApp(
        bundleID: try required(params.bundleID, "bundleID"),
        pid: pid
      )
    case "observe":
      let bundleID = try required(params.bundleID, "bundleID")
      let maxElements = params.maxElements ?? 200
      guard (1...1_000).contains(maxElements) else {
        throw ArgumentError.invalidOption("maxElements", String(maxElements))
      }
      return .observe(
        bundleID: bundleID,
        windowID: params.windowID,
        maxElements: maxElements
      )
    case "capture":
      let windowID = try required(params.windowID, "windowID")
      guard windowID > 0 else {
        throw ArgumentError.invalidOption("windowID", String(windowID))
      }
      return .capture(
        bundleID: try required(params.bundleID, "bundleID"),
        windowID: windowID,
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
      if action == .press, params.value != nil {
        throw ArgumentError.invalidOption("value", "not allowed for press")
      }
      return .act(
        bundleID: try required(params.bundleID, "bundleID"),
        windowID: try positiveWindowID(params.windowID),
        elementID: try required(params.elementID, "elementID"),
        action: action,
        value: params.value
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
}

struct ProtocolParameters: Codable, Equatable {
  var promptAccessibility: Bool? = nil
  var promptScreenRecording: Bool? = nil
  var bundleID: String? = nil
  var maxElements: Int? = nil
  var windowID: UInt32? = nil
  var output: String? = nil
  var elementID: String? = nil
  var action: String? = nil
  var value: String? = nil
  var pid: Int32? = nil
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
        requestID = request.id
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
