import Testing

@testable import PuddingComputerUseHelper

@Test func readyForegroundDoesNotActivateOrRaise() async throws {
  try await ForegroundPolicy.ensure(
    bundleID: "test.app", isActive: { true },
    activate: { Issue.record("already active"); return false },
    windowsReady: { true },
    raiseWindows: { Issue.record("already visible; AX is unnecessary"); return [] },
    wait: { Issue.record("already ready") })
}

@Test func activationThatShowsWindowsDoesNotRequireAXRaise() async throws {
  var active = false
  var activations = 0
  try await ForegroundPolicy.ensure(
    bundleID: "test.app", isActive: { active },
    activate: { activations += 1; active = true; return true },
    windowsReady: { active },
    raiseWindows: { Issue.record("AXRaise must not be required"); return ["unsupported"] },
    wait: {})
  #expect(activations == 1)
}

@Test func rejectedAXRaiseCanStillSucceedIfWindowsBecomeReady() async throws {
  var ready = false
  var raises = 0
  try await ForegroundPolicy.ensure(
    bundleID: "test.app", isActive: { true }, activate: { false },
    windowsReady: { ready },
    raiseWindows: { raises += 1; return ["AXRaise returned -25206"] },
    wait: { ready = true })
  #expect(raises == 1)
}

@Test func failedWindowRaiseRetainsNativeDiagnostic() async {
  var raises = 0
  var waits = 0
  do {
    try await ForegroundPolicy.ensure(
      bundleID: "test.app", isActive: { true }, activate: { false },
      windowsReady: { false },
      raiseWindows: { raises += 1; return ["AXRaise window[0] returned -25206"] },
      wait: { waits += 1 })
    Issue.record("covered windows should not report success")
  } catch {
    let detail = errorDetail(for: error)
    #expect(detail.code == "computer_window_raise_failed")
    #expect(detail.message.contains("-25206"))
    #expect(!detail.message.contains("launch failed"))
    #expect(detail.outcome == .unknown)
    #expect(!detail.retryable)
  }
  #expect(raises == 1)
  #expect(waits == 40)
}

@Test func successfulAXRaiseDoesNotProveWindowReadiness() async {
  do {
    try await ForegroundPolicy.ensure(
      bundleID: "test.app", isActive: { true }, activate: { false },
      windowsReady: { false }, raiseWindows: { [] }, wait: {})
    Issue.record("AX success alone is insufficient")
  } catch {
    #expect(errorDetail(for: error).code == "computer_window_raise_failed")
  }
}

@Test func activationFailureDoesNotAttemptWindowRaise() async {
  do {
    try await ForegroundPolicy.ensure(
      bundleID: "test.app", isActive: { false }, activate: { false },
      windowsReady: { false },
      raiseWindows: { Issue.record("activation failed"); return [] }, wait: {})
    Issue.record("expected activation error")
  } catch {
    #expect(errorDetail(for: error).code == "computer_activation_failed")
  }
}

@Test func activationWaitIsBounded() async {
  var activations = 0
  var waits = 0
  do {
    try await ForegroundPolicy.ensure(
      bundleID: "test.app", isActive: { false },
      activate: { activations += 1; return true },
      windowsReady: { false },
      raiseWindows: { Issue.record("not foreground"); return [] },
      wait: { waits += 1 })
    Issue.record("expected activation timeout")
  } catch {
    #expect(errorDetail(for: error).code == "computer_activation_failed")
  }
  #expect(activations == 1)
  #expect(waits == 200)
}

@Test func foregroundLossWhileRaisingDoesNotReactivate() async {
  var active = true
  var waits = 0
  do {
    try await ForegroundPolicy.ensure(
      bundleID: "test.app", isActive: { active },
      activate: { Issue.record("must not steal foreground back"); return false },
      windowsReady: { false }, raiseWindows: { [] },
      wait: { waits += 1; active = false })
    Issue.record("expected foreground loss")
  } catch {
    let detail = errorDetail(for: error)
    #expect(detail.code == "computer_activation_failed")
    #expect(detail.message.contains("without reactivating"))
  }
  #expect(waits == 1)
}

@Test func lifecycleFailureStagesRemainDistinct() {
  let errors: [(HelperError, String)] = [
    (.launchFailed("native error"), "computer_launch_failed"),
    (.activationFailed("native error"), "computer_activation_failed"),
    (.windowRaiseFailed("AXRaise returned -25206"), "computer_window_raise_failed"),
    (.useFailed("identity mismatch"), "computer_use_failed"),
  ]
  for (error, code) in errors {
    let detail = errorDetail(for: error)
    #expect(detail.code == code)
    #expect(detail.outcome == .unknown)
    #expect(!detail.retryable)
  }
}

@Test func backgroundInputErrorExplainsRecoveryWithoutFocusStealing() {
  let detail = errorDetail(for: HelperError.appNotForeground("test.app", "Electron"))
  #expect(detail.code == "computer_app_not_foreground")
  #expect(detail.outcome == .notStarted)
  #expect(!detail.retryable)
  #expect(detail.message.contains("current foreground app: Electron"))
  #expect(detail.message.contains("Do not repeat"))
  #expect(detail.message.contains("reactivate automatically"))
}
