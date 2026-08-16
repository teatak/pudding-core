import Testing

@testable import PuddingComputerUseHelper

@Test func protocolObserveDefaultsToBoundedTree() throws {
  let request = ProtocolRequest(
    id: "req-1",
    command: "observe",
    params: ProtocolParameters(bundleID: "com.apple.TextEdit", windowID: 42)
  )

  #expect(
    try request.helperCommand()
      == .observe(bundleID: "com.apple.TextEdit", windowID: 42, maxElements: 200))
}

@Test func protocolActRejectsPressValue() {
  let request = ProtocolRequest(
    id: "req-2",
    command: "act",
    params: ProtocolParameters(
      bundleID: "com.apple.calculator",
      elementID: "e_123",
      action: "press",
      value: "7"
    )
  )

  #expect(throws: ArgumentError.invalidOption("value", "allowed only for set_value")) {
    try request.helperCommand()
  }
}

@Test func protocolActRejectsOversizedValue() {
  let request = ProtocolRequest(
    id: "req-long-value",
    command: "act",
    params: ProtocolParameters(
      bundleID: "com.apple.TextEdit",
      elementID: "e_field",
      action: "set_value",
      value: String(repeating: "🙂", count: 20_001)
    )
  )

  #expect(throws: ArgumentError.invalidOption("value", "too long")) {
    try request.helperCommand()
  }
}

@Test func protocolSubmitRoutesWithoutValue() throws {
  let request = ProtocolRequest(
    id: "req-submit",
    command: "act",
    params: ProtocolParameters(
      bundleID: "com.google.Chrome",
      windowID: 42,
      elementID: "address",
      action: "submit"
    )
  )

  #expect(
    try request.helperCommand()
      == .act(
        bundleID: "com.google.Chrome",
        windowID: 42,
        elementID: "address",
        action: .submit,
        value: nil
      ))
}

@Test func protocolRejectsRemovedConfirmAction() {
  let request = ProtocolRequest(
    id: "req-confirm",
    command: "act",
    params: ProtocolParameters(
      bundleID: "com.example.fixture",
      windowID: 42,
      elementID: "field",
      action: "confirm"
    )
  )

  #expect(throws: ArgumentError.invalidOption("action", "confirm")) {
    try request.helperCommand()
  }
}

@Test func protocolRejectsOversizedElementLimit() {
  let request = ProtocolRequest(
    id: "req-3",
    command: "observe",
    params: ProtocolParameters(
      bundleID: "com.apple.TextEdit",
      maxElements: 1_001,
      windowID: 42
    )
  )

  #expect(throws: ArgumentError.invalidOption("maxElements", "1001")) {
    try request.helperCommand()
  }
}

@Test func protocolObserveRequiresExplicitWindow() {
  let request = ProtocolRequest(
    id: "req-window",
    command: "observe",
    params: ProtocolParameters(bundleID: "com.apple.TextEdit")
  )

  #expect(throws: ArgumentError.missingOption("windowID")) {
    try request.helperCommand()
  }
}

@Test func protocolObserveCaptureRoutesOneAtomicRead() throws {
  let request = ProtocolRequest(
    id: "req-4",
    command: "observe_capture",
    params: ProtocolParameters(
      bundleID: "com.apple.TextEdit",
      maxElements: 50,
      windowID: 42,
      output: "/tmp/window.png"
    )
  )

  #expect(
    try request.helperCommand()
      == .observeCapture(
        bundleID: "com.apple.TextEdit",
        windowID: 42,
        maxElements: 50,
        output: "/tmp/window.png"
      ))
}

@Test func protocolUseDefaultsToBackgroundAndRoutesExplicitForeground() throws {
  let backgroundRequest = ProtocolRequest(
    id: "req-5",
    command: "use_app",
    params: ProtocolParameters(bundleID: "com.apple.calculator")
  )
  let foregroundRequest = ProtocolRequest(
    id: "req-foreground",
    command: "use_app",
    params: ProtocolParameters(bundleID: "com.apple.calculator", foreground: true)
  )

  #expect(
    try backgroundRequest.helperCommand()
      == .useApp(bundleID: "com.apple.calculator", foreground: false))
  #expect(
    try foregroundRequest.helperCommand()
      == .useApp(bundleID: "com.apple.calculator", foreground: true))
}

@Test func protocolApplicationIdentityRoutesBundleID() throws {
  let request = ProtocolRequest(
    id: "req-identity",
    command: "app_identity",
    params: ProtocolParameters(bundleID: "com.apple.Notes")
  )

  #expect(try request.helperCommand() == .applicationIdentity(bundleID: "com.apple.Notes"))
}

@Test func protocolPointerRoutesActionAndCaptureGeometry() throws {
  let request = ProtocolRequest(
    id: "req-click",
    command: "pointer",
    params: ProtocolParameters(
      bundleID: "com.example.App",
      windowID: 42,
      action: "drag",
      x: 12,
      y: 34,
      toX: 56,
      toY: 78,
      captureWidth: 200,
      captureHeight: 100,
      scaleFactor: 2
    )
  )
  #expect(
    try request.helperCommand() == .pointer(
      bundleID: "com.example.App",
      windowID: 42,
      input: PointerInput(
        action: .drag, x: 12, y: 34, toX: 56, toY: 78,
        button: nil, clickCount: nil, deltaX: nil, deltaY: nil),
      captureWidth: 200,
      captureHeight: 100,
      scaleFactor: 2
    ))
}

@Test func protocolPointerRejectsMixedOrUnsafeGestures() {
  let common = ProtocolParameters(
    bundleID: "com.example.App", windowID: 42, action: "click",
    x: 12, y: 34, button: "right", clickCount: 2,
    captureWidth: 200, captureHeight: 100, scaleFactor: 2)
  #expect(throws: ArgumentError.self) {
    try ProtocolRequest(id: "right-double", command: "pointer", params: common).helperCommand()
  }
  #expect(throws: ArgumentError.self) {
    try ProtocolRequest(
      id: "drag-missing-end", command: "pointer",
      params: ProtocolParameters(
        bundleID: "com.example.App", windowID: 42, action: "drag",
        x: 12, y: 34, captureWidth: 200, captureHeight: 100, scaleFactor: 2)
    ).helperCommand()
  }
  #expect(throws: ArgumentError.self) {
    try ProtocolRequest(
      id: "scroll-zero", command: "pointer",
      params: ProtocolParameters(
        bundleID: "com.example.App", windowID: 42, action: "scroll",
        x: 12, y: 34, deltaX: 0, deltaY: 0,
        captureWidth: 200, captureHeight: 100, scaleFactor: 2)
    ).helperCommand()
  }
}

@Test func protocolQuitRequiresPositivePID() {
  let request = ProtocolRequest(
    id: "req-6",
    command: "quit_app",
    params: ProtocolParameters(bundleID: "com.apple.calculator", pid: 0)
  )

  #expect(throws: ArgumentError.invalidOption("pid", "0")) {
    try request.helperCommand()
  }
}
