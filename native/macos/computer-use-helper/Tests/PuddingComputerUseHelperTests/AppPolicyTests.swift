import XCTest

@testable import PuddingComputerUseHelper

final class AppPolicyTests: XCTestCase {
  func testBlocksPuddingTerminalAndPasswordManagers() {
    for bundleID in [
      "com.teatak.pudding",
      "com.teatak.pudding.computer-use-helper",
      "com.apple.Terminal",
      "com.1password.1password",
    ] {
      XCTAssertFalse(AppPolicy.allows(bundleID: bundleID, pid: 20, parentPID: 10))
    }
  }

  func testBlocksParentApplicationAndAllowsOrdinaryApps() {
    XCTAssertFalse(AppPolicy.allows(bundleID: "com.example.Parent", pid: 10, parentPID: 10))
    XCTAssertTrue(AppPolicy.allows(bundleID: "com.apple.TextEdit", pid: 20, parentPID: 10))
  }

  func testBlocksBundleBeforeLaunch() {
    XCTAssertFalse(AppPolicy.allows(bundleID: "com.apple.Terminal"))
    XCTAssertTrue(AppPolicy.allows(bundleID: "com.apple.calculator"))
  }
}
