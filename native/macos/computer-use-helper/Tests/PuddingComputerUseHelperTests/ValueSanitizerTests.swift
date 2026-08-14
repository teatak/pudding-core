import Testing

@testable import PuddingComputerUseHelper

@Test func secureValuesAreAlwaysRedacted() {
  #expect(ValueSanitizer.string("secret", secure: true) == nil)
}

@Test func ordinaryValuesAreBounded() {
  #expect(ValueSanitizer.string("abcdef", secure: false, limit: 3) == "abc")
}
