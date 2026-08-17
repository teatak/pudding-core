import Testing

@testable import PuddingComputerUseHelper

@Test func parsesServe() throws {
  #expect(try ArgumentParser.parse(["serve"]) == .serve)
}

@Test func parsesPermissionsWithoutPrompting() throws {
  #expect(
    try ArgumentParser.parse(["permissions"])
      == .permissions(
        promptAccessibility: false,
        promptScreenRecording: false
      ))
}

@Test func parsesObserve() throws {
  #expect(
    try ArgumentParser.parse([
      "observe",
      "--bundle-id", "com.apple.TextEdit",
      "--window-id", "42",
      "--max-elements", "50",
    ]) == .observe(bundleID: "com.apple.TextEdit", windowID: 42, maxElements: 50))
}

@Test func observeRequiresExplicitWindow() {
  #expect(throws: ArgumentError.missingOption("--window-id")) {
    try ArgumentParser.parse(["observe", "--bundle-id", "com.apple.TextEdit"])
  }
}

@Test func setValueRequiresValue() {
  #expect(throws: ArgumentError.missingOption("--value")) {
    try ArgumentParser.parse([
      "act",
      "--bundle-id", "com.apple.TextEdit",
      "--window-id", "42",
      "--element-id", "1",
      "--action", "set_value",
    ])
  }
}

@Test func observeCaptureRequiresExplicitApplication() {
  #expect(throws: ArgumentError.missingOption("--bundle-id")) {
    try ArgumentParser.parse([
      "observe-capture",
      "--window-id", "42",
      "--output", "/tmp/window.png",
    ])
  }
}

@Test func pressRejectsValue() {
  #expect(throws: ArgumentError.invalidOption("--value", "allowed only for set_value")) {
    try ArgumentParser.parse([
      "act",
      "--bundle-id", "com.apple.calculator",
      "--window-id", "42",
      "--element-id", "1",
      "--action", "press",
      "--value", "7",
    ])
  }
}

@Test func parsesSelectAndSubmit() throws {
  for action in [ElementAction.select, .submit] {
    #expect(
      try ArgumentParser.parse([
        "act",
        "--bundle-id", "com.example.fixture",
        "--window-id", "42",
        "--element-id", "row",
        "--action", action.rawValue,
      ]) == .act(
        bundleID: "com.example.fixture",
        windowID: 42,
        elementID: "row",
        action: action,
        value: nil
      ))
  }
}

@Test func rejectsRemovedConfirmAction() {
  #expect(throws: ArgumentError.invalidOption("--action", "confirm")) {
    try ArgumentParser.parse([
      "act",
      "--bundle-id", "com.example.fixture",
      "--window-id", "42",
      "--element-id", "field",
      "--action", "confirm",
    ])
  }
}

@Test func parsesUseApplication() throws {
  #expect(
    try ArgumentParser.parse([
      "use-app", "--bundle-id", "com.apple.calculator",
    ]) == .useApp(bundleID: "com.apple.calculator", foreground: false))

  #expect(
    try ArgumentParser.parse([
      "use-app", "--bundle-id", "com.apple.calculator", "--foreground",
    ]) == .useApp(bundleID: "com.apple.calculator", foreground: true))
}

@Test func parsesApplicationIdentity() throws {
  #expect(
    try ArgumentParser.parse([
      "app-identity", "--bundle-id", "com.apple.Notes",
    ]) == .applicationIdentity(bundleID: "com.apple.Notes"))
}

@Test func parsesQuitApplication() throws {
  #expect(
    try ArgumentParser.parse([
      "quit-app", "--bundle-id", "com.apple.calculator", "--pid", "42",
    ]) == .quitApp(bundleID: "com.apple.calculator", pid: 42))
}

@Test func parsesPointerActions() throws {
  #expect(
    try ArgumentParser.parse([
      "pointer", "--bundle-id", "com.example.App", "--window-id", "42",
      "--action", "click", "--button", "left", "--click-count", "2",
      "--x", "0.12", "--y", "0.34",
    ]) == .pointer(
      bundleID: "com.example.App",
      windowID: 42,
      input: PointerInput(
        action: .click, x: 0.12, y: 0.34, toX: nil, toY: nil,
        button: .left, clickCount: 2, deltaX: nil, deltaY: nil)
    ))
}
