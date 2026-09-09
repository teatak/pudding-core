import AppKit
import Foundation
import Testing

@testable import PuddingComputerUseHelper

@Test func applicationIdentityIncludesRenderedAppIcon() throws {
  let identity = try ApplicationLifecycleService().identity(bundleID: "com.apple.finder", locale: "en")
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

@Test func applicationIdentityReadsCalculatorLocalizationsWithoutChangingIdentity() throws {
  let service = ApplicationLifecycleService()
  for (locale, name) in [("zh-CN", "计算器"), ("en", "Calculator"), ("zh-TW", "計算機"), ("zh-CN", "计算器")] {
    let identity = try service.identity(bundleID: "com.apple.calculator", locale: locale)
    #expect(identity.bundleID == "com.apple.calculator")
    #expect(identity.name == name)
    #expect(identity.iconPNGBase64?.isEmpty == false)
  }
}

@Test func applicationInventoryIncludesInstalledAppsThatNeedNotBeRunning() {
  let apps = ApplicationLifecycleService().listApplications()
  let calculator = apps.first(where: { $0.bundleID == "com.apple.calculator" })
  #expect(calculator != nil)
  #expect(calculator?.name.isEmpty == false)
}
