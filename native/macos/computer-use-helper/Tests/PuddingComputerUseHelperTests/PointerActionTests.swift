import CoreGraphics
import Testing

@testable import PuddingComputerUseHelper

@Test func pointerCoordinatesMapScreenshotPixelsAndAllowWindowMovement() throws {
  let point = try PointerCoordinatePolicy.globalPoint(
    frame: FrameSnapshot(x: 300, y: 400, width: 100, height: 50),
    currentScaleFactor: 2,
    x: 40,
    y: 20,
    captureWidth: 200,
    captureHeight: 100,
    captureScaleFactor: 2
  )
  #expect(point.x == 320)
  #expect(point.y == 410)
}

@Test func pointerCoordinatesRejectChangedGeometryAndOutOfBoundsPixels() {
  #expect(throws: HelperError.self) {
    try PointerCoordinatePolicy.globalPoint(
      frame: FrameSnapshot(x: 0, y: 0, width: 120, height: 50),
      currentScaleFactor: 2,
      x: 40,
      y: 20,
      captureWidth: 200,
      captureHeight: 100,
      captureScaleFactor: 2
    )
  }
  #expect(throws: HelperError.self) {
    try PointerCoordinatePolicy.globalPoint(
      frame: FrameSnapshot(x: 0, y: 0, width: 100, height: 50),
      currentScaleFactor: 2,
      x: 200,
      y: 20,
      captureWidth: 200,
      captureHeight: 100,
      captureScaleFactor: 2
    )
  }
}

@Test func pointerInputRequiresTheTargetAppToBeForeground() throws {
  let service = PointerService(
    isTrusted: { true },
    frontmostPID: { 99 },
    topmostWindowAtPoint: { _ in PointerWindowTarget(pid: 42, windowID: 7) },
    postEvent: { _ in Issue.record("event must not be posted") },
    wait: { _ in Issue.record("wait must not run") }
  )
  let input = try pointerInput(action: .click, button: .left, clickCount: 1)

  do {
    _ = try service.perform(
      bundleID: "com.example.App", pid: 42, windowID: 7, input: input,
      start: CGPoint(x: 10, y: 20), end: nil)
    Issue.record("expected foreground rejection")
  } catch let error as HelperError {
    #expect(error.code == "computer_app_not_foreground")
    #expect(error.outcome == .notStarted)
  }
}

@Test func pointerInputPostsSingleDoubleAndRightClickSequences() throws {
  let cases: [(PointerButton, Int, [CGEventType], [Int64])] = [
    (.left, 1, [.mouseMoved, .leftMouseDown, .leftMouseUp], [0, 1, 1]),
    (.left, 2, [.mouseMoved, .leftMouseDown, .leftMouseUp, .leftMouseDown, .leftMouseUp], [0, 1, 1, 2, 2]),
    (.right, 1, [.mouseMoved, .rightMouseDown, .rightMouseUp], [0, 1, 1]),
  ]
  for (button, count, expectedTypes, expectedStates) in cases {
    var events: [(CGEventType, Int64)] = []
    let service = pointerService { event in
      events.append((event.type, event.getIntegerValueField(.mouseEventClickState)))
    }
    let input = try pointerInput(action: .click, button: button, clickCount: count)
    let result = try service.perform(
      bundleID: "com.example.App", pid: 42, windowID: 7, input: input,
      start: CGPoint(x: 10, y: 20), end: nil)

    #expect(result.completed)
    #expect(result.button == button.rawValue)
    #expect(result.clickCount == count)
    #expect(events.map(\.0) == expectedTypes)
    #expect(events.map(\.1) == expectedStates)
  }
}

@Test func pointerInputPostsDragSequence() throws {
  var events: [(CGEventType, CGPoint)] = []
  let service = pointerService { events.append(($0.type, $0.location)) }
  let input = try pointerInput(action: .drag, toX: 80, toY: 60)
  let result = try service.perform(
    bundleID: "com.example.App", pid: 42, windowID: 7, input: input,
    start: CGPoint(x: 10, y: 20), end: CGPoint(x: 80, y: 60))

  #expect(result.action == "drag")
  #expect(events.first?.0 == .mouseMoved)
  #expect(events[1].0 == .leftMouseDown)
  #expect(events.filter { $0.0 == .leftMouseDragged }.count == 8)
  #expect(events.last?.0 == .leftMouseUp)
  #expect(events.last?.1 == CGPoint(x: 80, y: 60))
}

@Test func pointerInputPostsScrollSequence() throws {
  var eventTypes: [CGEventType] = []
  let service = pointerService { eventTypes.append($0.type) }
  let input = try pointerInput(action: .scroll, deltaX: 0, deltaY: 240)
  let result = try service.perform(
    bundleID: "com.example.App", pid: 42, windowID: 7, input: input,
    start: CGPoint(x: 10, y: 20), end: nil)

  #expect(result.action == "scroll")
  #expect(result.deltaY == 240)
  #expect(eventTypes == [.mouseMoved, .scrollWheel])
}

@Test func pointerInputAllowsTheUIToChangeAfterMouseUp() throws {
  var released = false
  let service = PointerService(
    isTrusted: { true },
    frontmostPID: { released ? 99 : 42 },
    topmostWindowAtPoint: { _ in
      released ? PointerWindowTarget(pid: 99, windowID: 8) : PointerWindowTarget(pid: 42, windowID: 7)
    },
    postEvent: { event in
      if event.type == .leftMouseUp { released = true }
    },
    wait: { _ in }
  )
  let input = try pointerInput(action: .click, button: .left, clickCount: 1)

  let result = try service.perform(
    bundleID: "com.example.App", pid: 42, windowID: 7, input: input,
    start: CGPoint(x: 10, y: 20), end: nil)

  #expect(result.completed)
}

@Test func pointerInputRejectsAnOccludedTargetBeforeMovingThePointer() throws {
  let service = PointerService(
    isTrusted: { true },
    frontmostPID: { 42 },
    topmostWindowAtPoint: { _ in PointerWindowTarget(pid: 42, windowID: 8) },
    postEvent: { _ in Issue.record("event must not be posted") },
    wait: { _ in Issue.record("wait must not run") }
  )
  let input = try pointerInput(action: .click, button: .left, clickCount: 1)

  do {
    _ = try service.perform(
      bundleID: "com.example.App", pid: 42, windowID: 7, input: input,
      start: CGPoint(x: 10, y: 20), end: nil)
    Issue.record("expected target-window rejection")
  } catch let error as HelperError {
    #expect(error.code == "computer_observation_stale")
    #expect(error.outcome == .notStarted)
  }
}

@Test func pointerInputRechecksTheForegroundAppBeforeMouseDown() throws {
  var waits = 0
  var eventTypes: [CGEventType] = []
  let service = PointerService(
    isTrusted: { true },
    frontmostPID: { waits == 0 ? 42 : 99 },
    topmostWindowAtPoint: { _ in PointerWindowTarget(pid: 42, windowID: 7) },
    postEvent: { eventTypes.append($0.type) },
    wait: { _ in waits += 1 }
  )
  let input = try pointerInput(action: .click, button: .left, clickCount: 1)

  do {
    _ = try service.perform(
      bundleID: "com.example.App", pid: 42, windowID: 7, input: input,
      start: CGPoint(x: 10, y: 20), end: nil)
    Issue.record("expected foreground rejection")
  } catch let error as HelperError {
    #expect(error.code == "computer_app_not_foreground")
    #expect(error.outcome == .notStarted)
    #expect(eventTypes == [.mouseMoved])
  }
}

@Test func pointerInputReleasesAnInterruptedDrag() throws {
  var waits = 0
  var eventTypes: [CGEventType] = []
  let service = PointerService(
    isTrusted: { true },
    frontmostPID: { waits < 2 ? 42 : 99 },
    topmostWindowAtPoint: { _ in PointerWindowTarget(pid: 42, windowID: 7) },
    postEvent: { eventTypes.append($0.type) },
    wait: { _ in waits += 1 }
  )
  let input = try pointerInput(action: .drag, toX: 80, toY: 60)

  do {
    _ = try service.perform(
      bundleID: "com.example.App", pid: 42, windowID: 7, input: input,
      start: CGPoint(x: 10, y: 20), end: CGPoint(x: 80, y: 60))
    Issue.record("expected interrupted drag rejection")
  } catch let error as HelperError {
    #expect(error.code == "computer_action_failed")
    #expect(error.outcome == .unknown)
    #expect(eventTypes == [.mouseMoved, .leftMouseDown, .leftMouseUp])
  }
}

private func pointerService(postEvent: @escaping (CGEvent) -> Void) -> PointerService {
  PointerService(
    isTrusted: { true },
    frontmostPID: { 42 },
    topmostWindowAtPoint: { _ in PointerWindowTarget(pid: 42, windowID: 7) },
    postEvent: postEvent,
    wait: { _ in }
  )
}

private func pointerInput(
  action: PointerAction,
  toX: Double? = nil,
  toY: Double? = nil,
  button: PointerButton? = nil,
  clickCount: Int? = nil,
  deltaX: Int? = nil,
  deltaY: Int? = nil
) throws -> PointerInput {
  try PointerInput.validated(
    action: action, x: 10, y: 20, toX: toX, toY: toY,
    button: button, clickCount: clickCount, deltaX: deltaX, deltaY: deltaY)
}
