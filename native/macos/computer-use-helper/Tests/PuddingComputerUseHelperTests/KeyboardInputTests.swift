import Foundation
import Testing

@testable import PuddingComputerUseHelper

@Test func keyboardSeparatesTextFromPhysicalKeys() throws {
  let key = try KeyboardInput.validated(
    action: "press_key", key: "A", modifiers: ["command"], value: nil)
  #expect(key.key == "a")
  #expect(throws: (any Error).self) {
    try KeyboardInput.validated(action: "press_key", key: "a", modifiers: nil, value: "你好")
  }
  #expect(throws: (any Error).self) {
    try KeyboardInput.validated(action: "type_text", key: "a", modifiers: nil, value: "你好")
  }
  #expect(throws: (any Error).self) {
    try KeyboardInput.validated(
      action: "press_key", key: "a", modifiers: ["command", "command"], value: nil)
  }
  #expect(throws: (any Error).self) {
    try KeyboardInput.validated(action: "press_key", key: "unknown", modifiers: nil, value: nil)
  }
  #expect(
    try KeyboardInput.validated(action: "type_text", key: nil, modifiers: nil, value: "你好🙂").value
      == "你好🙂")
}

@Test func textSelectionUsesUTF16AndRejectsAmbiguity() throws {
  #expect(
    try AccessibilityService.uniqueTextRange(text: "a🙂你好b", selection: "你好")
      == NSRange(location: 3, length: 2))
  #expect(throws: (any Error).self) {
    try AccessibilityService.uniqueTextRange(text: "你好你好", selection: "你好")
  }
  #expect(throws: (any Error).self) {
    try AccessibilityService.uniqueTextRange(text: "aaa", selection: "aa")
  }
  #expect(throws: (any Error).self) {
    try AccessibilityService.uniqueTextRange(text: "a", selection: "b")
  }
}

@Test func imageOnlyProtocolSkipsAX() throws {
  let command = try ProtocolRequest(
    id: "image", command: "observe_capture",
    params: ProtocolParameters(
      bundleID: "com.example.App", windowID: 42, output: "/tmp/image.png",
      includeAccessibility: false)
  ).helperCommand()
  #expect(
    command
      == .observeCapture(
        bundleID: "com.example.App", windowID: 42, maxElements: 200, output: "/tmp/image.png",
        includeAccessibility: false))
}
