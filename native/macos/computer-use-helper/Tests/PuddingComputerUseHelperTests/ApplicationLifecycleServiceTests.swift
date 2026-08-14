import AppKit
import Foundation
import Testing

@testable import PuddingComputerUseHelper

@Test func applicationIdentityIncludesRenderedAppIcon() throws {
  let identity = try ApplicationLifecycleService().identity(bundleID: "com.apple.finder")
  guard
    let encoded = identity.iconPNGBase64,
    let data = Data(base64Encoded: encoded)
  else {
    Issue.record("Finder identity did not include a PNG icon")
    return
  }

  #expect(identity.name == "Finder")
  #expect(NSImage(data: data) != nil)
}
