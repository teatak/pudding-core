import CryptoKit
import Foundation

enum ElementIdentity {
  static func make(
    windowIndex: Int,
    path: [Int],
    role: String?,
    subrole: String?,
    identifier: String?,
    label: String?,
    frame: FrameSnapshot?
  ) -> String {
    let fields = [
      String(windowIndex),
      path.map(String.init).joined(separator: "."),
      role ?? "",
      subrole ?? "",
      identifier ?? "",
      label ?? "",
      frame.map { "\($0.x),\($0.y),\($0.width),\($0.height)" } ?? "",
    ]
    let payload =
      fields
      .map { "\($0.utf8.count):\($0)" }
      .joined()
    let digest = SHA256.hash(data: Data(payload.utf8))
    return "e_" + digest.prefix(16).map { String(format: "%02x", $0) }.joined()
  }
}
