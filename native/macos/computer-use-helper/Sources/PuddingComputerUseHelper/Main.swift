import Darwin
import Foundation

@main
struct PuddingComputerUseHelper {
  static func main() async {
    do {
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
