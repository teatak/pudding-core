import Darwin
import Foundation

// Keep the executable @main out of the module linked into the test bundle.
@MainActor
public enum HelperEntryPoint {
  public static func run() async {
    do {
      if Array(CommandLine.arguments.dropFirst()) == [BackgroundPointerWorker.command] {
        BackgroundPointerWorker.run()
        return
      }
      if Array(CommandLine.arguments.dropFirst()) == ["preview"] {
        try await WindowPreview().run()
        return
      }
      let command = try ArgumentParser.parse(Array(CommandLine.arguments.dropFirst()))
      let runtime = HelperRuntime()
      if command == .serve {
        await ProtocolServer(runtime: runtime).run()
        return
      }
      try writeJSON(try await runtime.execute(command))
    } catch {
      let detail = errorDetail(for: error)
      let snapshot = ErrorSnapshot(
        ok: false,
        code: detail.code,
        message: detail.message,
        permission: detail.permission,
        retryable: detail.retryable,
        outcome: detail.outcome
      )
      if let data = try? JSONEncoder.pudding.encode(snapshot) {
        FileHandle.standardError.write(data)
        FileHandle.standardError.write(Data([0x0A]))
      }
      exit(1)
    }
  }
}
