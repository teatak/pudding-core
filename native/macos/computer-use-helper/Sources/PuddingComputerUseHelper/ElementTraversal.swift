// One node budget for both observation and action resolution; no hidden depth cutoff.
enum ElementTraversal {
  static func walk<Node>(root: Node, limit: Int, visit: (Node) -> [Node]) -> Bool {
    precondition(limit > 0)
    var queue = [root]
    var cursor = 0
    var truncated = false
    while cursor < queue.count {
      let children = visit(queue[cursor])
      cursor += 1
      let remaining = limit - queue.count
      if children.count > remaining { truncated = true }
      queue.append(contentsOf: children.prefix(remaining))
    }
    return truncated
  }
}
