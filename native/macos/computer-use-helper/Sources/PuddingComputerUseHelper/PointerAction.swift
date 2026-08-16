import AppKit
import ApplicationServices
import Foundation

struct PointerWindowTarget: Equatable {
  let pid: pid_t
  let windowID: UInt32
}

enum PointerWindowResolver {
  static func topmostWindow(at point: CGPoint) -> PointerWindowTarget? {
    var hit: AXUIElement?
    guard AXUIElementCopyElementAtPosition(
      AXUIElementCreateSystemWide(), Float(point.x), Float(point.y), &hit
    ) == .success, let hit else {
      return nil
    }
    var pid: pid_t = 0
    guard AXUIElementGetPid(hit, &pid) == .success, pid > 0 else {
      return nil
    }
    let windowID = windowID(of: hit) ?? normalWindowID(pid: pid, at: point)
    guard let windowID else { return nil }
    return PointerWindowTarget(pid: pid, windowID: windowID)
  }

  private static func normalWindowID(pid: pid_t, at point: CGPoint) -> UInt32? {
    guard
      let windows = CGWindowListCopyWindowInfo(
        [.optionOnScreenOnly, .excludeDesktopElements], CGWindowID(0)
      ) as? [[String: Any]]
    else {
      return nil
    }
    for window in windows {
      guard
        (window[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value == pid,
        (window[kCGWindowLayer as String] as? NSNumber)?.intValue == 0,
        let number = window[kCGWindowNumber as String] as? NSNumber,
        let boundsDictionary = window[kCGWindowBounds as String] as? NSDictionary,
        let bounds = CGRect(dictionaryRepresentation: boundsDictionary as CFDictionary),
        bounds.contains(point)
      else {
        continue
      }
      return number.uint32Value
    }
    return nil
  }

  private static func windowID(of element: AXUIElement) -> UInt32? {
    if let number = copyAttribute(element, "AXWindowNumber" as CFString) as? NSNumber {
      return number.uint32Value
    }
    guard let rawWindow = copyAttribute(element, kAXWindowAttribute as CFString),
      CFGetTypeID(rawWindow as CFTypeRef) == AXUIElementGetTypeID()
    else {
      return nil
    }
    let window = rawWindow as! AXUIElement
    return (copyAttribute(window, "AXWindowNumber" as CFString) as? NSNumber)?.uint32Value
  }

  private static func copyAttribute(_ element: AXUIElement, _ attribute: CFString) -> Any? {
    var value: CFTypeRef?
    guard AXUIElementCopyAttributeValue(element, attribute, &value) == .success else {
      return nil
    }
    return value
  }
}

enum PointerCoordinatePolicy {
  static func globalPoint(
    frame: FrameSnapshot,
    currentScaleFactor: Double,
    x: Double,
    y: Double,
    captureWidth: Int,
    captureHeight: Int,
    captureScaleFactor: Double
  ) throws -> CGPoint {
    guard frame.width > 0, frame.height > 0,
      currentScaleFactor.isFinite, currentScaleFactor > 0,
      captureScaleFactor.isFinite, captureScaleFactor > 0,
      captureWidth > 0, captureHeight > 0,
      x.isFinite, y.isFinite, x >= 0, y >= 0,
      x < Double(captureWidth), y < Double(captureHeight)
    else {
      throw HelperError.coordinateSourceStale("invalid screenshot coordinates")
    }
    guard abs(currentScaleFactor - captureScaleFactor) <= 0.01,
      abs(frame.width * captureScaleFactor - Double(captureWidth)) <= 1,
      abs(frame.height * captureScaleFactor - Double(captureHeight)) <= 1
    else {
      throw HelperError.coordinateSourceStale("window size or display scale changed")
    }
    return CGPoint(
      x: frame.x + x / captureScaleFactor,
      y: frame.y + y / captureScaleFactor
    )
  }
}

final class PointerService {
  private static let doubleClickGapMicroseconds: UInt32 = 80_000
  private static let dragSteps = 8

  private let isTrusted: () -> Bool
  private let frontmostPID: () -> pid_t?
  private let topmostWindowAtPoint: (CGPoint) -> PointerWindowTarget?
  private let postEvent: (CGEvent) -> Void
  private let wait: (UInt32) -> Void

  init(
    isTrusted: @escaping () -> Bool = AXIsProcessTrusted,
    frontmostPID: @escaping () -> pid_t? = {
      NSWorkspace.shared.frontmostApplication?.processIdentifier
    },
    topmostWindowAtPoint: @escaping (CGPoint) -> PointerWindowTarget? =
      PointerWindowResolver.topmostWindow,
    postEvent: @escaping (CGEvent) -> Void = { event in
      event.post(tap: .cghidEventTap)
    },
    wait: @escaping (UInt32) -> Void = { microseconds in
      usleep(microseconds)
    }
  ) {
    self.isTrusted = isTrusted
    self.frontmostPID = frontmostPID
    self.topmostWindowAtPoint = topmostWindowAtPoint
    self.postEvent = postEvent
    self.wait = wait
  }

  func perform(
    bundleID: String,
    pid: pid_t,
    windowID: UInt32,
    input: PointerInput,
    start: CGPoint,
    end: CGPoint?
  ) throws -> PointerSnapshot {
    guard isTrusted() else {
      throw HelperError.permissionRequired("accessibility")
    }
    guard AppPolicy.allows(bundleID: bundleID, pid: pid) else {
      throw HelperError.appNotAllowed(bundleID)
    }
    try requireCurrentTarget(bundleID: bundleID, pid: pid, windowID: windowID, points: targetPoints(start, end))
    guard let source = CGEventSource(stateID: .privateState) else {
      throw HelperError.actionFailed("could not create a pointer event source")
    }

    postEvent(try mouseEvent(source: source, type: .mouseMoved, point: start, button: .left))
    wait(20_000)
    try requireCurrentTarget(bundleID: bundleID, pid: pid, windowID: windowID, points: targetPoints(start, end))
    let targetIsCurrent = { [self] in
      isCurrentTarget(pid: pid, windowID: windowID, points: targetPoints(start, end))
    }
    switch input.action {
    case .click:
      guard let button = input.button, let count = input.clickCount else {
        throw HelperError.actionFailed("click parameters are missing")
      }
      try click(
        source: source,
        point: start,
        button: button,
        count: count,
        targetIsCurrent: targetIsCurrent
      )
    case .drag:
      guard let end else {
        throw HelperError.actionFailed("drag endpoint is missing")
      }
      try drag(
        source: source,
        start: start,
        end: end,
        targetIsCurrent: targetIsCurrent
      )
    case .scroll:
      guard let deltaX = input.deltaX, let deltaY = input.deltaY else {
        throw HelperError.actionFailed("scroll parameters are missing")
      }
      try scroll(source: source, deltaX: deltaX, deltaY: deltaY)
    }

    return PointerSnapshot(
      bundleID: bundleID,
      elementID: "",
      action: input.action.rawValue,
      completed: true,
      x: input.x,
      y: input.y,
      toX: input.toX,
      toY: input.toY,
      button: input.button?.rawValue,
      clickCount: input.clickCount,
      deltaX: input.deltaX,
      deltaY: input.deltaY
    )
  }

  private func click(
    source: CGEventSource,
    point: CGPoint,
    button: PointerButton,
    count: Int,
    targetIsCurrent: () -> Bool
  ) throws {
    let mouseButton: CGMouseButton = button == .left ? .left : .right
    let downType: CGEventType = button == .left ? .leftMouseDown : .rightMouseDown
    let upType: CGEventType = button == .left ? .leftMouseUp : .rightMouseUp
    let events = try (1...count).map { index in
      (
        try mouseEvent(
          source: source, type: downType, point: point, button: mouseButton,
          clickState: Int64(index)),
        try mouseEvent(
          source: source, type: upType, point: point, button: mouseButton,
          clickState: Int64(index))
      )
    }
    for (index, pair) in events.enumerated() {
      guard targetIsCurrent() else {
        throw index == 0
          ? HelperError.coordinateSourceStale("target window is no longer topmost")
          : HelperError.actionFailed("pointer target changed during a double-click")
      }
      postEvent(pair.0)
      wait(40_000)
      let targetStayedCurrent = targetIsCurrent()
      postEvent(pair.1)
      guard targetStayedCurrent else {
        throw HelperError.actionFailed("pointer target changed while a click was in progress")
      }
      if index + 1 < events.count {
        wait(Self.doubleClickGapMicroseconds)
      }
    }
  }

  private func drag(
    source: CGEventSource,
    start: CGPoint,
    end: CGPoint,
    targetIsCurrent: () -> Bool
  ) throws {
    let down = try mouseEvent(
      source: source, type: .leftMouseDown, point: start, button: .left, clickState: 1)
    let dragged = try (1...Self.dragSteps).map { step in
      let progress = CGFloat(step) / CGFloat(Self.dragSteps)
      let point = CGPoint(
        x: start.x + (end.x - start.x) * progress,
        y: start.y + (end.y - start.y) * progress
      )
      return try mouseEvent(
        source: source, type: .leftMouseDragged, point: point, button: .left,
        clickState: 1)
    }
    let up = try mouseEvent(
      source: source, type: .leftMouseUp, point: end, button: .left, clickState: 1)

    guard targetIsCurrent() else {
      throw HelperError.coordinateSourceStale("target window is no longer topmost")
    }
    postEvent(down)
    wait(40_000)
    var lastPoint = start
    for event in dragged {
      guard targetIsCurrent() else {
        postEvent(try mouseEvent(
          source: source, type: .leftMouseUp, point: lastPoint, button: .left, clickState: 1))
        throw HelperError.actionFailed("pointer target changed while a drag was in progress")
      }
      postEvent(event)
      lastPoint = event.location
      wait(12_000)
    }
    let targetStayedCurrent = targetIsCurrent()
    postEvent(up)
    guard targetStayedCurrent else {
      throw HelperError.actionFailed("pointer target changed while a drag was in progress")
    }
  }

  private func scroll(source: CGEventSource, deltaX: Int, deltaY: Int) throws {
    guard let event = CGEvent(
      scrollWheelEvent2Source: source,
      units: .pixel,
      wheelCount: 2,
      wheel1: Int32(-deltaY),
      wheel2: Int32(-deltaX),
      wheel3: 0
    ) else {
      throw HelperError.actionFailed("could not create a scroll event")
    }
    event.flags = []
    postEvent(event)
  }

  private func mouseEvent(
    source: CGEventSource,
    type: CGEventType,
    point: CGPoint,
    button: CGMouseButton,
    clickState: Int64? = nil
  ) throws -> CGEvent {
    guard let event = CGEvent(
      mouseEventSource: source,
      mouseType: type,
      mouseCursorPosition: point,
      mouseButton: button
    ) else {
      throw HelperError.actionFailed("could not create a pointer event")
    }
    if let clickState {
      event.setIntegerValueField(.mouseEventClickState, value: clickState)
    }
    event.flags = []
    return event
  }

  private func targetPoints(_ start: CGPoint, _ end: CGPoint?) -> [CGPoint] {
    end == nil ? [start] : [start, end!]
  }

  private func isCurrentTarget(pid: pid_t, windowID: UInt32, points: [CGPoint]) -> Bool {
    guard frontmostPID() == pid else { return false }
    let expected = PointerWindowTarget(pid: pid, windowID: windowID)
    return points.allSatisfy { topmostWindowAtPoint($0) == expected }
  }

  private func requireCurrentTarget(
    bundleID: String,
    pid: pid_t,
    windowID: UInt32,
    points: [CGPoint]
  ) throws {
    let foregroundPID = frontmostPID()
    guard foregroundPID == pid else {
      let name = foregroundPID.flatMap {
        NSRunningApplication(processIdentifier: $0)?.localizedName
      } ?? "unknown"
      throw HelperError.appNotForeground(bundleID, name)
    }
    let expected = PointerWindowTarget(pid: pid, windowID: windowID)
    for point in points {
      let actual = topmostWindowAtPoint(point)
      guard actual == expected else {
        let actualDescription = actual.map {
          let name = NSRunningApplication(processIdentifier: $0.pid)?.localizedName ?? "unknown"
          return "app=\(name), pid=\($0.pid), windowID=\($0.windowID)"
        } ?? "none"
        throw HelperError.coordinateSourceStale(
          "target window pid=\(pid), windowID=\(windowID) is not topmost at "
            + "x=\(point.x), y=\(point.y); found \(actualDescription)"
        )
      }
    }
  }
}
