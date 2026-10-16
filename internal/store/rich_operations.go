package store

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Rich edits address body blocks, never derived page numbers or plain-text read offsets.
type RichOperation struct {
	Kind      string         `json:"kind"`
	BlockID   string         `json:"block_id"`
	OldText   string         `json:"old_text,omitempty"`
	Text      *string        `json:"text,omitempty"`
	Content   []RichNode     `json:"content,omitempty"`
	Marks     []RichMark     `json:"marks,omitempty"`
	Placement string         `json:"placement,omitempty"`
	NodeType  string         `json:"node_type,omitempty"`
	Attrs     map[string]any `json:"attrs,omitempty"`
}

func richEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func richCopy(body RichDocumentBody) RichDocumentBody {
	raw, _ := json.Marshal(body)
	var out RichDocumentBody
	_ = json.Unmarshal(raw, &out)
	return out
}
func richHasID(kind string) bool {
	switch kind {
	case "paragraph", "heading", "blockquote", "codeBlock", "bulletList", "orderedList", "listItem", "table":
		return true
	}
	return false
}
func richID(n RichNode) string { id, _ := n.Attrs["blockId"].(string); return id }

// Legacy blocks without IDs receive deterministic anchors in read projections.
// Reading never writes; an edit persists these same IDs, not new random IDs.
func AnchorRichBody(body RichDocumentBody) RichDocumentBody {
	out := richCopy(body)
	raw, _ := json.Marshal(body.Body)
	richIDs(&out.Body, DocumentHash(string(raw)), "body", false)
	return out
}
func richIDs(n *RichNode, seed, path string, replace bool) {
	if richHasID(n.Type) && (replace || richID(*n) == "") {
		if n.Attrs == nil {
			n.Attrs = map[string]any{}
		}
		n.Attrs["blockId"] = "block_" + DocumentHash(seed + "/" + path)[:32]
	}
	for i := range n.Content {
		richIDs(&n.Content[i], seed, fmt.Sprintf("%s/%d", path, i), replace)
	}
}
func NewRichBody(nodes []RichNode, seed string) (RichDocumentBody, error) {
	body := EmptyRichDocument()
	if len(nodes) == 0 {
		return body, invalidRich("content must contain at least one block")
	}
	if err := validateGeneratedRichNodes(nodes); err != nil {
		return body, err
	}
	body.Body.Content = nodes
	body = richCopy(body)
	richIDs(&body.Body, seed, "body", true)
	if err := ValidateRichEditBody(body); err != nil {
		return body, err
	}
	return body, nil
}

// Additional shape checks prevent the editor from silently repairing AI output.
func ValidateRichEditBody(body RichDocumentBody) error {
	if err := ValidateRichDocument(body); err != nil {
		return err
	}
	var visit func(RichNode) error
	visit = func(n RichNode) error {
		switch n.Type {
		case "doc", "blockquote", "bulletList", "orderedList", "listItem", "table", "tableRow", "tableCell", "tableHeader":
			if len(n.Content) == 0 {
				return invalidRich("empty container: " + n.Type)
			}
		}
		if n.Type == "listItem" && n.Content[0].Type != "paragraph" {
			return invalidRich("list items must start with a paragraph")
		}
		if n.Type == "codeBlock" {
			for _, c := range n.Content {
				if len(c.Marks) > 0 {
					return invalidRich("code blocks cannot contain marks")
				}
			}
		}
		if n.Type == "table" {
			if err := validateRichTable(n); err != nil {
				return err
			}
		}
		if n.Type != "text" && len(n.Marks) > 0 {
			return invalidRich("marks require a text node")
		}
		for _, c := range n.Content {
			if err := visit(c); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(body.Body)
}

type richLocation struct {
	parent *RichNode
	index  int
}

func findRichBlock(root *RichNode, id string) *richLocation {
	for i := range root.Content {
		if richID(root.Content[i]) == id {
			return &richLocation{root, i}
		}
		if found := findRichBlock(&root.Content[i], id); found != nil {
			return found
		}
	}
	return nil
}

func ApplyRichOperations(body RichDocumentBody, ops []RichOperation, seed string) (RichDocumentBody, error) {
	if len(ops) == 0 || len(ops) > 100 {
		return body, invalidRich("supply 1–100 operations")
	}
	out := AnchorRichBody(body)
	for i, op := range ops {
		if err := validateRichOperation(op); err != nil {
			return body, err
		}
		if err := validateGeneratedRichNodes(op.Content); err != nil {
			return body, err
		}
		if op.BlockID == "" {
			return body, invalidRich("block_id is required")
		}
		loc := findRichBlock(&out.Body, op.BlockID)
		if loc == nil {
			return body, invalidRich("body block not found: " + op.BlockID)
		}
		node := &loc.parent.Content[loc.index]
		switch op.Kind {
		case "replace_text", "format_text":
			if err := editRichText(node, op); err != nil {
				return body, err
			}
		case "set_block":
			if node.Type != "paragraph" && node.Type != "heading" {
				return body, invalidRich("set_block requires a paragraph or heading")
			}
			if op.NodeType != "" {
				if op.NodeType != "paragraph" && op.NodeType != "heading" {
					return body, invalidRich("unsupported block type")
				}
				node.Type = op.NodeType
			}
			if node.Attrs == nil {
				node.Attrs = map[string]any{}
			}
			if node.Type == "paragraph" {
				delete(node.Attrs, "level")
			}
			for key, value := range op.Attrs {
				if key != "textAlign" && key != "level" || key == "level" && node.Type != "heading" {
					return body, invalidRich("unsupported block format")
				}
				node.Attrs[key] = value
			}
			if node.Type == "heading" && node.Attrs["level"] == nil {
				node.Attrs["level"] = 1
			}
		case "insert_blocks", "replace_block", "delete_block":
			if op.Kind == "delete_block" && len(op.Content) > 0 || op.Kind != "delete_block" && len(op.Content) == 0 {
				return body, invalidRich("invalid block content")
			}
			nodes := op.Content
			raw, _ := json.Marshal(nodes)
			_ = json.Unmarshal(raw, &nodes)
			for j := range nodes {
				richIDs(&nodes[j], seed, fmt.Sprintf("%d/%d", i, j), true)
			}
			start, end := loc.index, loc.index+1
			if op.Kind == "insert_blocks" {
				if op.Placement != "before" && op.Placement != "after" {
					return body, invalidRich("placement must be before or after")
				}
				if op.Placement == "after" {
					start++
				}
				end = start
			} else if op.Kind == "replace_block" && richHasID(nodes[0].Type) {
				nodes[0].Attrs["blockId"] = op.BlockID
			}
			next := append([]RichNode{}, loc.parent.Content[:start]...)
			next = append(next, nodes...)
			next = append(next, loc.parent.Content[end:]...)
			loc.parent.Content = next
		default:
			return body, invalidRich("unsupported rich operation: " + op.Kind)
		}
	}
	if err := ValidateRichEditBody(out); err != nil {
		return body, err
	}
	return out, nil
}

// Validate supplied attributes against their node type; otherwise Tiptap would
// silently discard unsupported formatting after the next human keystroke.
func validateGeneratedRichNodes(nodes []RichNode) error {
	for _, n := range nodes {
		for key := range n.Attrs {
			allowed := key == "blockId" && richHasID(n.Type)
			switch n.Type {
			case "paragraph", "heading":
				allowed = allowed || key == "textAlign" || key == "level" && n.Type == "heading"
			case "orderedList":
				allowed = allowed || key == "start" || key == "type"
			case "codeBlock":
				allowed = allowed || key == "language"
			case "tableCell", "tableHeader":
				allowed = key == "colspan" || key == "rowspan" || key == "colwidth"
			case "image":
				allowed = key == "assetId" || key == "alt" || key == "width"
			}
			if !allowed {
				return invalidRich("attribute " + key + " is not supported on " + n.Type)
			}
		}
		if err := validateGeneratedRichNodes(n.Content); err != nil {
			return err
		}
	}
	return nil
}

func editRichText(node *RichNode, op RichOperation) error {
	if node.Type != "paragraph" && node.Type != "heading" && node.Type != "codeBlock" {
		return invalidRich("text operations require a paragraph, heading or code block")
	}
	// Object boundaries cannot be crossed by a text operation.
	text := ""
	for _, n := range node.Content {
		if n.Type == "text" {
			text += n.Text
		} else {
			text += "\x00"
		}
	}
	if op.OldText == "" || strings.ContainsRune(op.OldText, 0) || strings.Count(text, op.OldText) != 1 {
		return invalidRich("old_text must match exactly once within the block")
	}
	from := strings.Index(text, op.OldText)
	to := from + len(op.OldText)
	var before, selected, after []RichNode
	at := 0
	for _, n := range node.Content {
		length := len(n.Text)
		if n.Type != "text" {
			length = 1
		}
		end := at + length
		if end <= from {
			before = append(before, n)
		} else if at >= to {
			after = append(after, n)
		} else {
			if n.Type != "text" {
				return invalidRich("cannot replace an inline object as text")
			}
			if at < from {
				p := n
				p.Text = n.Text[:from-at]
				before = append(before, p)
			}
			mid := n
			mid.Text = n.Text[max(0, from-at):min(length, to-at)]
			selected = append(selected, mid)
			if end > to {
				p := n
				p.Text = n.Text[to-at:]
				after = append(after, p)
			}
		}
		at = end
	}
	var replacement []RichNode
	if op.Kind == "format_text" {
		if node.Type == "codeBlock" && len(op.Marks) > 0 {
			return invalidRich("code blocks cannot contain formatted text")
		}
		replacement = selected
		for i := range replacement {
			replacement[i].Marks = op.Marks
		}
	} else {
		if (op.Text == nil) == (len(op.Content) == 0) {
			return invalidRich("replace_text requires text or inline content")
		}
		if op.Text != nil {
			for _, n := range selected {
				if !reflect.DeepEqual(n.Marks, selected[0].Marks) {
					return invalidRich("mixed formatting: supply explicit inline content instead of text")
				}
			}
			if *op.Text != "" {
				replacement = []RichNode{{Type: "text", Text: *op.Text, Marks: selected[0].Marks}}
			}
		} else {
			for _, n := range op.Content {
				if n.Type != "text" && n.Type != "hardBreak" {
					return invalidRich("replacement inline content supports text and hardBreak")
				}
			}
			replacement = op.Content
		}
	}
	node.Content = append(append(before, replacement...), after...)
	return nil
}

func validateRichOperation(op RichOperation) error {
	allowed := map[string]bool{"kind": true, "block_id": true}
	fields := map[string][]string{
		"replace_text": {"old_text", "text", "content"}, "format_text": {"old_text", "marks"},
		"set_block": {"node_type", "attrs"}, "insert_blocks": {"placement", "content"}, "replace_block": {"content"}, "delete_block": {},
	}
	list, ok := fields[op.Kind]
	if !ok {
		return invalidRich("unknown operation")
	}
	for _, k := range list {
		allowed[k] = true
	}
	raw, _ := json.Marshal(op)
	var values map[string]any
	_ = json.Unmarshal(raw, &values)
	for k := range values {
		if !allowed[k] {
			return invalidRich("unexpected operation field: " + k)
		}
	}
	if op.Kind == "format_text" && op.Marks == nil {
		return invalidRich("marks is required; use [] to remove formatting")
	}
	if op.Kind == "set_block" && op.NodeType == "" && len(op.Attrs) == 0 {
		return invalidRich("set_block requires a type or attributes")
	}
	return nil
}

func validateRichTable(table RichNode) error {
	number := func(n RichNode, key string) int {
		v := n.Attrs[key]
		if v == nil {
			return 1
		}
		switch x := v.(type) {
		case float64:
			return int(x)
		case int:
			return x
		}
		return 1
	}
	width := 0
	for _, cell := range table.Content[0].Content {
		width += number(cell, "colspan")
	}
	if width < 1 || width > 1000 {
		return invalidRich("invalid table width")
	}
	occupied := make([]int, width)
	for rowIndex, row := range table.Content {
		col := 0
		for _, cell := range row.Content {
			for col < width && occupied[col] > rowIndex {
				col++
			}
			span, rows := number(cell, "colspan"), number(cell, "rowspan")
			if col+span > width || rowIndex+rows > len(table.Content) {
				return invalidRich("invalid table spans")
			}
			for j := col; j < col+span; j++ {
				if occupied[j] > rowIndex {
					return invalidRich("overlapping table cells")
				}
				occupied[j] = rowIndex + rows
			}
			col += span
		}
		for _, until := range occupied {
			if until <= rowIndex {
				return invalidRich("table rows must fill the same columns")
			}
		}
	}
	return nil
}
