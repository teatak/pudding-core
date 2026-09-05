import Testing

@testable import PuddingComputerUseHelper

@Test func elementIdentityIsStableForTheSameElement() {
  let first = ElementIdentity.make(
    windowIndex: 0,
    path: [1, 2],
    role: "AXButton",
    subrole: nil,
    identifier: "save",
    label: "Save"
  )
  let second = ElementIdentity.make(
    windowIndex: 0,
    path: [9, 7],
    role: "AXButton",
    subrole: nil,
    identifier: "save",
    label: "Save changes"
  )

  #expect(first == second)
}

@Test func labeledElementIdentitySurvivesTreeRebuild() {
  let base = ElementIdentity.make(
    windowIndex: 0,
    path: [1, 2],
    role: "AXButton",
    subrole: nil,
    identifier: nil,
    label: "Save"
  )
  let rebuilt = ElementIdentity.make(
    windowIndex: 0,
    path: [8, 4, 2],
    role: "AXButton",
    subrole: nil,
    identifier: nil,
    label: "Save"
  )
  let changedRole = ElementIdentity.make(
    windowIndex: 0,
    path: [1, 2],
    role: "AXTextField",
    subrole: nil,
    identifier: nil,
    label: "Save"
  )

  #expect(base == rebuilt)
  #expect(base != changedRole)
}

@Test func unlabeledElementIdentityUsesStructuralPath() {
  let base = ElementIdentity.make(
    windowIndex: 0,
    path: [1, 2],
    role: "AXGroup",
    subrole: nil,
    identifier: nil,
    label: nil
  )
  let moved = ElementIdentity.make(
    windowIndex: 0,
    path: [1, 3],
    role: "AXGroup",
    subrole: nil,
    identifier: nil,
    label: nil
  )

  #expect(base != moved)
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

@Test func identicalLabelsAndIdentifiersAreScopedToTheirParents() {
  for identifier in [nil, "remove"] as [String?] {
    let first = ElementIdentity.make(
      windowIndex: 0, path: [1, 0], role: "AXButton", subrole: nil,
      identifier: identifier, label: "Remove", parentID: "first-row"
    )
    let second = ElementIdentity.make(
      windowIndex: 0, path: [2, 0], role: "AXButton", subrole: nil,
      identifier: identifier, label: "Remove", parentID: "second-row"
    )
    #expect(first != second)
  }
}

@Test func scopedIdentitySurvivesARebuiltNamedParent() {
  func childID(parentPath: [Int], childPath: [Int]) -> String {
    let parentID = ElementIdentity.make(
      windowIndex: 0, path: parentPath, role: "AXGroup", subrole: nil,
      identifier: "toolbar", label: nil
    )
    return ElementIdentity.make(
      windowIndex: 0, path: childPath, role: "AXButton", subrole: nil,
      identifier: nil, label: "Save", parentID: parentID
    )
  }
  #expect(childID(parentPath: [1], childPath: [1, 2]) ==
    childID(parentPath: [9], childPath: [9, 4]))
}

@Test func changingWindowTitleDoesNotRenameDescendants() {
  func childID(windowTitle: String) -> String {
    let windowID = ElementIdentity.make(
      windowIndex: 0, path: [], role: "AXWindow", subrole: nil,
      identifier: nil, label: windowTitle
    )
    return ElementIdentity.make(
      windowIndex: 0, path: [0], role: "AXButton", subrole: nil,
      identifier: "4", label: "4", parentID: windowID
    )
  }
  #expect(childID(windowTitle: "0") == childID(windowTitle: "4+4"))
}

@Test func equalRowLabelsKeepTheirAbsoluteRowIdentity() {
  let first = ElementIdentity.make(
    windowIndex: 0, path: [0, 1], role: "AXRow", subrole: nil,
    identifier: nil, label: "Unread", parentID: "table"
  )
  let second = ElementIdentity.make(
    windowIndex: 0, path: [0, 100], role: "AXRow", subrole: nil,
    identifier: nil, label: "Unread", parentID: "table"
  )
  #expect(first != second)
}

@Test func indistinguishableSiblingsRemainAmbiguous() {
  let first = ElementIdentity.make(
    windowIndex: 0, path: [0, 1], role: "AXButton", subrole: nil,
    identifier: nil, label: "Save", parentID: "toolbar"
  )
  let second = ElementIdentity.make(
    windowIndex: 0, path: [0, 2], role: "AXButton", subrole: nil,
    identifier: nil, label: "Save", parentID: "toolbar"
  )
  #expect(first == second)
}
