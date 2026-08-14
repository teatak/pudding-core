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
      "--max-elements", "50",
    ]) == .observe(bundleID: "com.apple.TextEdit", windowID: nil, maxElements: 50))
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

@Test func captureRequiresExplicitApplication() {
  #expect(throws: ArgumentError.missingOption("--bundle-id")) {
    try ArgumentParser.parse([
      "capture",
      "--window-id", "42",
      "--output", "/tmp/window.png",
    ])
  }
}

@Test func pressRejectsValue() {
  #expect(throws: ArgumentError.invalidOption("--value", "not allowed for press")) {
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

@Test func parsesLaunchApplication() throws {
  #expect(
    try ArgumentParser.parse([
      "launch-app", "--bundle-id", "com.apple.calculator",
    ]) == .launchApp(bundleID: "com.apple.calculator"))
}

@Test func parsesQuitApplication() throws {
  #expect(
    try ArgumentParser.parse([
      "quit-app", "--bundle-id", "com.apple.calculator", "--pid", "42",
    ]) == .quitApp(bundleID: "com.apple.calculator", pid: 42))
}
