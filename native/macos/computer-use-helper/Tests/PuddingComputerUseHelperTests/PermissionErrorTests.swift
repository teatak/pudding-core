import Testing

@testable import PuddingComputerUseHelper

@Test func permissionErrorsExposeTheOwningMacOSPermission() {
  let accessibility = errorDetail(for: HelperError.permissionRequired("accessibility"))
  #expect(accessibility.code == "computer_permission_required")
  #expect(accessibility.permission == "accessibility")
  #expect(accessibility.outcome == .notStarted)

  let screenRecording = errorDetail(for: HelperError.permissionRequired("screen_recording"))
  #expect(screenRecording.permission == "screen_recording")
}

@Test func unrelatedErrorsDoNotClaimAPermission() {
  let detail = errorDetail(for: HelperError.windowNotFound(42))
  #expect(detail.permission == nil)
}
