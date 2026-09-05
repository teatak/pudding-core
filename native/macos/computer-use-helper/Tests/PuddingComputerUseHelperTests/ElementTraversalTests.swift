import Testing

@testable import PuddingComputerUseHelper

@Test func traversalReachesControlsDeeperThanEightLevels() {
  var visited: [Int] = []
  let truncated = ElementTraversal.walk(root: 0, limit: 100) { node in
    visited.append(node)
    return node < 20 ? [node + 1] : []
  }
  #expect(visited == Array(0...20))
  #expect(!truncated)
}

@Test func traversalReportsTheNodeBudgetInsteadOfSilentlyDroppingChildren() {
  var visited: [Int] = []
  let truncated = ElementTraversal.walk(root: 0, limit: 10) { node in
    visited.append(node)
    return [node + 1]
  }
  #expect(visited == Array(0..<10))
  #expect(truncated)
}

@Test func traversalDoesNotTruncateAnExactlyFullTree() {
  var visited: [Int] = []
  let truncated = ElementTraversal.walk(root: 0, limit: 3) { node in
    visited.append(node)
    return node == 0 ? [1, 2] : []
  }
  #expect(visited == [0, 1, 2])
  #expect(!truncated)
}

@Test func traversalBoundsWideTreesAndKeepsBreadthFirstOrder() {
  var visited: [Int] = []
  let truncated = ElementTraversal.walk(root: 0, limit: 4) { node in
    visited.append(node)
    return node == 0 ? [1, 2] : Array(10...20)
  }
  #expect(visited == [0, 1, 2, 10])
  #expect(truncated)
}
