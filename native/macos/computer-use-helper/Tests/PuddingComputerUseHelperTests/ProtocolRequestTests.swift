import Testing

@testable import PuddingComputerUseHelper

@Test func protocolObserveDefaultsToBoundedTree() throws {
  let request = ProtocolRequest(
    id: "req-1",
    command: "observe",
    params: ProtocolParameters(bundleID: "com.apple.TextEdit")
  )

  #expect(
    try request.helperCommand()
      == .observe(bundleID: "com.apple.TextEdit", windowID: nil, maxElements: 200))
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

  #expect(throws: ArgumentError.invalidOption("value", "not allowed for press")) {
    try request.helperCommand()
  }
}

@Test func protocolRejectsOversizedElementLimit() {
  let request = ProtocolRequest(
    id: "req-3",
    command: "observe",
    params: ProtocolParameters(
      bundleID: "com.apple.TextEdit",
      maxElements: 1_001
    )
  )

  #expect(throws: ArgumentError.invalidOption("maxElements", "1001")) {
    try request.helperCommand()
  }
}

@Test func protocolCaptureRoutesExplicitApplicationAndWindow() throws {
  let request = ProtocolRequest(
    id: "req-4",
    command: "capture",
    params: ProtocolParameters(
      bundleID: "com.apple.TextEdit",
      windowID: 42,
      output: "/tmp/window.png"
    )
  )

  #expect(
    try request.helperCommand()
      == .capture(bundleID: "com.apple.TextEdit", windowID: 42, output: "/tmp/window.png"))
}

@Test func protocolUseRoutesBundleID() throws {
  let request = ProtocolRequest(
    id: "req-5",
    command: "use_app",
    params: ProtocolParameters(bundleID: "com.apple.calculator")
  )

  #expect(try request.helperCommand() == .useApp(bundleID: "com.apple.calculator"))
}

@Test func protocolApplicationIdentityRoutesBundleID() throws {
  let request = ProtocolRequest(
    id: "req-identity",
    command: "app_identity",
    params: ProtocolParameters(bundleID: "com.apple.Notes")
  )

  #expect(try request.helperCommand() == .applicationIdentity(bundleID: "com.apple.Notes"))
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
