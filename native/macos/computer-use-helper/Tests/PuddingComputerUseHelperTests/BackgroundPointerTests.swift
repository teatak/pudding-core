import AppKit
import Darwin
import Foundation
import Testing
@testable import PuddingComputerUseHelper

private func input(_ action: PointerAction = .click, button: PointerButton? = nil,
                   count: Int? = nil, toX: Double? = nil, toY: Double? = nil,
                   deltaX: Int? = nil, deltaY: Int? = nil) throws -> PointerInput {
  try PointerInput.validated(action: action, x: 0.5, y: 0.25, toX: toX, toY: toY,
    button: button, clickCount: count, deltaX: deltaX, deltaY: deltaY, delivery: "background")
}
private func inputs() throws -> [PointerInput] {
  try [input(), input(button: .right), input(count: 2), input(.drag, toX: 0.8, toY: 0.7), input(.scroll, deltaX: -40, deltaY: 120)]
}
private func request(_ input: PointerInput, bundleID: String = "com.example.Custom") -> BackgroundPointerRequest {
  BackgroundPointerRequest(bundleID: bundleID, pid: 42,
    stamp: BackgroundProcessStamp(seconds: 1, microseconds: 2),
    executable: "/Applications/Custom.app/Contents/MacOS/Custom",
    windowID: 7, frame: FrameSnapshot(x: -100, y: 20, width: 800, height: 600), input: input, foregroundPID: 99)
}
private func withJournal(_ body: (BackgroundInputJournal, URL) throws -> Void) throws {
  let directory = FileManager.default.temporaryDirectory.appendingPathComponent("pudding-input-test-\(UUID().uuidString)")
  defer { try? FileManager.default.removeItem(at: directory) }
  let target = request(try input())
  let journal = try BackgroundInputJournal.acquire(pid: target.pid, stamp: target.stamp, directory: directory)
  try body(journal, directory)
}

@Test func backgroundPointerHasNoCompatibilityAllowlistButKeepsTargetAndActionValidation() throws {
  for action in try inputs() {
    try request(action).validate()
    try request(action, bundleID: "com.electron.lark").validate()
    #expect(throws: (any Error).self) { try request(action, bundleID: "com.apple.Terminal").validate() }
    #expect(throws: (any Error).self) { try request(action, bundleID: "").validate() }
    #expect(try !request(action).sameProcess()) // An arbitrary claimed identity never passes the live check.
  }
  var forged = try input(count: 2)
  forged.delivery = "foreground"
  #expect(throws: (any Error).self) { try request(forged).validate() }
  let malformed = PointerInput(action: .click, x: 0.5, y: 0.25, toX: nil, toY: nil,
    button: .right, clickCount: 2, deltaX: nil, deltaY: nil, delivery: "background")
  #expect(throws: (any Error).self) { try request(malformed).validate() }
}

@Test func backgroundPointerParsesAllGesturesThroughExistingProtocolAndCLI() throws {
  let cases: [(String, [String])] = [
    (#""action":"click""#, ["--action", "click"]),
    (#""action":"click","button":"right""#, ["--action", "click", "--button", "right"]),
    (#""action":"click","clickCount":2"#, ["--action", "click", "--click-count", "2"]),
    (#""action":"drag","toX":0.8,"toY":0.7"#, ["--action", "drag", "--to-x", "0.8", "--to-y", "0.7"]),
    (#""action":"scroll","deltaX":-40,"deltaY":120"#, ["--action", "scroll", "--delta-x", "-40", "--delta-y", "120"]),
  ]
  for (fields, arguments) in cases {
    for delivery: String? in [nil, "background", "foreground"] {
      let field = delivery.map { "\"delivery\":\"\($0)\"," } ?? ""
      let options = delivery.map { ["--delivery", $0] } ?? []
      let raw = #"{"id":"one","command":"pointer","params":{"bundleID":"com.example.Custom","windowID":7,"x":0.5,"y":0.25,"# + field + fields + "}}"
      let command = try JSONDecoder().decode(ProtocolRequest.self, from: Data(raw.utf8)).helperCommand()
      let cli = try ArgumentParser.parse(["pointer", "--bundle-id", "com.example.Custom", "--window-id", "7", "--x", "0.5", "--y", "0.25"] + options + arguments)
      #expect(cli == command)
      guard case .pointer(_, _, let input) = command else {
        Issue.record("expected pointer command")
        continue
      }
      #expect(input.delivery == (delivery ?? "background"))
    }
  }
  for fields in [#""action":"drag""#, #""action":"scroll","deltaY":0"#,
    #""action":"click","clickCount":3"#, #""action":"click","button":"right","clickCount":2"#] {
    let raw = #"{"id":"bad","command":"pointer","params":{"bundleID":"com.example.Custom","windowID":7,"x":0.5,"y":0.25,"delivery":"background","# + fields + "}}"
    #expect(throws: (any Error).self) { try JSONDecoder().decode(ProtocolRequest.self, from: Data(raw.utf8)).helperCommand() }
  }
}

@Test func backgroundPointerPlansPreserveCountsButtonsCoordinatesAndScrollDirection() throws {
  let actions = try inputs()
  for (index, action) in actions.enumerated() {
    let plan = try BackgroundPointerEvents.make(request: request(action))
    #expect(plan.steps.count == [2, 2, 4, 10, 1][index])
    for step in plan.steps {
      #expect(step.event.type != .mouseMoved)
      #expect(step.event.flags.rawValue == 0)
      if step.event.type != .scrollWheel {
        #expect(step.event.getIntegerValueField(.mouseEventWindowUnderMousePointer) == 7)
      }
      if let release = step.release { #expect(release.location == step.event.location) }
    }
    if action.action != .drag { #expect(plan.steps.allSatisfy { $0.event.location == CGPoint(x: 300, y: 170) }) }
  }
  let right = try BackgroundPointerEvents.make(request: request(input(button: .right)))
  #expect(right.steps.map(\.event.type) == [.rightMouseDown, .rightMouseUp])
  #expect(right.steps.allSatisfy { $0.event.getIntegerValueField(.mouseEventButtonNumber) == 1 })
  let double = try BackgroundPointerEvents.make(request: request(input(count: 2)))
  #expect(double.steps.map { $0.event.getIntegerValueField(.mouseEventClickState) } == [1, 1, 2, 2])
  let drag = try BackgroundPointerEvents.make(request: request(input(.drag, toX: 0.8, toY: 0.7)))
  #expect(drag.steps.dropFirst().dropLast().allSatisfy { $0.event.type == .leftMouseDragged })
  #expect(drag.steps.last?.event.location == CGPoint(x: 540, y: 440))
  let scroll = try BackgroundPointerEvents.make(request: request(input(.scroll, deltaX: -40, deltaY: 120)))
  #expect(scroll.steps[0].event.type == .scrollWheel && scroll.steps[0].release == nil)
  #expect(scroll.steps[0].event.getIntegerValueField(.scrollWheelEventPointDeltaAxis1) == -120)
  #expect(scroll.steps[0].event.getIntegerValueField(.scrollWheelEventPointDeltaAxis2) == 40)
  #expect(scroll.steps[0].event.getIntegerValueField(CGEventField(rawValue: 51)!) == 7)
}

@Test func backgroundPointerTransactionsPairAllGesturesAndRestore() throws {
  for action in try inputs() {
    let events = try BackgroundPointerEvents.make(request: request(action))
    try withJournal { journal, _ in
      var posted: [BackgroundPointerPost] = []
      let transaction = BackgroundPointerTransaction(journal: journal, events: events, check: {}, post: { posted.append($0) },
        wait: { _ in }, sameProcess: { true }, windowExists: { true }, targetIsForeground: { false })
      try transaction.run()
      #expect(posted == [.activate] + events.steps.indices.map { .step($0) } + [.deactivate])
      #expect((try? journal.phase()) == .idle)
    }
  }
}

@Test func backgroundPointerInterruptionAtEveryStepOnlyReleasesCurrentButtonAndNeverReplays() throws {
  for action in try inputs() {
    let events = try BackgroundPointerEvents.make(request: request(action))
    for failureCheck in 1...(events.steps.count + 2) {
      try withJournal { journal, _ in
        var posted: [BackgroundPointerPost] = [], checks = 0
        let transaction = BackgroundPointerTransaction(journal: journal, events: events, check: {
          checks += 1
          if checks == failureCheck { throw BackgroundInputError("cancel / permission / target / foreground changed") }
        }, post: { posted.append($0) }, wait: { _ in }, sameProcess: { true }, windowExists: { true }, targetIsForeground: { false })
        do { try transaction.run(); Issue.record("expected interruption") }
        catch { #expect(errorDetail(for: error).outcome == (failureCheck == 1 ? .notStarted : .unknown)) }
        let delivered = max(0, failureCheck - 2)
        var expected: [BackgroundPointerPost] = failureCheck == 1 ? [] : [.activate] + (0..<delivered).map { .step($0) }
        if delivered > 0 && events.steps[delivered - 1].release != nil { expected.append(.release(delivered - 1)) }
        if failureCheck > 1 { expected.append(.deactivate) }
        #expect(posted == expected)
        #expect((try? journal.phase()) == .idle)
      }
    }
  }
}

@Test func backgroundPointerCrashRecoveryUsesSharedStepAndHonorsProcessWindowAndUserActivation() throws {
  for action in try inputs() {
    let events = try BackgroundPointerEvents.make(request: request(action))
    for (index, step) in events.steps.enumerated() where step.release != nil {
      for (alive, window, foreground) in [(true, true, false), (true, true, true), (true, false, false), (false, false, false)] {
        try withJournal { journal, _ in
          try journal.set(.pressed, step: index)
          var posted: [BackgroundPointerPost] = []
          let transaction = BackgroundPointerTransaction(journal: journal, events: events, check: {}, post: { posted.append($0) },
            wait: { _ in }, sameProcess: { alive }, windowExists: { window }, targetIsForeground: { foreground })
          try transaction.recover()
          let expected: [BackgroundPointerPost] = (alive && window ? [.release(index)] : []) + (alive && !foreground ? [.deactivate] : [])
          #expect(posted == expected)
          #expect((try? journal.phase()) == .idle)
          try transaction.recover(); #expect(posted == expected)
        }
      }
    }
  }
}

@Test func backgroundPointerRecoveryFailureRetainsJournalAndReportsUnknown() throws {
  try withJournal { journal, _ in
    let events = try BackgroundPointerEvents.make(request: request(input()))
    let transaction = BackgroundPointerTransaction(journal: journal, events: events, check: {}, post: { post in
      if post == .deactivate { throw HelperError.permissionRequired("accessibility") }
    }, wait: { _ in }, sameProcess: { true }, windowExists: { true }, targetIsForeground: { false })
    do { try transaction.run(); Issue.record("expected recovery failure") }
    catch { #expect(errorDetail(for: error).outcome == .unknown) }
    #expect((try? journal.phase()) == .released)
    try journal.set(.pressed, step: 63)
    #expect(throws: (any Error).self) { try transaction.recover() }
    #expect((try? journal.checkpoint().step) == 63)
  }
}

@Test func backgroundProcessIdentityUsesKernelStartAndExecutableNotAppMetadata() throws {
  let stamp = try BackgroundProcessStamp.read(getpid())
  let executable = try BackgroundProcessStamp.executablePath(getpid())
  for _ in 0..<100 { #expect(try stamp.matches(pid: getpid(), executable: executable)) }
  #expect(try !stamp.matches(pid: getpid(), executable: "/not/the/target"))
  #expect(try !BackgroundProcessStamp(seconds: 0, microseconds: 0).matches(pid: getpid(), executable: executable))
  let child = Process()
  child.executableURL = URL(fileURLWithPath: "/usr/bin/true")
  try child.run(); child.waitUntilExit()
  #expect(try !stamp.matches(pid: child.processIdentifier, executable: executable))
}

@Test func unavailableProcessIdentityDoesNotClearRecoveryJournalOrPostInput() throws {
  try withJournal { journal, _ in
    try journal.set(.pressed, step: 4)
    let events = try BackgroundPointerEvents.make(request: request(input(.drag, toX: 0.8, toY: 0.7)))
    var posted: [BackgroundPointerPost] = []
    let transaction = BackgroundPointerTransaction(journal: journal, events: events, check: {}, post: { posted.append($0) },
      wait: { _ in }, sameProcess: { throw BackgroundInputError("identity temporarily unavailable") },
      windowExists: { true }, targetIsForeground: { false })
    #expect(throws: (any Error).self) { try transaction.recover() }
    #expect(posted.isEmpty)
    #expect(try journal.checkpoint().phase == .pressed && journal.checkpoint().step == 4)
  }
}

// Real hidden NSWindow, but record events instead of posting OS input. This
// reproduces the lifecycle bug without showing a window or changing focus.
@Test @MainActor func hiddenBackgroundWindowStillRequiresButtonRelease() throws {
  _ = NSApplication.shared
  let window = NSWindow(contentRect: NSRect(x: 20, y: 20, width: 120, height: 100),
    styleMask: [.titled], backing: .buffered, defer: false)
  window.isReleasedWhenClosed = false
  defer { window.close() }
  #expect(!window.isVisible)
  let action = try input(.drag, toX: 0.8, toY: 0.7)
  let target = BackgroundPointerRequest(bundleID: "com.example.HiddenWindow", pid: getpid(),
    stamp: try BackgroundProcessStamp.read(getpid()),
    executable: try BackgroundProcessStamp.executablePath(getpid()), windowID: UInt32(window.windowNumber),
    frame: FrameSnapshot(x: 20, y: 20, width: 120, height: 122), input: action, foregroundPID: getpid())
  try withJournal { journal, _ in
    try journal.set(.pressed, step: 4)
    var posted: [BackgroundPointerPost] = []
    let transaction = BackgroundPointerTransaction(journal: journal,
      events: try BackgroundPointerEvents.make(request: target), check: {}, post: { posted.append($0) },
      wait: { _ in }, sameProcess: { try target.sameProcess() },
      windowExists: { try target.currentFrame(includeOffscreen: true) != nil }, targetIsForeground: { false })
    #expect(try target.currentFrame() == nil)
    #expect(try target.currentFrame(includeOffscreen: true) != nil)
    try transaction.recover()
    #expect(posted == [.release(4), .deactivate])
    #expect(try journal.phase() == .idle)
  }
}

@Test func backgroundWindowLookupDistinguishesInvisibleMissingAndUnavailable() throws {
  let target = request(try input())
  let bounds = CGRect(x: -100, y: 20, width: 800, height: 600)
  for visible: Bool? in [true, false, nil] {
    var info: [String: Any] = [kCGWindowNumber as String: 7, kCGWindowOwnerPID as String: 42,
      kCGWindowLayer as String: 0,
      kCGWindowBounds as String: bounds.dictionaryRepresentation]
    if let visible { info[kCGWindowIsOnscreen as String] = visible }
    let copy: (CGWindowListOption, CGWindowID) -> CFArray? = { options, id in
      #expect(options == [.optionAll, .excludeDesktopElements])
      #expect(id == kCGNullWindowID)
      return [info] as CFArray
    }
    #expect(try target.currentFrame(copyWindowInfo: copy) == (visible == true ? bounds : nil))
    #expect(try target.currentFrame(includeOffscreen: true, copyWindowInfo: copy) == bounds)
  }
  #expect(try target.currentFrame(includeOffscreen: true, copyWindowInfo: { _, _ in [] as CFArray }) == nil)
  #expect(throws: (any Error).self) {
    try target.currentFrame(includeOffscreen: true, copyWindowInfo: { _, _ in nil })
  }
  #expect(throws: (any Error).self) {
    try target.currentFrame(includeOffscreen: true, copyWindowInfo: { _, _ in
      [[kCGWindowNumber as String: 7, kCGWindowOwnerPID as String: 42]] as CFArray
    })
  }
  #expect(try target.currentFrame(includeOffscreen: true, copyWindowInfo: { _, _ in
    [[kCGWindowNumber as String: 7, kCGWindowOwnerPID as String: 99]] as CFArray
  }) == nil)
}

@Test func unavailableWindowOrRevokedPostPermissionRetainsPressedRecovery() throws {
  for failLookup in [true, false] {
    try withJournal { journal, _ in
      try journal.set(.pressed, step: 4)
      var posted: [BackgroundPointerPost] = []
      let target = request(try input(.drag, toX: 0.8, toY: 0.7))
      let transaction = BackgroundPointerTransaction(journal: journal,
        events: try BackgroundPointerEvents.make(request: target), check: {}, post: { event in
          if !failLookup { throw HelperError.permissionRequired("accessibility") }
          posted.append(event)
        }, wait: { _ in }, sameProcess: { true }, windowExists: {
          if failLookup { return try target.currentFrame(includeOffscreen: true, copyWindowInfo: { _, _ in nil }) != nil }
          return true
        }, targetIsForeground: { false })
      #expect(throws: (any Error).self) { try transaction.recover() }
      #expect(posted.isEmpty)
      #expect(try journal.checkpoint().phase == .pressed && journal.checkpoint().step == 4)
    }
  }
}

@Test func backgroundOwnershipRejectsConcurrentAndOldOrNewUnresolvedState() throws {
  let stamp = request(try input()).stamp
  try withJournal { _, directory in
    #expect(throws: (any Error).self) { try BackgroundInputJournal.acquire(pid: 42, stamp: stamp, directory: directory) }
  }
  for step in [0, 2, 8] {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent("pudding-dirty-input-\(UUID().uuidString)")
    defer { try? FileManager.default.removeItem(at: directory) }
    do {
      let journal = try BackgroundInputJournal.acquire(pid: 42, stamp: stamp, directory: directory)
      try journal.set(.pressed, step: step)
    }
    #expect(throws: (any Error).self) { try BackgroundInputJournal.acquire(pid: 42, stamp: stamp, directory: directory) }
  }
}
