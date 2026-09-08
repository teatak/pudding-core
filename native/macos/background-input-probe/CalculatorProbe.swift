// Isolated Calculator comparison. No injection, event filtering or TCC requests.
import AppKit
import ApplicationServices

func isCalculator(_ app: NSRunningApplication, executable: String) -> Bool {
  app.bundleIdentifier == "com.apple.calculator"
    && app.bundleURL?.path == "/System/Applications/Calculator.app"
    && executable == "/System/Applications/Calculator.app/Contents/MacOS/Calculator"
    && app.executableURL?.path == executable
}

private func calculatorAttribute(_ element: AXUIElement, _ name: String) -> CFTypeRef? {
  var value: CFTypeRef?
  guard AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success else { return nil }
  return value
}

private func calculatorFrame(_ element: AXUIElement) -> CGRect? {
  guard let position = calculatorAttribute(element, kAXPositionAttribute), CFGetTypeID(position) == AXValueGetTypeID(),
    let size = calculatorAttribute(element, kAXSizeAttribute), CFGetTypeID(size) == AXValueGetTypeID() else { return nil }
  var origin = CGPoint.zero, extent = CGSize.zero
  guard AXValueGetValue(position as! AXValue, .cgPoint, &origin), AXValueGetValue(size as! AXValue, .cgSize, &extent)
  else { return nil }
  return CGRect(origin: origin, size: extent)
}

func calculatorState() throws -> [String: Any] {
  try require(AXIsProcessTrusted() && CGPreflightScreenCaptureAccess(), "existing AX/window-list permissions required; no request issued")
  let apps = NSWorkspace.shared.runningApplications.filter { $0.bundleIdentifier == "com.apple.calculator" }
  guard apps.count == 1, let app = apps.first, let executable = app.executableURL?.path,
    isCalculator(app, executable: executable) else { throw ProbeError("running system Calculator missing or ambiguous") }
  let windows = calculatorAttribute(AXUIElementCreateApplication(app.processIdentifier), kAXWindowsAttribute) as? [AXUIElement] ?? []
  let main = windows.filter { calculatorAttribute($0, kAXIdentifierAttribute) as? String == "main" }
  guard main.count == 1, let frame = calculatorFrame(main[0]) else { throw ProbeError("Calculator main AX window missing or ambiguous") }
  let candidates = CGWindowListCopyWindowInfo(.optionOnScreenOnly, 0) as? [[String: Any]] ?? []
  let info = try mainWindowInfo(candidates, pid: app.processIdentifier, frame: frame)
  var queue = [main[0]], index = 0, text: [String] = [], buttons: [[String: Any]] = []
  while index < queue.count {
    try require(index < 256, "Calculator AX tree exceeds probe limit")
    let element = queue[index]; index += 1
    let role = calculatorAttribute(element, kAXRoleAttribute) as? String
    if role == kAXStaticTextRole, let value = calculatorAttribute(element, kAXValueAttribute) as? String { text.append(value) }
    if role == kAXButtonRole, let id = calculatorAttribute(element, kAXIdentifierAttribute) as? String,
      let rect = calculatorFrame(element) {
      buttons.append(["id": id, "x": rect.midX - frame.minX, "y": rect.midY - frame.minY,
        "width": rect.width, "height": rect.height])
    }
    queue.append(contentsOf: calculatorAttribute(element, kAXChildrenAttribute) as? [AXUIElement] ?? [])
  }
  var displays = [CGDirectDisplayID](repeating: 0, count: 32), displayCount: UInt32 = 0
  try require(CGGetActiveDisplayList(32, &displays, &displayCount) == .success, "display list unavailable")
  let containing = displays.prefix(Int(displayCount)).map { CGDisplayBounds($0) }.filter {
    $0.contains(CGPoint(x: frame.midX, y: frame.midY))
  }
  guard containing.count == 1, let display = containing.first else { throw ProbeError("Calculator display missing or ambiguous") }
  return ["pid": app.processIdentifier, "executable": executable, "windowID": info[kCGWindowNumber as String]!,
    "frame": ["x": frame.minX, "y": frame.minY, "width": frame.width, "height": frame.height],
    "active": app.isActive, "points": [String: String](), "text": text, "buttons": buttons,
    "display": ["x": display.minX, "y": display.minY, "width": display.width, "height": display.height]]
}

private let calculatorClickMask: CGEventMask = (1 << CGEventType.leftMouseDown.rawValue) | (1 << CGEventType.leftMouseUp.rawValue)

func listenToCalculator() throws {
  try require(CGPreflightListenEventAccess(), "event-listen permission missing; no request issued")
  let state = try calculatorState()
  let pid = state["pid"] as! pid_t
  // A process tap observes routing, not the app's eventual NSEvent/SwiftUI callback.
  guard let tap = CGEvent.tapCreateForPid(pid: pid, place: .headInsertEventTap, options: .listenOnly,
    eventsOfInterest: calculatorClickMask, callback: { _, type, event, _ in
      if type == .tapDisabledByTimeout || type == .tapDisabledByUserInput {
        emit(["kind": "tap-disabled", "type": type.rawValue])
      } else {
        let ns = NSEvent(cgEvent: event)
        emit(["kind": "routed-click", "type": type.rawValue, "flags": event.flags.rawValue,
          "clickCount": event.getIntegerValueField(.mouseEventClickState),
          "eventNumber": event.getIntegerValueField(.mouseEventNumber),
          "screenX": event.location.x, "screenY": event.location.y,
          "decodedX": ns?.locationInWindow.x ?? 0, "decodedY": ns?.locationInWindow.y ?? 0,
          "windowID": ns?.windowNumber ?? 0,
          "windowUnderPointer": event.getIntegerValueField(.mouseEventWindowUnderMousePointer),
          "targetPID": event.getIntegerValueField(.eventTargetUnixProcessID),
          "sourcePID": event.getIntegerValueField(.eventSourceUnixProcessID)])
      }
      return Unmanaged.passUnretained(event) // Listen-only; never suppress or modify.
    }, userInfo: nil), let source = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0)
  else { throw ProbeError("Calculator listen-only process tap unavailable; no fallback") }
  CFRunLoopAddSource(CFRunLoopGetMain(), source, .commonModes)
  defer { CFMachPortInvalidate(tap); CFRunLoopRemoveSource(CFRunLoopGetMain(), source, .commonModes) }
  FileHandle.standardInput.readabilityHandler = { handle in
    if handle.availableData.isEmpty { CFRunLoopStop(CFRunLoopGetMain()) }
  }
  let deadline = Timer.scheduledTimer(withTimeInterval: 60, repeats: false) { _ in
    emit(["kind": "listener-deadline"])
    CFRunLoopStop(CFRunLoopGetMain())
  }
  defer { deadline.invalidate(); FileHandle.standardInput.readabilityHandler = nil }
  emit(["kind": "ready", "pid": getpid(), "targetPID": pid, "listenOnly": true, "eventTypes": [1, 2]])
  CFRunLoopRun()
  emit(["kind": "listener-stopped"])
}
