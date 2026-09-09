// Test-only process-directed input experiment. Not linked into Pudding or its Helper.
import AppKit
import ApplicationServices
import Darwin
import ScreenCaptureKit

func emit(_ value: [String: Any]) {
  var record = value
  record["monotonicTime"] = ProcessInfo.processInfo.systemUptime
  let data = try! JSONSerialization.data(withJSONObject: record, options: [.sortedKeys])
  // A lost diagnostics reader must not interrupt input/focus restoration.
  try? FileHandle.standardOutput.write(contentsOf: data + Data([10]))
}

struct ProbeError: Error, CustomStringConvertible {
  let description: String
  init(_ description: String) { self.description = description }
}

func require(_ condition: Bool, _ message: String) throws {
  if !condition { throw ProbeError(message) }
}

func eventDetails(_ event: NSEvent) -> [String: Any] {
  var details: [String: Any] = ["kind": "event", "type": event.type.rawValue,
   "flags": event.modifierFlags.intersection(.deviceIndependentFlagsMask).rawValue,
   "command": event.modifierFlags.contains(.command),
   "x": event.locationInWindow.x, "y": event.locationInWindow.y,
   "windowID": event.windowNumber, "active": NSApp.isActive]
  if [.leftMouseDown, .leftMouseUp, .rightMouseDown, .rightMouseUp].contains(event.type) {
    details["clickCount"] = event.clickCount
    details["eventNumber"] = event.eventNumber
  }
  if event.type == .keyDown { details["characters"] = event.characters ?? "" }
  return details
}

final class ProbeButton: NSButton {
  var firstMousePolicy: Bool?
  override func acceptsFirstMouse(for event: NSEvent?) -> Bool {
    let accepted = firstMousePolicy ?? super.acceptsFirstMouse(for: event)
    emit(["kind": "first-mouse", "accepted": accepted, "clickCount": event?.clickCount ?? 0,
          "eventNumber": event?.eventNumber ?? 0])
    return accepted
  }
  override func mouseDown(with event: NSEvent) {
    var details = eventDetails(event)
    details["kind"] = "button-received"
    emit(details)
    super.mouseDown(with: event)
  }
}

final class ProbeApplication: NSApplication {
  override func sendEvent(_ event: NSEvent) {
    if event.type == .appKitDefined {
      emit(["kind": "focus-event", "phase": "before", "subtype": event.subtype.rawValue,
        "active": isActive, "keyWindow": keyWindow?.windowNumber ?? 0,
        "foregroundPID": NSWorkspace.shared.frontmostApplication?.processIdentifier ?? 0])
    }
    switch event.type {
    case .leftMouseDown, .leftMouseUp, .rightMouseDown, .rightMouseUp,
      .leftMouseDragged, .scrollWheel, .keyDown:
      emit(eventDetails(event))
    default: break
    }
    super.sendEvent(event)
    if event.type == .appKitDefined {
      emit(["kind": "focus-event", "phase": "after", "subtype": event.subtype.rawValue,
        "active": isActive, "keyWindow": keyWindow?.windowNumber ?? 0,
        "foregroundPID": NSWorkspace.shared.frontmostApplication?.processIdentifier ?? 0])
    }
  }
}

final class GestureView: NSView {
  private var start: NSPoint?
  var afterDrag: (() -> Void)?
  var holdingPointer: Bool { start != nil }
  override var isFlipped: Bool { true }
  override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
  override func draw(_ dirtyRect: NSRect) {
    NSColor.systemBlue.setFill()
    bounds.fill()
  }
  override func mouseDown(with event: NSEvent) {
    start = event.locationInWindow
    emit(["kind": "effect", "effect": event.clickCount == 2 ? "double" : "canvas-click",
          "command": event.modifierFlags.contains(.command)])
  }
  override func rightMouseDown(with event: NSEvent) {
    emit(["kind": "effect", "effect": "right", "command": event.modifierFlags.contains(.command)])
  }
  override func mouseDragged(with event: NSEvent) { afterDrag?() }
  override func mouseUp(with event: NSEvent) {
    if let start, event.locationInWindow.x - start.x > 30 {
      emit(["kind": "effect", "effect": "drag", "command": event.modifierFlags.contains(.command)])
    }
    start = nil
  }
}

final class ProbeScrollView: NSScrollView {
  override func scrollWheel(with event: NSEvent) {
    let before = contentView.bounds.origin
    super.scrollWheel(with: event)
    // NSScrollView may apply its bounds change on a later animation tick.
    DispatchQueue.main.asyncAfter(deadline: .now() + 0.12) { [weak self] in
      guard let self else { return }
      emit(["kind": "effect", "effect": "scroll", "before": before.y,
            "after": self.contentView.bounds.origin.y, "command": event.modifierFlags.contains(.command)])
    }
  }
}

final class FlippedView: NSView { override var isFlipped: Bool { true } }

final class Fixture: NSObject, NSApplicationDelegate {
  let role: String
  var window: NSWindow!
  var input = Data()
  var monitor: Timer?
  var samples = 0
  var foregroundPIDs = Set<pid_t>()
  var cursorOrigin = NSEvent.mouseLocation
  var maxCursorDistance = 0.0
  var activations: [pid_t] = []
  var monitoredTargetPID: pid_t?
  var windowOrders = Set<String>()
  var count = 0
  var testInput: NSTextField?
  var humanStatus: NSTextField?
  var controls: [String: NSView] = [:]
  var pendingWindowMutation: String?
  var lifecycleDragSteps = 0
  init(role: String) { self.role = role }

  func applicationDidFinishLaunching(_ notification: Notification) {
    window = NSWindow(contentRect: NSRect(x: 80, y: 140, width: 400, height: 400),
      styleMask: [.titled, .closable, .miniaturizable], backing: .buffered, defer: false)
    window.isReleasedWhenClosed = false
    window.title = "Pudding Background Probe — \(role)"
    let content = window.contentView!
    let button = ProbeButton(title: "Increment (test data only)", target: self, action: #selector(increment))
    if CommandLine.arguments.contains("first-mouse-on") { button.firstMousePolicy = true }
    if CommandLine.arguments.contains("first-mouse-off") { button.firstMousePolicy = false }
    button.frame = NSRect(x: 30, y: 320, width: 330, height: 40)
    content.addSubview(button)
    let canvas = GestureView(frame: NSRect(x: 30, y: 210, width: 330, height: 80))
    canvas.afterDrag = { [weak self] in self?.applyWindowMutationAfterDrag() }
    content.addSubview(canvas)
    let scroll = ProbeScrollView(frame: NSRect(x: 30, y: 20, width: 330, height: 160))
    let document = FlippedView(frame: NSRect(x: 0, y: 0, width: 310, height: 3000))
    for i in 0..<80 {
      let label = NSTextField(labelWithString: "Test row \(i)")
      label.frame = NSRect(x: 10, y: i * 35, width: 250, height: 25)
      document.addSubview(label)
    }
    scroll.documentView = document
    scroll.hasVerticalScroller = true
    content.addSubview(scroll)
    controls = ["click": button, "double": canvas, "right": canvas, "drag": canvas, "scroll": scroll]
    if role == "guard" {
      let field = NSTextField(frame: NSRect(x: 30, y: 184, width: 330, height: 22))
      field.placeholderString = "Isolated keyboard-routing test"
      content.addSubview(field)
      testInput = field
    }
    window.makeKeyAndOrderFront(nil)
    NSApp.activate(ignoringOtherApps: true) // Fixture setup only; never in the input sender.
    NSWorkspace.shared.notificationCenter.addObserver(forName: NSWorkspace.didActivateApplicationNotification,
      object: nil, queue: .main) { [weak self] note in
        if let app = note.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication {
          self?.activations.append(app.processIdentifier)
          if self?.monitor != nil { emit(["kind": "workspace-activation", "targetPID": app.processIdentifier]) }
        }
      }
    FileHandle.standardInput.readabilityHandler = { [weak self] handle in
      let data = handle.availableData
      DispatchQueue.main.async {
        guard let self else { return }
        if data.isEmpty { NSApp.terminate(nil); return }
        self.input.append(data)
        while let end = self.input.firstIndex(of: 10) {
          let line = self.input.prefix(upTo: end)
          self.input.removeSubrange(...end)
          do {
            let command = try JSONSerialization.jsonObject(with: line) as! [String: Any]
            self.command(command)
          } catch { emit(["kind": "error", "error": "invalid fixture command"]) }
        }
      }
    }
    emit(["kind": "ready", "pid": getpid()])
  }

  @objc func increment() {
    count += 1
    emit(["kind": "effect", "effect": "click", "count": count,
          "command": NSApp.currentEvent?.modifierFlags.contains(.command) ?? false,
          "clickCount": NSApp.currentEvent?.clickCount ?? 0])
  }

  @objc func startHumanTest(_ sender: NSButton) {
    guard role == "guard", let testInput else { return }
    sender.isEnabled = false
    testInput.stringValue = ""
    window.makeFirstResponder(testInput)
    emit(["kind": "human-start"])
  }

  func applyWindowMutationAfterDrag() {
    guard let mutation = pendingWindowMutation, let window else { return }
    lifecycleDragSteps += 1
    guard lifecycleDragSteps == 4 else { return }
    pendingWindowMutation = nil // Exactly once, after the receiver saw a held drag.
    let windowID = window.windowNumber
    switch mutation {
    case "hide": window.orderOut(nil)
    case "minimize": window.miniaturize(nil)
    case "move": window.setFrameOrigin(NSPoint(x: window.frame.minX + 20, y: window.frame.minY))
    case "close":
      window.close()
      controls.removeAll()
      self.window = nil
    default: return
    }
    emit(["kind": "window-mutation", "mutation": mutation, "windowID": windowID,
      "afterDragStep": lifecycleDragSteps, "visible": window.isVisible, "minimized": window.isMiniaturized])
  }

  func command(_ command: [String: Any]) {
    switch command["op"] as? String {
    case "arm-window-mutation":
      guard role == "target", let mutation = command["mutation"] as? String,
        ["hide", "minimize", "move", "close"].contains(mutation), window != nil else {
        emit(["kind": "error", "error": "only an owned fixture window may be changed"]); return
      }
      pendingWindowMutation = mutation
      lifecycleDragSteps = 0
    case "position":
      let covered = command["covered"] as? Bool == true
      window.setFrameOrigin(NSPoint(x: covered || role == "target" ? 80 : 540, y: 140))
      window.makeKeyAndOrderFront(nil)
      NSApp.activate(ignoringOtherApps: true)
    case "place":
      guard role == "guard", let raw = command["frame"],
        let data = try? JSONSerialization.data(withJSONObject: raw),
        let frame = try? JSONDecoder().decode(CGRectRecord.self, from: data),
        frame.width > 0, frame.height > 0
      else { emit(["kind": "error", "error": "invalid guard placement"]); return }
      let top = CGDisplayBounds(CGMainDisplayID()).height
      window.setFrame(NSRect(x: frame.x, y: top - frame.y - frame.height,
        width: frame.width, height: frame.height), display: true)
      window.makeKeyAndOrderFront(nil)
      NSApp.activate(ignoringOtherApps: true)
    case "move-window":
      window.setFrameOrigin(NSPoint(x: window.frame.minX + 20, y: window.frame.minY))
    case "input-focus":
      if let testInput { window.makeFirstResponder(testInput) }
    case "human-prepare":
      guard role == "guard", let testInput else { return }
      for view in controls.values { view.isHidden = true }
      window.title = "Pudding 并行测试（独立窗口）"
      let label = NSTextField(wrappingLabelWithString: "点击开始后，在下方输入一次：\npudding 1234567890\n然后在本窗口内移动鼠标，等待 30 秒。\n不要切换应用，不要输入隐私内容。")
      label.frame = NSRect(x: 25, y: 225, width: 350, height: 130)
      window.contentView?.addSubview(label)
      humanStatus = label
      let button = NSButton(title: "开始 30 秒测试", target: self, action: #selector(startHumanTest(_:)))
      button.frame = NSRect(x: 60, y: 110, width: 280, height: 44)
      window.contentView?.addSubview(button)
      testInput.placeholderString = "pudding 1234567890"
    case "human-status":
      if role == "guard", let text = command["text"] as? String { humanStatus?.stringValue = text }
    case "activate-test-target":
      guard role == "guard", let pid = command["targetPID"] as? Int32,
        let executable = command["targetExecutable"] as? String,
        let app = NSRunningApplication(processIdentifier: pid),
        app.bundleIdentifier == "com.teatak.pudding.background-probe.target",
        app.executableURL?.path == executable
      else { emit(["kind": "error", "error": "only an owned AppKit target may be activated"]); return }
      // External activation uses the actual system state, unlike the target's NSApp.isActive.
      _ = app.activate(options: .activateIgnoringOtherApps)
    case "type-probe":
      guard role == "guard", NSWorkspace.shared.frontmostApplication?.processIdentifier == getpid(),
        window.isKeyWindow, let source = CGEventSource(stateID: .privateState)
      else { emit(["kind": "error", "error": "owned guard must be foreground for keyboard test"]); return }
      // Fixed test text through the system input route, never a user's app or text.
      for var character: UniChar in [97, 98, 99] {
        for isDown in [true, false] {
          guard let event = CGEvent(keyboardEventSource: source, virtualKey: 0, keyDown: isDown) else { return }
          event.keyboardSetUnicodeString(stringLength: 1, unicodeString: &character)
          event.flags = []
          event.post(tap: .cghidEventTap)
        }
      }
    case "monitor":
      monitor?.invalidate()
      samples = 0; foregroundPIDs = []; activations = []
      monitoredTargetPID = command["targetPID"] as? Int32
      windowOrders = []
      cursorOrigin = NSEvent.mouseLocation; maxCursorDistance = 0
      monitor = Timer.scheduledTimer(withTimeInterval: 0.005, repeats: true) { [weak self] _ in
        guard let self else { return }
        self.samples += 1
        self.foregroundPIDs.insert(NSWorkspace.shared.frontmostApplication?.processIdentifier ?? 0)
        if let targetPID = self.monitoredTargetPID {
          let windows = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], 0)
            as? [[String: Any]] ?? []
          let order = windows.filter {
            let pid = ($0[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value
            return (pid == targetPID || pid == getpid()) &&
              ($0[kCGWindowLayer as String] as? NSNumber)?.intValue == 0
          }.compactMap { ($0[kCGWindowOwnerPID as String] as? NSNumber)?.stringValue }
          self.windowOrders.insert(order.joined(separator: ","))
        }
        let point = NSEvent.mouseLocation
        self.maxCursorDistance = max(self.maxCursorDistance,
          hypot(point.x - self.cursorOrigin.x, point.y - self.cursorOrigin.y))
      }
    case "stop-monitor": monitor?.invalidate(); monitor = nil
    case "quit": NSApp.terminate(nil); return
    default: break
    }
    guard let window else {
      emit(["kind": "reply", "id": command["id"] ?? 0, "pid": getpid(), "windowID": 0,
        "visible": false, "minimized": false, "holdingPointer": false,
        "active": NSApp.isActive, "foregroundPID": NSWorkspace.shared.frontmostApplication?.processIdentifier ?? 0])
      return
    }
    let top = CGDisplayBounds(CGMainDisplayID()).height
    let frame = window.frame
    var points: [String: [String: Double]] = [:]
    for (name, view) in controls {
      let local = view.convert(NSPoint(x: view.bounds.midX, y: view.bounds.midY), to: nil)
      let screen = window.convertPoint(toScreen: local)
      points[name] = ["x": screen.x, "y": top - screen.y]
    }
    emit(["kind": "reply", "id": command["id"] ?? 0, "pid": getpid(),
      "windowID": window.windowNumber, "executable": Bundle.main.executableURL!.path,
      "frame": ["x": frame.minX, "y": top - frame.maxY, "width": frame.width, "height": frame.height],
      "points": points, "active": NSApp.isActive, "keyWindow": window.isKeyWindow, "count": count,
      "visible": window.isVisible, "minimized": window.isMiniaturized,
      "holdingPointer": (controls["drag"] as? GestureView)?.holdingPointer ?? false,
      "inputText": testInput?.stringValue ?? "",
      "foregroundPID": NSWorkspace.shared.frontmostApplication?.processIdentifier ?? 0,
      "samples": samples, "foregroundPIDs": foregroundPIDs.sorted(), "activations": activations,
      "windowOrders": windowOrders.sorted(),
      "maxCursorDistance": maxCursorDistance])
  }
}

enum Variant: String, CaseIterable {
  case quartz, appkit, localFactory = "appkit-local-factory"
  case windowLocal = "appkit-window-local", command = "appkit-command"
  var usesPrivateAPI: Bool { self == .windowLocal || self == .command }
}

typealias WindowLocationSetter = @convention(c) (CGEvent, CGPoint) -> Void

struct Target: Decodable {
  let pid: pid_t
  let windowID: UInt32
  let executable: String
  let frame: CGRectRecord
  let points: [String: PointRecord]
}
struct PointRecord: Decodable { let x: Double; let y: Double; var point: CGPoint { CGPoint(x: x, y: y) } }
struct CGRectRecord: Decodable {
  let x: Double; let y: Double; let width: Double; let height: Double
  var rect: CGRect { CGRect(x: x, y: y, width: width, height: height) }
}
struct Request: Decodable {
  let target: Target
  let guardPID: pid_t
  let variant: String
  let action: String
  var end: PointRecord? = nil
  var scrollY: Int32? = nil
  var realApp: RealApp? = nil // Explicit manual smoke; never inferred from an arbitrary PID.
}

// Explicit diagnostic metadata, not part of the normal/mirror action contract.
struct ProbeClick: Decodable {
  let clickCount: Int
  let eventNumber: Int
}

func validateProbeClicks(_ clicks: [ProbeClick]) throws {
  try require((1...2).contains(clicks.count), "probe requires one or two click pairs")
  try require(clicks.allSatisfy { (1...2).contains($0.clickCount) && (1...2).contains($0.eventNumber) },
    "probe click metadata must be 1 or 2")
}

func isMirror(_ app: NSRunningApplication, executable: String) -> Bool {
  app.bundleIdentifier == "com.apple.ScreenContinuity"
    && app.bundleURL?.path.hasPrefix("/System/Applications/") == true
    && app.executableURL?.path == executable
}

func isFixture(_ app: NSRunningApplication, executable: String) -> Bool {
  let resolvedExecutable = URL(fileURLWithPath: executable).resolvingSymlinksInPath().path
  guard app.executableURL?.resolvingSymlinksInPath().path == resolvedExecutable else { return false }
  // The Codex comparison owns a uniquely named copy beside this probe binary,
  // avoiding ambiguity with a user's already-running source Electron process.
  let electronCopy = URL(fileURLWithPath: CommandLine.arguments[0]).deletingLastPathComponent()
    .appendingPathComponent("Electron Probe.app/Contents/MacOS/Electron").resolvingSymlinksInPath().path
  return app.bundleIdentifier == "com.teatak.pudding.background-probe.target"
    || (app.bundleIdentifier == "com.teatak.pudding.background-probe.electron" && resolvedExecutable == electronCopy)
    || (app.bundleIdentifier == "com.github.Electron"
      && executable.hasSuffix("/web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron"))
}

func windowInfo(_ pid: pid_t, _ windowID: UInt32? = nil, external: Bool = false) -> [String: Any]? {
  let options: CGWindowListOption = windowID == nil ? .optionOnScreenOnly : .optionIncludingWindow
  let windows = CGWindowListCopyWindowInfo(options, windowID ?? 0) as? [[String: Any]] ?? []
  return windows.first {
    ($0[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value == pid
      && ($0[kCGWindowLayer as String] as? NSNumber)?.intValue == 0
      && (windowID == nil || ($0[kCGWindowNumber as String] as? NSNumber)?.uint32Value == windowID)
      && (external || ["Pudding Background Probe — target", "Pudding Background Probe — electron"].contains($0[kCGWindowName as String] as? String ?? ""))
  }
}

func buildEvent(variant: Variant, type: CGEventType, target: Target, point: CGPoint,
                count: Int, setter: WindowLocationSetter?, scrollY: Int32 = -80, eventNumber: Int? = nil) throws -> CGEvent {
  let button: CGMouseButton = type == .rightMouseDown || type == .rightMouseUp ? .right : .left
  let flags: CGEventFlags = variant == .command ? .maskCommand : []
  let event: CGEvent?
  if type == .scrollWheel {
    event = CGEvent(scrollWheelEvent2Source: nil, units: .pixel, wheelCount: 2, wheel1: scrollY, wheel2: 0, wheel3: 0)
  } else if variant == .quartz {
    event = CGEvent(mouseEventSource: nil, mouseType: type, mouseCursorPosition: point, mouseButton: button)
  } else {
    // Factory carries the AppKit window number; tag screen and local spaces separately below.
    let factoryPoint = variant == .localFactory
      ? CGPoint(x: point.x - target.frame.x, y: target.frame.height - (point.y - target.frame.y)) : point
    event = NSEvent.mouseEvent(with: NSEvent.EventType(rawValue: UInt(type.rawValue))!, location: factoryPoint,
      modifierFlags: variant == .command ? .command : [], timestamp: ProcessInfo.processInfo.systemUptime,
      windowNumber: Int(target.windowID), context: nil, eventNumber: eventNumber ?? count, clickCount: count, pressure: 1)?.cgEvent
  }
  guard let event else { throw ProbeError("event construction failed") }
  event.flags = flags
  event.location = point
  event.setIntegerValueField(.mouseEventWindowUnderMousePointer, value: Int64(target.windowID))
  event.setIntegerValueField(.mouseEventWindowUnderMousePointerThatCanHandleThisEvent, value: Int64(target.windowID))
  event.setIntegerValueField(.mouseEventSubtype, value: 3)
  event.setIntegerValueField(.mouseEventButtonNumber, value: Int64(button.rawValue))
  event.setIntegerValueField(.mouseEventClickState, value: Int64(count))
  if type == .scrollWheel { event.setIntegerValueField(CGEventField(rawValue: 51)!, value: Int64(target.windowID)) }
  if variant.usesPrivateAPI {
    guard let setter else { throw ProbeError("private window-location symbol unavailable; no fallback") }
    setter(event, CGPoint(x: point.x - target.frame.x, y: point.y - target.frame.y))
  }
  return event
}

func validate(_ request: Request) throws -> Variant {
  guard let variant = Variant(rawValue: request.variant) else { throw ProbeError("unknown experiment variant") }
  try require(["click", "double", "right", "drag", "scroll"].contains(request.action), "unknown probe action")
  try require(request.target.pid > 0 && request.guardPID > 0 && request.target.pid != request.guardPID, "invalid target PID")
  try require(request.target.windowID > 0, "invalid target window ID")
  let frame = request.target.frame.rect
  try require(frame.origin.x.isFinite && frame.origin.y.isFinite && frame.width.isFinite && frame.height.isFinite
    && frame.width > 0 && frame.height > 0, "invalid window frame")
  guard let point = request.target.points[request.action]?.point else { throw ProbeError("missing action point") }
  try require(point.x.isFinite && point.y.isFinite && frame.contains(point), "point outside target window")
  if request.action == "drag" {
    let end = request.end?.point ?? CGPoint(x: point.x + 60, y: point.y)
    try require(end.x.isFinite && end.y.isFinite && frame.contains(end), "drag outside target window")
  } else { try require(request.end == nil, "end is only valid for drag") }
  if let scrollY = request.scrollY {
    try require(request.action == "scroll" && scrollY != 0 && abs(Int64(scrollY)) <= 600, "invalid scroll delta")
  }
  return variant
}

// Causal experiment: notify only the owned receiver, not WindowServer activation.
// Public AppKit event/subtype constants; no modifier trick or extra mouse click.
func focusNotification(activated: Bool) throws -> CGEvent {
  let subtype: NSEvent.EventSubtype = activated ? .applicationActivated : .applicationDeactivated
  guard let event = NSEvent.otherEvent(with: .appKitDefined, location: .zero, modifierFlags: [],
    timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: 0, context: nil,
    subtype: subtype.rawValue, data1: 0, data2: 0)?.cgEvent
  else { throw ProbeError("could not construct synthetic activation notification") }
  return event
}

func send(_ request: Request, mirror: Bool = false, calculator: Bool = false, probeClicks: [ProbeClick]? = nil,
          syntheticActivation: Bool = false, focusLease: FocusLease? = nil, notifyOnly: Bool = false,
          requireOwner: () throws -> Void = {}) throws {
  let variant = try validate(request)
  try require(!mirror || !calculator, "ambiguous probe target")
  try require(request.realApp == nil || (syntheticActivation && !mirror && !calculator && probeClicks == nil),
    "real-app smoke requires the shared ordinary-click route")
  if syntheticActivation {
    try require(!mirror && !calculator && probeClicks == nil && variant == .windowLocal && request.action == "click",
      "synthetic activation probe only permits one ordinary click")
  }
  try require(syntheticActivation == (focusLease != nil), "synthetic input requires shared ownership")
  try require(!notifyOnly || (syntheticActivation && request.realApp != nil), "notification isolation is real-app smoke only")
  if let probeClicks {
    try require(!mirror && !calculator && variant == .windowLocal && request.action == "click", "click factor probe is fixture-only")
    try validateProbeClicks(probeClicks)
  }
  if mirror { try require(variant == .windowLocal, "mirror probe only permits unmodified window-local input") }
  if calculator {
    try require(variant == .windowLocal && request.action == "click", "calculator probe only permits an unmodified single click")
  }
  try require(AXIsProcessTrusted() && CGPreflightPostEventAccess(), "permission missing; no request issued")
  let originalApp = NSRunningApplication(processIdentifier: request.target.pid)
  let originalStamp = try ProcessStamp.read(request.target.pid)
  func waitForReceiver(_ microseconds: UInt32) {
    if syntheticActivation {
      // NSWorkspace/NSRunningApplication update on the run loop. Blocking sleeps
      // kept a dead foreground snapshot during real user activation in the probe.
      RunLoop.current.run(until: Date(timeIntervalSinceNow: Double(microseconds) / 1_000_000))
    } else { usleep(microseconds) }
  }
  func checkIdentity() throws {
    guard let app = NSRunningApplication(processIdentifier: request.target.pid),
      let originalApp, !originalApp.isTerminated,
      (calculator ? isCalculator(app, executable: request.target.executable)
        : mirror ? isMirror(app, executable: request.target.executable) : isFocusTarget(app, request: request))
    else { throw ProbeError("probe target identity changed") }
    try require(try ProcessStamp.read(request.target.pid) == originalStamp, "probe target process instance changed")
  }
  func check() throws {
    try requireOwner()
    try checkIdentity()
    let foreground = NSWorkspace.shared.frontmostApplication
    if syntheticActivation {
      emit(["kind": "focus-check", "foregroundPID": foreground?.processIdentifier ?? 0])
    }
    guard let foreground,
      foreground.processIdentifier == request.guardPID,
      (calculator || foreground.bundleIdentifier == "com.teatak.pudding.background-probe.guard")
    else { throw ProbeError("foreground guard changed") }
    guard let info = windowInfo(request.target.pid, request.target.windowID, external: mirror || calculator || request.realApp != nil),
      let bounds = info[kCGWindowBounds as String] as? NSDictionary,
      let frame = CGRect(dictionaryRepresentation: bounds as CFDictionary), frame == request.target.frame.rect
    else { throw ProbeError("target window changed") }
  }
  try check()
  let windows = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], 0) as? [[String: Any]] ?? []
  let pointForScene = request.target.points[request.action]!.point
  let covering = windows.first { info in
    guard let bounds = info[kCGWindowBounds as String] as? NSDictionary,
      let frame = CGRect(dictionaryRepresentation: bounds as CFDictionary),
      (info[kCGWindowLayer as String] as? NSNumber)?.intValue == 0,
      (info[kCGWindowAlpha as String] as? NSNumber)?.doubleValue ?? 1 > 0
    else { return false }
    return frame.contains(pointForScene)
  }
  // Only normal-window stacking, not a global hit-test: Dock has a transparent
  // full-display layer-20 surface on macOS 26. Do not call its bounds an occlusion.
  emit(["kind": "scene", "topmostNormalWindowPID": covering?[kCGWindowOwnerPID as String] ?? 0,
        "targetPID": request.target.pid, "guardPID": request.guardPID])
  let setter: WindowLocationSetter?
  if variant.usesPrivateAPI {
    guard let symbol = dlsym(UnsafeMutableRawPointer(bitPattern: -2), "CGEventSetWindowLocation") else {
      throw ProbeError("private window-location symbol unavailable; no fallback")
    }
    setter = unsafeBitCast(symbol, to: WindowLocationSetter.self)
  } else { setter = nil }
  let point = request.target.points[request.action]!.point
  var events: [CGEvent] = []
  func append(_ type: CGEventType, at: CGPoint? = nil, count: Int = 1) throws {
    events.append(try buildEvent(variant: variant, type: type, target: request.target,
      point: at ?? point, count: count, setter: setter, scrollY: request.scrollY ?? -80))
  }
  if let probeClicks {
    for click in probeClicks {
      for type: CGEventType in [.leftMouseDown, .leftMouseUp] {
        events.append(try buildEvent(variant: variant, type: type, target: request.target,
          point: point, count: click.clickCount, setter: setter, eventNumber: click.eventNumber))
      }
    }
    emit(["kind": "click-plan", "clicks": probeClicks.map { ["clickCount": $0.clickCount, "eventNumber": $0.eventNumber] }])
  } else { switch request.action {
  case "click", "double":
    for count in 1...(request.action == "double" ? 2 : 1) {
      try append(.leftMouseDown, count: count); try append(.leftMouseUp, count: count)
    }
  case "right": try append(.rightMouseDown); try append(.rightMouseUp)
  case "drag":
    let end = request.end?.point ?? CGPoint(x: point.x + 60, y: point.y)
    try append(.leftMouseDown)
    for step in 1...6 {
      let fraction = Double(step) / 6
      try append(.leftMouseDragged, at: CGPoint(x: point.x + (end.x - point.x) * fraction,
        y: point.y + (end.y - point.y) * fraction))
    }
    try append(.leftMouseUp, at: end)
  case "scroll": try append(.mouseMoved, count: 0); try append(.scrollWheel, count: 0)
  default: throw ProbeError("unknown action")
  } }
  let activation = syntheticActivation ? try focusNotification(activated: true) : nil
  // Construct every event before sending. No real activation, warp, HID route or retries.
  var sent = 0
  var release: CGEvent?
  defer {
    if let focusLease {
      do { try restoreFocus(request, lease: focusLease, originalApp: originalApp, stamp: originalStamp) }
      catch { emit(["kind": "recovery-error", "error": String(describing: error)]) }
    }
    // Other diagnostic variants retain their existing local mouse pairing.
    if let release, (try? checkIdentity()) != nil,
      windowInfo(request.target.pid, request.target.windowID, external: mirror || calculator || request.realApp != nil) != nil {
      release.postToPid(request.target.pid)
      emit(["kind": "cleanup", "event": "button-up", "sentBeforeFailure": sent])
    }
  }
  if let activation {
    try check()
    try focusLease!.set(.activated) // Write intent before any irreversible posting.
    activation.postToPid(request.target.pid)
    emit(["kind": "focus-notification", "activated": true, "targetPID": request.target.pid])
    waitForReceiver(100_000) // Allow the receiver to process the notification; never retry the click.
  }
  for (index, event) in (notifyOnly ? [] : events).enumerated() {
    try check()
    if event.type == .leftMouseDown { try focusLease?.set(.pressed) }
    event.postToPid(request.target.pid)
    sent += 1
    if let focusLease {
      if event.type == .leftMouseUp { try focusLease.set(.released) }
    } else if event.type == .leftMouseDown || event.type == .rightMouseDown {
      release = events.dropFirst(index + 1).first { $0.type == .leftMouseUp || $0.type == .rightMouseUp }
    } else if event.type == .leftMouseUp || event.type == .rightMouseUp { release = nil }
    if syntheticActivation {
      emit(["kind": "input-posted", "type": event.type.rawValue, "count": sent])
    }
    waitForReceiver(50_000)
  }
  if notifyOnly { waitForReceiver(100_000) } // Match the two omitted per-event waits in the no-click control.
  emit(["kind": "sent", "count": sent, "variant": variant.rawValue,
        "privateAPI": variant.usesPrivateAPI, "command": variant == .command])
}

func mainWindowInfo(_ candidates: [[String: Any]], pid: pid_t, frame mainFrame: CGRect) throws -> [String: Any] {
  let matching = candidates.filter {
    guard ($0[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value == pid,
      ($0[kCGWindowLayer as String] as? NSNumber)?.intValue == 0,
      let raw = $0[kCGWindowBounds as String] as? NSDictionary,
      let frame = CGRect(dictionaryRepresentation: raw as CFDictionary) else { return false }
    return frame == mainFrame
  }
  guard matching.count == 1, let info = matching.first else { throw ProbeError("mirror main CG window missing or ambiguous") }
  return info
}

func mirrorState() throws -> [String: Any] {
  let apps = NSWorkspace.shared.runningApplications.filter { $0.bundleIdentifier == "com.apple.ScreenContinuity" }
  guard apps.count == 1, let app = apps.first, let executable = app.executableURL?.path,
    isMirror(app, executable: executable)
  else { throw ProbeError("running system iPhone Mirroring window missing or ambiguous") }
  func attribute(_ element: AXUIElement, _ name: String) -> CFTypeRef? {
    var value: CFTypeRef?
    guard AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success else { return nil }
    return value
  }
  let windows = attribute(AXUIElementCreateApplication(app.processIdentifier), kAXWindowsAttribute) as? [AXUIElement] ?? []
  let main = windows.filter { attribute($0, kAXIdentifierAttribute) as? String == "iphone-mirroring-main" }
  guard main.count == 1,
    let position = attribute(main[0], kAXPositionAttribute), CFGetTypeID(position) == AXValueGetTypeID(),
    let size = attribute(main[0], kAXSizeAttribute), CFGetTypeID(size) == AXValueGetTypeID()
  else { throw ProbeError("mirror main AX window missing or ambiguous") }
  var origin = CGPoint.zero, extent = CGSize.zero
  guard AXValueGetValue(position as! AXValue, .cgPoint, &origin), AXValueGetValue(size as! AXValue, .cgSize, &extent)
  else { throw ProbeError("mirror main AX geometry unavailable") }
  let mainFrame = CGRect(origin: origin, size: extent)
  let candidates = CGWindowListCopyWindowInfo(.optionOnScreenOnly, 0) as? [[String: Any]] ?? []
  let info = try mainWindowInfo(candidates, pid: app.processIdentifier, frame: mainFrame)
  var displays = [CGDirectDisplayID](repeating: 0, count: 32), displayCount: UInt32 = 0
  guard CGGetActiveDisplayList(32, &displays, &displayCount) == .success else { throw ProbeError("display list unavailable") }
  let containing = displays.prefix(Int(displayCount)).map { CGDisplayBounds($0) }.filter {
    $0.contains(CGPoint(x: mainFrame.midX, y: mainFrame.midY))
  }
  guard containing.count == 1, let bounds = containing.first else { throw ProbeError("mirror display missing or ambiguous") }
  return ["pid": app.processIdentifier, "executable": executable, "windowID": info[kCGWindowNumber as String]!,
    "frame": ["x": mainFrame.minX, "y": mainFrame.minY, "width": mainFrame.width, "height": mainFrame.height],
    "active": app.isActive, "points": [String: String](),
    "display": ["x": bounds.minX, "y": bounds.minY, "width": bounds.width, "height": bounds.height]]
}

@available(macOS 14.2, *)
func captureWindow(_ target: Target, output: String, realApp: RealApp? = nil) async throws {
  try require(CGPreflightScreenCaptureAccess(), "screen permission missing; no request issued")
  guard let app = NSRunningApplication(processIdentifier: target.pid),
    realApp?.matches(app, executable: target.executable) ?? isMirror(app, executable: target.executable) else {
    throw ProbeError("capture target identity changed")
  }
  let content = try await SCShareableContent.excludingDesktopWindows(true, onScreenWindowsOnly: true)
  guard let window = content.windows.first(where: {
    $0.windowID == target.windowID && $0.owningApplication?.processID == target.pid
      && $0.owningApplication?.bundleIdentifier == (realApp?.bundleID ?? "com.apple.ScreenContinuity")
  }), window.frame == target.frame.rect else { throw ProbeError("target window changed before capture") }
  let config = SCStreamConfiguration()
  config.width = Int(window.frame.width); config.height = Int(window.frame.height)
  config.showsCursor = false; config.capturesAudio = false; config.ignoreShadowsSingleWindow = true
  let image = try await SCScreenshotManager.captureImage(contentFilter: SCContentFilter(desktopIndependentWindow: window), configuration: config)
  guard let png = NSBitmapImageRep(cgImage: image).representation(using: .png, properties: [:]) else { throw ProbeError("PNG encoding failed") }
  try png.write(to: URL(fileURLWithPath: output), options: .withoutOverwriting)
  emit(["kind": "capture", "output": output, "width": image.width, "height": image.height])
}

func selfTest() throws {
  try focusLeaseSelfTest()
  for activated in [true, false] {
    let event = try focusNotification(activated: activated)
    guard let decoded = NSEvent(cgEvent: event) else { throw ProbeError("focus notification did not round-trip") }
    try require(decoded.type == .appKitDefined && decoded.windowNumber == 0
      && decoded.subtype == (activated ? .applicationActivated : .applicationDeactivated)
      && decoded.modifierFlags.intersection(.deviceIndependentFlagsMask).isEmpty,
      "focus notification changed type, scope or modifiers")
  }
  let target = Target(pid: 100, windowID: 42, executable: "/unused/test-only",
    frame: CGRectRecord(x: -400, y: 20, width: 400, height: 400),
    points: ["click": PointRecord(x: -200, y: 100), "drag": PointRecord(x: -200, y: 100),
             "scroll": PointRecord(x: -200, y: 100)])
  _ = try validate(Request(target: target, guardPID: 200, variant: "appkit", action: "click"))
  _ = try validate(Request(target: target, guardPID: 200, variant: "appkit", action: "drag"))
  _ = try validate(Request(target: target, guardPID: 200, variant: "appkit-window-local", action: "drag",
    end: PointRecord(x: -399, y: 21)))
  _ = try validate(Request(target: target, guardPID: 200, variant: "appkit-window-local", action: "scroll", scrollY: -600))
  var rejected = 0
  for request in [
    Request(target: target, guardPID: 100, variant: "appkit", action: "click"),
    Request(target: target, guardPID: 200, variant: "invalid", action: "click"),
    Request(target: target, guardPID: 200, variant: "appkit", action: "press"),
    Request(target: target, guardPID: 200, variant: "appkit", action: "right"),
    Request(target: target, guardPID: 200, variant: "appkit", action: "click", end: PointRecord(x: -100, y: 100)),
    Request(target: target, guardPID: 200, variant: "appkit", action: "drag", end: PointRecord(x: .nan, y: 100)),
    Request(target: target, guardPID: 200, variant: "appkit", action: "drag", end: PointRecord(x: 1, y: 100)),
    Request(target: target, guardPID: 200, variant: "appkit", action: "click", scrollY: 1),
    Request(target: target, guardPID: 200, variant: "appkit", action: "scroll", scrollY: 0),
    Request(target: target, guardPID: 200, variant: "appkit", action: "scroll", scrollY: Int32.min),
    Request(target: Target(pid: 100, windowID: 0, executable: "/unused",
      frame: target.frame, points: target.points), guardPID: 200, variant: "appkit", action: "click"),
    Request(target: Target(pid: 100, windowID: 42, executable: "/unused",
      frame: target.frame, points: ["click": PointRecord(x: 1, y: 100)]),
      guardPID: 200, variant: "appkit", action: "click"),
    Request(target: Target(pid: 100, windowID: 42, executable: "/unused",
      frame: target.frame, points: ["click": PointRecord(x: .nan, y: 100)]),
      guardPID: 200, variant: "appkit", action: "click"),
    Request(target: Target(pid: 100, windowID: 42, executable: "/unused",
      frame: target.frame, points: ["drag": PointRecord(x: -20, y: 100)]),
      guardPID: 200, variant: "appkit", action: "drag"),
  ] {
    do { _ = try validate(request) } catch { rejected += 1 }
  }
  try require(rejected == 14, "invalid request accepted")
  let mainWindow: [String: Any] = [kCGWindowOwnerPID as String: 100, kCGWindowLayer as String: 0,
    kCGWindowNumber as String: 42, kCGWindowBounds as String: target.frame.rect.dictionaryRepresentation]
  var indicator = mainWindow
  indicator[kCGWindowNumber as String] = 99
  indicator[kCGWindowBounds as String] = CGRect(x: -390, y: 25, width: 66, height: 20).dictionaryRepresentation
  let selected = try mainWindowInfo([indicator, mainWindow], pid: 100, frame: target.frame.rect)
  try require((selected[kCGWindowNumber as String] as? Int) == 42, "selected indicator instead of main window")
  for candidates in [[indicator], [mainWindow, mainWindow]] {
    do {
      _ = try mainWindowInfo(candidates, pid: 100, frame: target.frame.rect)
      throw ProbeError("missing or ambiguous main window accepted")
    } catch let error as ProbeError {
      try require(error.description == "mirror main CG window missing or ambiguous", error.description)
    }
  }
  let event = try buildEvent(variant: .appkit, type: .rightMouseDown, target: target,
    point: CGPoint(x: -200, y: 100), count: 2, setter: nil)
  try require(event.flags.isEmpty && event.type == .rightMouseDown, "unexpected event type/modifiers")
  try require(event.getIntegerValueField(.mouseEventWindowUnderMousePointer) == 42
    && event.getIntegerValueField(.mouseEventWindowUnderMousePointerThatCanHandleThisEvent) == 42,
    "wrong target window tags")
  try require(event.getIntegerValueField(.mouseEventClickState) == 2
    && event.getIntegerValueField(.mouseEventButtonNumber) == 1, "wrong click metadata")
  for count in 1...2 {
    for number in 1...2 {
      let independent = try buildEvent(variant: .appkit, type: .leftMouseDown, target: target,
        point: CGPoint(x: -200, y: 100), count: count, setter: nil, eventNumber: number)
      guard let decoded = NSEvent(cgEvent: independent) else { throw ProbeError("event decode failed") }
      try require(decoded.clickCount == count && decoded.eventNumber == number, "click metadata coupled")
    }
  }
  var rejectedPlans = 0
  for clicks: [ProbeClick] in [[], [ProbeClick(clickCount: 0, eventNumber: 1)],
    [ProbeClick(clickCount: 1, eventNumber: 3)], Array(repeating: ProbeClick(clickCount: 1, eventNumber: 1), count: 3)] {
    do { try validateProbeClicks(clicks) } catch { rejectedPlans += 1 }
  }
  try require(rejectedPlans == 4, "invalid click plan accepted")
  var rejectedCalculatorRequests = 0
  for request in [
    Request(target: target, guardPID: 200, variant: "appkit-command", action: "click"),
    Request(target: target, guardPID: 200, variant: "appkit-window-local", action: "drag"),
    Request(target: target, guardPID: 200, variant: "quartz", action: "click"),
  ] {
    do { try send(request, calculator: true) }
    catch let error as ProbeError {
      try require(error.description == "calculator probe only permits an unmodified single click", error.description)
      rejectedCalculatorRequests += 1
    }
  }
  try require(rejectedCalculatorRequests == 3, "unsafe Calculator test action accepted")
  var rejectedFocusRequests = 0
  for (mirror, calculator, variant, action) in [(true, false, "appkit-window-local", "click"),
    (false, true, "appkit-window-local", "click"), (false, false, "appkit-command", "click"),
    (false, false, "appkit-window-local", "drag")] {
    do { try send(Request(target: target, guardPID: 200, variant: variant, action: action),
      mirror: mirror, calculator: calculator, syntheticActivation: true) }
    catch let error as ProbeError {
      try require(error.description == "synthetic activation probe only permits one ordinary click", error.description)
      rejectedFocusRequests += 1
    }
  }
  try require(rejectedFocusRequests == 4, "unsafe synthetic focus test action accepted")
  try realAppSelfTest(target: target)
  do {
    _ = try buildEvent(variant: .windowLocal, type: .leftMouseDown, target: target,
      point: CGPoint(x: -200, y: 100), count: 1, setter: nil)
    throw ProbeError("private API silently fell back")
  } catch let error as ProbeError {
    try require(error.description == "private window-location symbol unavailable; no fallback", error.description)
  }
  emit(["kind": "self-test", "ok": true, "rejectedRequests": rejected, "rejectedClickPlans": rejectedPlans,
    "rejectedCalculatorRequests": rejectedCalculatorRequests, "rejectedFocusRequests": rejectedFocusRequests, "eventsPosted": 0])
}

let mode = CommandLine.arguments.dropFirst().first ?? "status"
if mode == "target" || mode == "guard" {
  guard Bundle.main.bundleIdentifier == "com.teatak.pudding.background-probe.\(mode)" else { exit(2) }
  let app = ProbeApplication.shared
  app.setActivationPolicy(.regular)
  let delegate = Fixture(role: mode)
  app.delegate = delegate
  app.run()
} else if mode == "click-probe-send" {
  struct Input: Decodable { let request: Request; let clicks: [ProbeClick] }
  do {
    let input = try JSONDecoder().decode(Input.self, from: FileHandle.standardInput.readDataToEndOfFile())
    try send(input.request, probeClicks: input.clicks)
  } catch { emit(["kind": "error", "error": String(describing: error)]); exit(1) }
} else if mode == "focus-probe-send" || mode == "real-app-notify" {
  do { try sendWithFocusLease(FileHandle.standardInput.readDataToEndOfFile(), notifyOnly: mode == "real-app-notify") }
  catch { emit(["kind": "error", "error": String(describing: error)]); exit(1) }
} else if mode == "focus-lease-descriptor-test" {
  do { try focusLeaseDescriptorTest() }
  catch { emit(["kind": "error", "error": String(describing: error)]); exit(1) }
} else if mode == "focus-probe-worker" {
  // Control-pipe EOF cancels input. Diagnostics EPIPE is not permission to skip cleanup.
  signal(SIGPIPE, SIG_IGN)
  do {
    struct Identity: Decodable { let processStart: ProcessStamp; let notifyOnly: Bool }
    let data = try readFocusRequest()
    let request = try JSONDecoder().decode(Request.self, from: data)
    let identity = try JSONDecoder().decode(Identity.self, from: data)
    let lease = try FocusLease.inherited()
    try require(try ProcessStamp.read(request.target.pid) == identity.processStart,
      "target identity changed before worker started")
    try send(request, syntheticActivation: true, focusLease: lease, notifyOnly: identity.notifyOnly,
      requireOwner: { try requireInputOwner(STDIN_FILENO) })
  } catch {
    emit(["kind": "error", "error": String(describing: error), "outcome": "inspect_receiver_log"])
    exit(1)
  }
} else if mode == "send" || mode == "mirror-send" || mode == "calculator-send" {
  do {
    let request = try JSONDecoder().decode(Request.self, from: FileHandle.standardInput.readDataToEndOfFile())
    try send(request, mirror: mode == "mirror-send", calculator: mode == "calculator-send")
  } catch {
    emit(["kind": "error", "error": String(describing: error), "outcome": "inspect_receiver_log"])
    exit(1)
  }
} else if mode == "mirror-window" {
  do { emit(try mirrorState()) } catch { emit(["kind": "error", "error": String(describing: error)]); exit(1) }
} else if mode == "calculator-window" {
  do { emit(try calculatorState()) } catch { emit(["kind": "error", "error": String(describing: error)]); exit(1) }
} else if mode == "calculator-listen" {
  do { try listenToCalculator() } catch { emit(["kind": "error", "error": String(describing: error)]); exit(1) }
} else if mode == "real-app-windows" {
  do {
    struct Input: Decodable { let realApp: RealApp }
    let input = try JSONDecoder().decode(Input.self, from: FileHandle.standardInput.readDataToEndOfFile())
    emit(try input.realApp.windows())
  } catch { emit(["kind": "error", "error": String(describing: error)]); exit(1) }
} else if mode == "mirror-capture" || mode == "real-app-capture" {
  if #available(macOS 14.2, *) {
    // ScreenCaptureKit needs an initialized WindowServer/AppKit connection even
    // in this short-lived CLI; remain prohibited from activating any window.
    let app = NSApplication.shared
    app.setActivationPolicy(.prohibited)
    struct CaptureRequest: Decodable { let target: Target; let output: String; var realApp: RealApp? = nil }
    do {
      let input = try JSONDecoder().decode(CaptureRequest.self, from: FileHandle.standardInput.readDataToEndOfFile())
      try require((mode == "real-app-capture") == (input.realApp != nil), "capture mode/target mismatch")
      Task { @MainActor in
        do { try await captureWindow(input.target, output: input.output, realApp: input.realApp); exit(0) }
        catch { emit(["kind": "error", "error": String(describing: error)]); exit(1) }
      }
      RunLoop.main.run()
    } catch { emit(["kind": "error", "error": String(describing: error)]); exit(1) }
  } else { emit(["kind": "error", "error": "mirror capture probe requires macOS 14.2"]); exit(1) }
} else if mode == "window" {
  do {
    let input = try JSONSerialization.jsonObject(with: FileHandle.standardInput.readDataToEndOfFile()) as! [String: Any]
    guard let pid = input["pid"] as? Int32, let executable = input["executable"] as? String,
      let app = NSRunningApplication(processIdentifier: pid), isFixture(app, executable: executable),
      let info = windowInfo(pid), let frame = info[kCGWindowBounds as String] as? NSDictionary
    else { throw ProbeError("test-owned window missing") }
    emit(["windowID": info[kCGWindowNumber as String]!, "frame": ["x": frame["X"]!, "y": frame["Y"]!,
      "width": frame["Width"]!, "height": frame["Height"]!], "active": app.isActive])
  } catch { emit(["kind": "error", "error": String(describing: error)]); exit(1) }
} else if mode == "self-test" {
  do { try selfTest() } catch { emit(["error": String(describing: error)]); exit(1) }
} else if mode == "status" {
  emit(["accessibility": AXIsProcessTrusted(), "postEvents": CGPreflightPostEventAccess(),
        "screenCapture": CGPreflightScreenCaptureAccess(), "os": ProcessInfo.processInfo.operatingSystemVersionString])
} else { exit(2) }
