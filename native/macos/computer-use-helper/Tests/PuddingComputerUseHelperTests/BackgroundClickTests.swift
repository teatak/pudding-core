import AppKit
import Darwin
import Foundation
import Testing
@testable import PuddingComputerUseHelper

private func withJournal(_ body: (BackgroundInputJournal, URL) throws -> Void) throws {
  let directory = FileManager.default.temporaryDirectory.appendingPathComponent("pudding-input-test-\(UUID().uuidString)")
  defer { try? FileManager.default.removeItem(at: directory) }
  let request = backgroundRequest()
  let journal = try BackgroundInputJournal.acquire(pid: request.pid, stamp: request.stamp, directory: directory)
  try body(journal, directory)
}

private func backgroundRequest() -> BackgroundClickRequest {
  BackgroundClickRequest(bundleID: "com.apple.iCal", pid: 42,
    stamp: BackgroundProcessStamp(seconds: 1, microseconds: 2),
    bundlePath: "/System/Applications/Calendar.app",
    executable: "/System/Applications/Calendar.app/Contents/MacOS/Calendar",
    windowID: 7, frame: FrameSnapshot(x: -100, y: 20, width: 800, height: 600),
    x: 0.5, y: 0.25, foregroundPID: 99)
}

@Test func backgroundPreviewRejectsUnprovenApplicationsAndSpoofedPaths() throws {
  try backgroundRequest().validate()
  #expect(!BackgroundClickPolicy.allows(bundleID: "com.electron.lark", bundlePath: "/Applications/Lark.app", executable: "/Applications/Lark.app/Contents/MacOS/Feishu"))
  #expect(!BackgroundClickPolicy.allows(bundleID: "com.apple.iCal", bundlePath: "/tmp/Calendar.app", executable: "/tmp/Calendar.app/Contents/MacOS/Calendar"))
  #expect(!BackgroundClickPolicy.allows(bundleID: "com.apple.iCal", bundlePath: "/System/Applications/Calendar.app", executable: "/tmp/Calendar"))
}

@Test func backgroundDeliveryParsesWithoutObservationOrNewTool() throws {
  let raw = #"{"id":"one","command":"pointer","params":{"bundleID":"com.apple.iCal","windowID":7,"action":"click","x":0.5,"y":0.4,"delivery":"background"}}"#
  let command = try JSONDecoder().decode(ProtocolRequest.self, from: Data(raw.utf8)).helperCommand()
  guard case .pointer(_, _, let input) = command else { Issue.record("wrong command"); return }
  #expect(input.delivery == "background" && input.button == .left && input.clickCount == 1)
  let cli = try ArgumentParser.parse(["pointer", "--bundle-id", "com.apple.iCal", "--window-id", "7", "--action", "click", "--x", "0.5", "--y", "0.4", "--delivery", "background"])
  #expect(cli == command)
  for bad in [raw.replacingOccurrences(of: "\"action\":\"click\"", with: "\"action\":\"drag\""),
              raw.replacingOccurrences(of: "\"delivery\":\"background\"", with: "\"delivery\":\"auto\""),
              raw.replacingOccurrences(of: "\"x\":0.5", with: "\"clickCount\":2,\"x\":0.5"),
              raw.replacingOccurrences(of: "\"x\":0.5", with: "\"button\":\"right\",\"x\":0.5"),
              raw.replacingOccurrences(of: "\"command\":\"pointer\"", with: "\"command\":\"permissions\"")] {
    #expect(throws: (any Error).self) {
      try JSONDecoder().decode(ProtocolRequest.self, from: Data(bad.utf8)).helperCommand()
    }
  }
}

@Test func backgroundTransactionPairsExactlyOneClickAndRestores() throws {
  try withJournal { journal, _ in
    var posted: [BackgroundInputPhase] = []
    let transaction = BackgroundClickTransaction(journal: journal, check: {}, post: { posted.append($0) },
      wait: { _ in }, sameProcess: { true }, windowExists: { true }, targetIsForeground: { false })
    try transaction.run()
    #expect(posted == [.activated, .pressed, .released, .idle])
    #expect((try? journal.phase()) == .idle)
  }
}

@Test func backgroundCancellationAndTargetChangesNeverReplay() throws {
  for failureCheck in 1...4 {
    try withJournal { journal, _ in
      var posted: [BackgroundInputPhase] = [], checks = 0
      let transaction = BackgroundClickTransaction(journal: journal, check: {
        checks += 1
        if checks == failureCheck { throw BackgroundInputError("cancelled / permission / geometry / foreground changed") }
      }, post: { posted.append($0) }, wait: { _ in }, sameProcess: { true },
        windowExists: { true }, targetIsForeground: { false })
      do { try transaction.run(); Issue.record("expected interruption") }
      catch {
        #expect(errorDetail(for: error).outcome == (failureCheck == 1 ? .notStarted : .unknown))
        #expect(!errorDetail(for: error).retryable)
      }
      let expected: [BackgroundInputPhase] = failureCheck == 1 ? [] : failureCheck == 2 ? [.activated, .idle] : [.activated, .pressed, .released, .idle]
      #expect(posted == expected)
      #expect((try? journal.phase()) == .idle)
    }
  }
}

@Test func backgroundCrashRecoveryHonorsProcessWindowAndRealUserActivation() throws {
  for phase in [BackgroundInputPhase.activated, .pressed, .released] {
    for (alive, window, foreground) in [(true, true, false), (true, true, true), (true, false, false), (false, false, false)] {
      try withJournal { journal, _ in
        try journal.set(phase)
        var posted: [BackgroundInputPhase] = []
        let transaction = BackgroundClickTransaction(journal: journal, check: {}, post: { posted.append($0) },
          wait: { _ in }, sameProcess: { alive }, windowExists: { window }, targetIsForeground: { foreground })
        try transaction.recover()
        let expected: [BackgroundInputPhase] = (alive && window && phase == .pressed ? [.released] : []) + (alive && !foreground ? [.idle] : [])
        #expect(posted == expected)
        #expect((try? journal.phase()) == .idle)
        try transaction.recover()
        #expect(posted == expected) // Second cleanup is inert.
      }
    }
  }
}

@Test func backgroundRecoveryFailureRetainsJournalAndReportsUnknown() throws {
  try withJournal { journal, _ in
    var count = 0
    let transaction = BackgroundClickTransaction(journal: journal, check: {}, post: { phase in
      if phase == .idle { count += 1; throw HelperError.permissionRequired("accessibility") }
    }, wait: { _ in }, sameProcess: { true }, windowExists: { true }, targetIsForeground: { false })
    do { try transaction.run(); Issue.record("expected recovery failure") }
    catch { #expect(errorDetail(for: error).outcome == .unknown) }
    #expect(count == 1)
    #expect((try? journal.phase()) == .released)
  }
}

@Test func backgroundOwnershipRejectsConcurrentOrUnresolvedState() throws {
  try withJournal { journal, directory in
    #expect(throws: (any Error).self) { try BackgroundInputJournal.acquire(pid: 42, stamp: backgroundRequest().stamp, directory: directory) }
    try journal.set(.pressed)
  }
  let directory = FileManager.default.temporaryDirectory.appendingPathComponent("pudding-dirty-input-\(UUID().uuidString)")
  defer { try? FileManager.default.removeItem(at: directory) }
  do {
    let journal = try BackgroundInputJournal.acquire(pid: 42, stamp: backgroundRequest().stamp, directory: directory)
    try journal.set(.pressed)
  }
  #expect(throws: (any Error).self) { try BackgroundInputJournal.acquire(pid: 42, stamp: backgroundRequest().stamp, directory: directory) }
}

@Test func backgroundEventConstructionDoesNotUseMoveModifiersOrExtraClicks() throws {
  let events = try BackgroundClickEvents.make(request: backgroundRequest())
  #expect(events.count == 4)
  #expect(events[.pressed]?.type == .leftMouseDown)
  #expect(events[.released]?.type == .leftMouseUp)
  for phase in [BackgroundInputPhase.pressed, .released] {
    let event = try #require(events[phase])
    #expect(event.flags.rawValue == 0)
    #expect(event.getIntegerValueField(.mouseEventClickState) == 1)
    #expect(event.getIntegerValueField(.mouseEventWindowUnderMousePointer) == 7)
    #expect(event.location == CGPoint(x: 300, y: 170))
  }
}
