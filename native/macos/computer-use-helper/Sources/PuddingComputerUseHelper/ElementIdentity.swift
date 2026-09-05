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
    parentID: String? = nil
  ) -> String {
    let fields: [String]
    let scope = [String(windowIndex), parentID ?? "", role ?? "", subrole ?? ""]
    if role == "AXWindow" {
      // Window titles often change after an action (document edits, navigation, etc.).
      // The request's windowID already scopes the tree; never rename all descendants.
      fields = ["window", String(windowIndex)]
    } else if let identifier = normalized(identifier) {
      fields = ["identifier"] + scope + [identifier]
    } else if role == "AXRow" {
      // Visible rows have absolute AX indices. Equal row titles are not unique keys.
      fields = ["row"] + scope + [path.map(String.init).joined(separator: ".")]
    } else if let label = normalized(label) {
      fields = ["label"] + scope + [label]
    } else {
      fields = ["path"] + scope + [path.map(String.init).joined(separator: ".")]
    }
    let payload =
      fields
      .map { "\($0.utf8.count):\($0)" }
      .joined()
    let digest = SHA256.hash(data: Data(payload.utf8))
    return "e_" + digest.prefix(16).map { String(format: "%02x", $0) }.joined()
  }

  private static func normalized(_ value: String?) -> String? {
    guard let value else { return nil }
    let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
    return trimmed.isEmpty ? nil : trimmed
  }
}
