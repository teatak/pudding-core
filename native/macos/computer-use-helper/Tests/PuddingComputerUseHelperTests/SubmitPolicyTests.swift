import Testing

@testable import PuddingComputerUseHelper

@Test func submitPrefersAccessibilityConfirm() {
  #expect(
    SubmitPolicy.resolve(
      secure: false,
      enabled: true,
      focused: true,
      editableText: true,
      supportsAccessibilityConfirm: true
    ) == .accessibilityConfirm
  )
}

@Test func submitUsesOneReturnWhenAccessibilityConfirmIsUnavailable() {
  #expect(
    SubmitPolicy.resolve(
      secure: false,
      enabled: true,
      focused: true,
      editableText: true,
      supportsAccessibilityConfirm: false
    ) == .returnKey
  )
}

@Test func submitRejectsUnsafeTargets() {
  let blocked = [
    SubmitPolicy.resolve(secure: true, enabled: true, focused: true, editableText: true, supportsAccessibilityConfirm: false),
    SubmitPolicy.resolve(secure: false, enabled: false, focused: true, editableText: true, supportsAccessibilityConfirm: false),
    SubmitPolicy.resolve(secure: false, enabled: true, focused: false, editableText: true, supportsAccessibilityConfirm: false),
    SubmitPolicy.resolve(secure: false, enabled: true, focused: true, editableText: false, supportsAccessibilityConfirm: false),
  ]
  #expect(blocked.allSatisfy { $0 == nil })
}
