import Foundation

enum WindowResolver {
  static func resolveIndex(
    requestedWindowID: UInt32,
    targetWindow: CapturableWindowSnapshot?,
    candidates: [ApplicationWindowSnapshot]
  ) -> Int? {
    if let directMatch = candidates.first(where: { $0.windowID == requestedWindowID }) {
      return directMatch.index
    }
    guard let targetWindow else {
      return nil
    }

    let frameMatches = candidates.filter { candidate in
      guard let frame = candidate.frame else { return false }
      return framesMatch(frame, targetWindow.frame)
    }
    if frameMatches.count == 1 {
      return frameMatches[0].index
    }

    guard let targetTitle = targetWindow.title, !targetTitle.isEmpty else {
      return nil
    }
    let titleMatches = frameMatches.filter { candidate in
      guard let title = candidate.title, !title.isEmpty else { return false }
      return title == targetTitle
    }
    guard titleMatches.count == 1 else {
      return nil
    }
    return titleMatches[0].index
  }

  private static func framesMatch(_ source: FrameSnapshot, _ target: FrameSnapshot) -> Bool {
    abs(source.x - target.x) <= 2
      && abs(source.y - target.y) <= 2
      && abs(source.width - target.width) <= 2
      && abs(source.height - target.height) <= 2
  }
}
