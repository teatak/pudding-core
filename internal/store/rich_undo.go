package store

import (
	"encoding/json"
	"sort"
)

// Selective inverse: retain later edits outside the fields/blocks changed by AI.
// Ambiguous structural changes or overlapping text edits fail without writing.
func UndoRichDocument(beforeRaw, afterRaw string, current RichDocumentBody) (RichDocumentBody, error) {
	var before, after RichDocumentBody
	if err := json.Unmarshal([]byte(beforeRaw), &before); err != nil {
		return current, err
	}
	if err := json.Unmarshal([]byte(afterRaw), &after); err != nil {
		return current, err
	}
	before = AnchorRichBody(before)
	after = AnchorRichBody(after)
	out := AnchorRichBody(current)
	// Only body operations are available to AI in this phase.
	b, a := before, after
	b.Body = RichNode{}
	a.Body = RichNode{}
	if !richEqual(b, a) {
		return current, ErrUndoConflict
	}
	node, err := undoRichNode(before.Body, after.Body, out.Body)
	if err != nil {
		return current, err
	}
	out.Body = node
	if richEqual(out, current) {
		return current, ErrUndoConflict
	}
	return out, ValidateRichEditBody(out)
}
func undoRichNode(before, after, current RichNode) (RichNode, error) {
	if richNodesEquivalent(before, after) {
		return current, nil
	}
	if richNodesEquivalent(after, current) {
		return before, nil
	}
	out := current
	if before.Type != after.Type {
		if current.Type != after.Type {
			return current, ErrUndoConflict
		}
		out.Type = before.Type
	}
	if before.Text != after.Text {
		if current.Text != after.Text {
			return current, ErrUndoConflict
		}
		out.Text = before.Text
	}
	bn, an, cn := normalizedRichNode(before), normalizedRichNode(after), normalizedRichNode(current)
	if !richEqual(bn.Attrs, an.Attrs) {
		if !richEqual(cn.Attrs, an.Attrs) {
			return current, ErrUndoConflict
		}
		out.Attrs = before.Attrs
	}
	if !richEqual(bn.Marks, an.Marks) {
		if !richEqual(cn.Marks, an.Marks) {
			return current, ErrUndoConflict
		}
		out.Marks = before.Marks
	}
	if !richEqual(bn.Content, an.Content) {
		if current.Type != after.Type {
			return current, ErrUndoConflict
		}
		if after.Type == "paragraph" || after.Type == "heading" || after.Type == "codeBlock" {
			if !richEqual(cn.Content, an.Content) {
				return current, ErrUndoConflict
			}
			out.Content = before.Content
		} else {
			children, err := undoRichChildren(before.Content, after.Content, current.Content)
			if err != nil {
				return current, err
			}
			out.Content = children
		}
	}
	return out, nil
}
func keyedRich(nodes []RichNode) (map[string]RichNode, bool) {
	result := map[string]RichNode{}
	for _, n := range nodes {
		id := richID(n)
		if id == "" {
			return nil, false
		}
		if _, ok := result[id]; ok {
			return nil, false
		}
		result[id] = n
	}
	return result, true
}
func undoRichChildren(before, after, current []RichNode) ([]RichNode, error) {
	if richChildrenEquivalent(before, after) {
		return current, nil
	}
	if richChildrenEquivalent(after, current) {
		return before, nil
	}
	b, bk := keyedRich(before)
	a, ak := keyedRich(after)
	c, ck := keyedRich(current)
	if !bk || !ak || !ck {
		if len(before) != len(after) || len(after) != len(current) {
			return nil, ErrUndoConflict
		}
		out := append([]RichNode{}, current...)
		for i := range out {
			n, err := undoRichNode(before[i], after[i], out[i])
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	}
	// Shared siblings must keep their order. Reordering is deliberately conservative.
	var oldOrder, newOrder []string
	for _, n := range before {
		if _, ok := a[richID(n)]; ok {
			oldOrder = append(oldOrder, richID(n))
		}
	}
	for _, n := range after {
		if _, ok := b[richID(n)]; ok {
			newOrder = append(newOrder, richID(n))
		}
	}
	if !richEqual(oldOrder, newOrder) {
		return nil, ErrUndoConflict
	}
	for id, n := range a {
		latest, present := c[id]
		old, existed := b[id]
		if !existed {
			if !present || !richNodesEquivalent(n, latest) {
				return nil, ErrUndoConflict
			}
		} else if !richNodesEquivalent(old, n) && !present {
			return nil, ErrUndoConflict
		}
	}
	out := []RichNode{}
	for _, n := range current {
		id := richID(n)
		an, inAfter := a[id]
		bn, inBefore := b[id]
		if inAfter && !inBefore {
			continue
		}
		if inBefore && !inAfter {
			return nil, ErrUndoConflict
		}
		if inBefore && inAfter {
			updated, err := undoRichNode(bn, an, n)
			if err != nil {
				return nil, err
			}
			n = updated
		}
		out = append(out, n)
	}
	for i, n := range before {
		if _, ok := a[richID(n)]; ok {
			continue
		}
		// Reinsert immediately before the nearest surviving next sibling, otherwise
		// after the previous one. If both anchors disappeared, do not guess.
		index := -1
		for j := i + 1; j < len(before) && index < 0; j++ {
			for k, x := range out {
				if richID(x) == richID(before[j]) {
					index = k
					break
				}
			}
		}
		if index < 0 {
			for j := i - 1; j >= 0 && index < 0; j-- {
				for k, x := range out {
					if richID(x) == richID(before[j]) {
						index = k + 1
						break
					}
				}
			}
		}
		if index < 0 {
			if len(out) > 0 {
				return nil, ErrUndoConflict
			}
			index = 0
		}
		next := append([]RichNode{}, out[:index]...)
		next = append(next, n)
		out = append(next, out[index:]...)
	}
	return out, nil
}

func richNodesEquivalent(a, b RichNode) bool {
	return richEqual(normalizedRichNode(a), normalizedRichNode(b))
}
func richChildrenEquivalent(a, b []RichNode) bool {
	return richNodesEquivalent(RichNode{Content: a}, RichNode{Content: b})
}

// Tiptap fills defaults, orders marks and joins adjacent text runs on a human
// save. Compare these equivalent representations without rewriting storage or
// treating actual text/style changes as safe to undo.
func normalizedRichNode(n RichNode) RichNode {
	out := n
	out.Attrs = nil
	for k, v := range n.Attrs {
		if v == nil || k == "textAlign" && v == "left" || k == "level" && n.Type == "heading" && richEqual(v, 1) || k == "start" && n.Type == "orderedList" && richEqual(v, 1) || (k == "rowspan" || k == "colspan") && richEqual(v, 1) {
			continue
		}
		if out.Attrs == nil {
			out.Attrs = map[string]any{}
		}
		out.Attrs[k] = v
	}
	out.Marks = nil
	for _, m := range n.Marks {
		mark := RichMark{Type: m.Type}
		for k, v := range m.Attrs {
			if v == nil || m.Type == "link" && (k == "target" && v == "_blank" || k == "rel" && v == "noopener noreferrer nofollow") {
				continue
			}
			if mark.Attrs == nil {
				mark.Attrs = map[string]any{}
			}
			mark.Attrs[k] = v
		}
		out.Marks = append(out.Marks, mark)
	}
	sort.Slice(out.Marks, func(i, j int) bool { return out.Marks[i].Type < out.Marks[j].Type })
	out.Content = nil
	for _, c := range n.Content {
		child := normalizedRichNode(c)
		if l := len(out.Content); l > 0 && child.Type == "text" && out.Content[l-1].Type == "text" && richEqual(child.Marks, out.Content[l-1].Marks) && richEqual(child.Attrs, out.Content[l-1].Attrs) {
			out.Content[l-1].Text += child.Text
		} else {
			out.Content = append(out.Content, child)
		}
	}
	return out
}
