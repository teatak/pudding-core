import Testing

@testable import PuddingComputerUseHelper

private let targetFrame = FrameSnapshot(x: 336, y: -1232, width: 1385, height: 818)

@Test func windowResolverPrefersTheAccessibilityWindowID() {
  let candidates = [
    window(index: 0, windowID: 12448, title: "Other", frame: nil),
    window(index: 1, title: "Mail", frame: targetFrame),
  ]

  let resolved = WindowResolver.resolveIndex(
    requestedWindowID: 12448,
    targetWindow: target(title: "Mail"),
    candidates: candidates
  )

  #expect(resolved == 0)
}

@Test func windowResolverAcceptsAUniqueFrameWhenTheTitleChanges() {
  let candidates = [
    window(
      index: 0,
      title: "收件箱 — iCloud – 16,365封邮件",
      frame: targetFrame
    )
  ]

  let resolved = WindowResolver.resolveIndex(
    requestedWindowID: 12448,
    targetWindow: target(title: "收件箱 — iCloud"),
    candidates: candidates
  )

  #expect(resolved == 0)
}

@Test func windowResolverUsesTheTitleOnlyToDisambiguateEqualFrames() {
  let candidates = [
    window(index: 0, title: "Drafts", frame: targetFrame),
    window(index: 1, title: "Inbox", frame: targetFrame),
  ]

  let resolved = WindowResolver.resolveIndex(
    requestedWindowID: 12448,
    targetWindow: target(title: "Inbox"),
    candidates: candidates
  )

  #expect(resolved == 1)
}

@Test func windowResolverRejectsAnAmbiguousFrame() {
  let candidates = [
    window(index: 0, title: "Drafts", frame: targetFrame),
    window(index: 1, title: "Inbox", frame: targetFrame),
  ]

  let resolved = WindowResolver.resolveIndex(
    requestedWindowID: 12448,
    targetWindow: target(title: "Unknown"),
    candidates: candidates
  )

  #expect(resolved == nil)
}

private func window(
  index: Int,
  windowID: UInt32? = nil,
  title: String?,
  frame: FrameSnapshot?
) -> ApplicationWindowSnapshot {
  ApplicationWindowSnapshot(index: index, windowID: windowID, title: title, frame: frame)
}

private func target(title: String?) -> CapturableWindowSnapshot {
  CapturableWindowSnapshot(
    windowID: 12448,
    bundleID: "com.apple.mail",
    applicationName: "Mail",
    title: title,
    frame: targetFrame
  )
}
