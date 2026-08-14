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
