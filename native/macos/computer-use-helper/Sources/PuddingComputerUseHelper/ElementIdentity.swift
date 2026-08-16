import CryptoKit
import Foundation

struct ElementPathComponent: Equatable {
  let index: Int
  let stable: Bool
}

enum ElementIdentity {
  static func visibleRowPathComponents(indices: [Int?]) -> [ElementPathComponent] {
    let resolved = indices.compactMap { $0 }
    let stable = resolved.count == indices.count
      && resolved.allSatisfy { $0 >= 0 }
      && Set(resolved).count == resolved.count
    if stable {
      return resolved.map { ElementPathComponent(index: $0, stable: true) }
    }
    return indices.indices.map { ElementPathComponent(index: $0, stable: false) }
  }

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
