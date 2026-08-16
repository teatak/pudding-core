import Testing

@testable import PuddingComputerUseHelper

@Test func secureValuesAreAlwaysRedacted() {
  #expect(ValueSanitizer.string("secret", secure: true) == nil)
  #expect(ValueSanitizer.isTruncated("secret", secure: true) == false)
}

@Test func ordinaryValuesAreBounded() {
  #expect(ValueSanitizer.string("abcdef", secure: false, limit: 3) == "abc")
  #expect(ValueSanitizer.isTruncated("abcdef", secure: false, limit: 3))
  #expect(ValueSanitizer.isTruncated("abc", secure: false, limit: 3) == false)
}

@Test func valuesAreBoundedByUTF8BytesWithoutSplittingCharacters() {
  #expect(ValueSanitizer.bounded("你好a", limit: 6) == "你好")
  #expect(ValueSanitizer.bounded("你好a", limit: 7) == "你好a")
}
