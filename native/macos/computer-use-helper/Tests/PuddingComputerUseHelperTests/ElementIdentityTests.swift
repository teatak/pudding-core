import Testing

@testable import PuddingComputerUseHelper

@Test func elementIdentityIsStableForTheSameElement() {
  let first = ElementIdentity.make(
    windowIndex: 0,
    path: [1, 2],
    role: "AXButton",
    subrole: nil,
    identifier: "save",
    label: "Save",
    frame: FrameSnapshot(x: 10, y: 20, width: 80, height: 30)
  )
  let second = ElementIdentity.make(
    windowIndex: 0,
    path: [1, 2],
    role: "AXButton",
    subrole: nil,
    identifier: "save",
    label: "Save",
    frame: FrameSnapshot(x: 10, y: 20, width: 80, height: 30)
  )

  #expect(first == second)
}

@Test func elementIdentityChangesWithPathOrRole() {
  let base = ElementIdentity.make(
    windowIndex: 0,
    path: [1, 2],
    role: "AXButton",
    subrole: nil,
    identifier: nil,
    label: "Save",
    frame: nil
  )
  let moved = ElementIdentity.make(
    windowIndex: 0,
    path: [1, 3],
    role: "AXButton",
    subrole: nil,
    identifier: nil,
    label: "Save",
    frame: nil
  )
  let changedRole = ElementIdentity.make(
    windowIndex: 0,
    path: [1, 2],
    role: "AXTextField",
    subrole: nil,
    identifier: nil,
    label: "Save",
    frame: nil
  )

  #expect(base != moved)
  #expect(base != changedRole)
}

@Test func visibleRowsUseTheirAbsoluteAccessibilityIndex() {
  let firstViewport = ElementIdentity.visibleRowPathComponents(indices: [0, 1, 2])
  let scrolledViewport = ElementIdentity.visibleRowPathComponents(indices: [100, 101, 102])

  #expect(firstViewport.allSatisfy { $0.stable })
  #expect(scrolledViewport.allSatisfy { $0.stable })
  #expect(firstViewport.map(\.index) == [0, 1, 2])
  #expect(scrolledViewport.map(\.index) == [100, 101, 102])
  #expect(firstViewport.map(\.index) != scrolledViewport.map(\.index))
}

@Test func visibleRowsWithoutUniqueAbsoluteIndicesAreReadOnly() {
  let missing = ElementIdentity.visibleRowPathComponents(indices: [nil, nil])
  let duplicate = ElementIdentity.visibleRowPathComponents(indices: [4, 4])
  let invalid = ElementIdentity.visibleRowPathComponents(indices: [-1, 0])

  #expect(missing == [
    ElementPathComponent(index: 0, stable: false),
    ElementPathComponent(index: 1, stable: false),
  ])
  #expect(duplicate.allSatisfy { !$0.stable })
  #expect(invalid.allSatisfy { !$0.stable })
}
