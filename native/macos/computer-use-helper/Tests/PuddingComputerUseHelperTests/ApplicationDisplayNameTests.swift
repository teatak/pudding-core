import Foundation
import Testing

@testable import PuddingComputerUseHelper

@Test func applicationDisplayNameUsesBundledStringsAndMatchesChineseScripts() throws {
  let fixture = try NameBundleFixture(localizations: ["en", "zh-Hans", "zh-Hant"])
  defer { fixture.remove() }
  try fixture.write(["CFBundleDisplayName": "示例应用"], "Resources/zh-Hans.lproj/InfoPlist.strings")
  try fixture.write(["CFBundleName": "範例應用"], "Resources/zh-Hant.lproj/InfoPlist.strings")
  let bundle = try #require(Bundle(url: fixture.url))
  #expect(fixture.name(bundle, "zh-CN") == "示例应用")
  #expect(fixture.name(bundle, "zh-TW") == "範例應用")
  #expect(fixture.name(bundle, "en") == "Original Brand")
  #expect(fixture.name(bundle, "zh-CN") == "示例应用")
}

@Test func applicationDisplayNameUsesLoctableAndPreservesUntranslatedBrand() throws {
  let fixture = try NameBundleFixture(localizations: ["en", "zh_CN", "zh_TW"])
  defer { fixture.remove() }
  try fixture.write([
    "zh_CN": ["CFBundleDisplayName": "简体名称", "CFBundleName": "不优先"],
    "zh_TW": ["CFBundleName": "繁體名稱"],
    "LocProvenance": ["zh_CN": 1],
  ], "Resources/InfoPlist.loctable")
  let bundle = try #require(Bundle(url: fixture.url))
  #expect(fixture.name(bundle, "zh-CN") == "简体名称")
  #expect(fixture.name(bundle, "zh-TW") == "繁體名稱")
  #expect(fixture.name(bundle, "en") == "Original Brand")
  #expect(fixture.name(bundle, "fr") == "Original Brand")
}

@Test func applicationDisplayNameIgnoresEmptyNamesAndUsesBundleFilenameWhenMissing() throws {
  let fixture = try NameBundleFixture(localizations: ["en", "zh-Hans"])
  defer { fixture.remove() }
  try fixture.write(["CFBundleDisplayName": "  ", "CFBundleName": ""], "Resources/zh-Hans.lproj/InfoPlist.strings")
  let bundle = try #require(Bundle(url: fixture.url))
  #expect(fixture.name(bundle, "zh-CN") == "Original Brand")
  #expect(ApplicationDisplayName.resolve(bundle: nil, url: fixture.url, preferredLanguages: ["zh-CN"]) == "Fixture")
}

private struct NameBundleFixture {
  let root: URL
  let url: URL
  init(localizations: [String]) throws {
    root = FileManager.default.temporaryDirectory.appendingPathComponent("pudding-name-\(UUID().uuidString)")
    url = root.appendingPathComponent("Fixture.app")
    try write([
      "CFBundleIdentifier": "com.example.name.\(UUID().uuidString)",
      "CFBundlePackageType": "APPL", "CFBundleDevelopmentRegion": "en",
      "CFBundleName": "Original", "CFBundleDisplayName": "Original Brand",
      "CFBundleLocalizations": localizations,
    ], "Info.plist")
    try FileManager.default.createDirectory(at: url.appendingPathComponent("Contents/Resources"), withIntermediateDirectories: true)
  }
  func write(_ dictionary: [String: Any], _ relative: String) throws {
    let target = url.appendingPathComponent("Contents/\(relative)")
    try FileManager.default.createDirectory(at: target.deletingLastPathComponent(), withIntermediateDirectories: true)
    try PropertyListSerialization.data(fromPropertyList: dictionary, format: .xml, options: 0).write(to: target)
  }
  func name(_ bundle: Bundle, _ locale: String) -> String {
    ApplicationDisplayName.resolve(bundle: bundle, url: url, preferredLanguages: [locale])
  }
  func remove() { try? FileManager.default.removeItem(at: root) }
}
