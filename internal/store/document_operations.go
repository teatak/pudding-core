package store

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Document edits address body blocks, never derived page numbers or plain-text read offsets.
type DocumentOperation struct {
	Kind      string         `json:"kind"`
	BlockID   string         `json:"block_id"`
	OldText   string         `json:"old_text,omitempty"`
	Text      *string        `json:"text,omitempty"`
	Content   []DocumentNode `json:"content,omitempty"`
	Marks     []DocumentMark `json:"marks,omitempty"`
	Placement string         `json:"placement,omitempty"`
	NodeType  string         `json:"node_type,omitempty"`
	Attrs     map[string]any `json:"attrs,omitempty"`
}

func documentEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func documentCopy(body DocumentBody) DocumentBody {
	raw, _ := json.Marshal(body)
	var out DocumentBody
	_ = json.Unmarshal(raw, &out)
	return out
}
func documentHasID(kind string) bool {
	switch kind {
	case "paragraph", "heading", "blockquote", "codeBlock", "bulletList", "orderedList", "listItem", "table":
		return true
	}
	return false
}
func documentID(n DocumentNode) string { id, _ := n.Attrs["blockId"].(string); return id }

// Legacy blocks without IDs receive deterministic anchors in read projections.
// Reading never writes; an edit persists these same IDs, not new random IDs.
func AnchorDocumentBody(body DocumentBody) DocumentBody {
	out := documentCopy(body)
	raw, _ := json.Marshal(body.Body)
	documentIDs(&out.Body, ContentHash(string(raw)), "body", false)
	return out
}
func documentIDs(n *DocumentNode, seed, path string, replace bool) {
	if documentHasID(n.Type) && (replace || documentID(*n) == "") {
		if n.Attrs == nil {
			n.Attrs = map[string]any{}
		}
		n.Attrs["blockId"] = "block_" + ContentHash(seed + "/" + path)[:32]
	}
	for i := range n.Content {
		documentIDs(&n.Content[i], seed, fmt.Sprintf("%s/%d", path, i), replace)
	}
}
func NewDocumentBody(nodes []DocumentNode, seed string) (DocumentBody, error) {
	body := EmptyDocument()
	if len(nodes) == 0 {
		return body, invalidDocument("content must contain at least one block")
	}
	if err := validateGeneratedDocumentNodes(nodes); err != nil {
		return body, err
	}
	body.Body.Content = nodes
	body = documentCopy(body)
	documentIDs(&body.Body, seed, "body", true)
	if err := ValidateDocumentEditBody(body); err != nil {
		return body, err
	}
	return body, nil
}

// Additional shape checks prevent the editor from silently repairing AI output.
func ValidateDocumentEditBody(body DocumentBody) error {
	if err := ValidateDocument(body); err != nil {
		return err
	}
	var visit func(DocumentNode) error
	visit = func(n DocumentNode) error {
		switch n.Type {
		case "doc", "blockquote", "bulletList", "orderedList", "listItem", "table", "tableRow", "tableCell", "tableHeader":
			if len(n.Content) == 0 {
				return invalidDocument("empty container: " + n.Type)
			}
		}
		if n.Type == "listItem" && n.Content[0].Type != "paragraph" {
			return invalidDocument("list items must start with a paragraph")
		}
		if n.Type == "codeBlock" {
			for _, c := range n.Content {
				if len(c.Marks) > 0 {
					return invalidDocument("code blocks cannot contain marks")
				}
			}
		}
		if n.Type == "table" {
			if err := validateDocumentTable(n); err != nil {
				return err
			}
		}
		if n.Type != "text" && len(n.Marks) > 0 {
			return invalidDocument("marks require a text node")
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

type documentLocation struct {
	parent *DocumentNode
	index  int
}

func findDocumentBlock(root *DocumentNode, id string) *documentLocation {
	for i := range root.Content {
		if documentID(root.Content[i]) == id {
			return &documentLocation{root, i}
		}
		if found := findDocumentBlock(&root.Content[i], id); found != nil {
			return found
		}
	}
	return nil
}

func ApplyDocumentOperations(body DocumentBody, ops []DocumentOperation, seed string) (DocumentBody, error) {
	if len(ops) == 0 || len(ops) > 100 {
		return body, invalidDocument("supply 1–100 operations")
	}
	out := AnchorDocumentBody(body)
	for i, op := range ops {
		if err := validateDocumentOperation(op); err != nil {
			return body, err
		}
		if err := validateGeneratedDocumentNodes(op.Content); err != nil {
			return body, err
		}
		if op.BlockID == "" {
			return body, invalidDocument("block_id is required")
		}
		loc := findDocumentBlock(&out.Body, op.BlockID)
		if loc == nil {
			return body, invalidDocument("body block not found: " + op.BlockID)
		}
		node := &loc.parent.Content[loc.index]
		switch op.Kind {
		case "replace_text", "format_text":
			if err := editDocumentText(node, op); err != nil {
				return body, err
			}
		case "set_block":
			if node.Type != "paragraph" && node.Type != "heading" {
				return body, invalidDocument("set_block requires a paragraph or heading")
			}
			if op.NodeType != "" {
				if op.NodeType != "paragraph" && op.NodeType != "heading" {
					return body, invalidDocument("unsupported block type")
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
					return body, invalidDocument("unsupported block format")
				}
				node.Attrs[key] = value
			}
			if node.Type == "heading" && node.Attrs["level"] == nil {
				node.Attrs["level"] = 1
			}
		case "insert_blocks", "replace_block", "delete_block":
			if op.Kind == "delete_block" && len(op.Content) > 0 || op.Kind != "delete_block" && len(op.Content) == 0 {
				return body, invalidDocument("invalid block content")
			}
			nodes := op.Content
			raw, _ := json.Marshal(nodes)
			_ = json.Unmarshal(raw, &nodes)
			for j := range nodes {
				documentIDs(&nodes[j], seed, fmt.Sprintf("%d/%d", i, j), true)
			}
			start, end := loc.index, loc.index+1
			if op.Kind == "insert_blocks" {
				if op.Placement != "before" && op.Placement != "after" {
					return body, invalidDocument("placement must be before or after")
				}
				if op.Placement == "after" {
					start++
				}
				end = start
			} else if op.Kind == "replace_block" && documentHasID(nodes[0].Type) {
				nodes[0].Attrs["blockId"] = op.BlockID
			}
			next := append([]DocumentNode{}, loc.parent.Content[:start]...)
			next = append(next, nodes...)
			next = append(next, loc.parent.Content[end:]...)
			loc.parent.Content = next
		default:
			return body, invalidDocument("unsupported rich operation: " + op.Kind)
		}
	}
	if err := ValidateDocumentEditBody(out); err != nil {
		return body, err
	}
	return out, nil
}

// Validate supplied attributes against their node type; otherwise Tiptap would
// silently discard unsupported formatting after the next human keystroke.
func validateGeneratedDocumentNodes(nodes []DocumentNode) error {
	for _, n := range nodes {
		for key := range n.Attrs {
			allowed := key == "blockId" && documentHasID(n.Type)
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
				return invalidDocument("attribute " + key + " is not supported on " + n.Type)
			}
		}
		if err := validateGeneratedDocumentNodes(n.Content); err != nil {
			return err
		}
	}
	return nil
}

func editDocumentText(node *DocumentNode, op DocumentOperation) error {
	if node.Type != "paragraph" && node.Type != "heading" && node.Type != "codeBlock" {
		return invalidDocument("text operations require a paragraph, heading or code block")
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
		return invalidDocument("old_text must match exactly once within the block")
	}
	from := strings.Index(text, op.OldText)
	to := from + len(op.OldText)
	var before, selected, after []DocumentNode
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
				return invalidDocument("cannot replace an inline object as text")
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
	var replacement []DocumentNode
	if op.Kind == "format_text" {
		if node.Type == "codeBlock" && len(op.Marks) > 0 {
			return invalidDocument("code blocks cannot contain formatted text")
		}
		replacement = selected
		for i := range replacement {
			replacement[i].Marks = op.Marks
		}
	} else {
		if (op.Text == nil) == (len(op.Content) == 0) {
			return invalidDocument("replace_text requires text or inline content")
		}
		if op.Text != nil {
			for _, n := range selected {
				if !reflect.DeepEqual(n.Marks, selected[0].Marks) {
					return invalidDocument("mixed formatting: supply explicit inline content instead of text")
				}
			}
			if *op.Text != "" {
				replacement = []DocumentNode{{Type: "text", Text: *op.Text, Marks: selected[0].Marks}}
			}
		} else {
			for _, n := range op.Content {
				if n.Type != "text" && n.Type != "hardBreak" {
					return invalidDocument("replacement inline content supports text and hardBreak")
				}
			}
			replacement = op.Content
		}
	}
	node.Content = append(append(before, replacement...), after...)
	return nil
}

func validateDocumentOperation(op DocumentOperation) error {
	allowed := map[string]bool{"kind": true, "block_id": true}
	fields := map[string][]string{
		"replace_text": {"old_text", "text", "content"}, "format_text": {"old_text", "marks"},
		"set_block": {"node_type", "attrs"}, "insert_blocks": {"placement", "content"}, "replace_block": {"content"}, "delete_block": {},
	}
	list, ok := fields[op.Kind]
	if !ok {
		return invalidDocument("unknown operation")
	}
	for _, k := range list {
		allowed[k] = true
	}
	raw, _ := json.Marshal(op)
	var values map[string]any
	_ = json.Unmarshal(raw, &values)
	for k := range values {
		if !allowed[k] {
			return invalidDocument("unexpected operation field: " + k)
		}
	}
	if op.Kind == "format_text" && op.Marks == nil {
		return invalidDocument("marks is required; use [] to remove formatting")
	}
	if op.Kind == "set_block" && op.NodeType == "" && len(op.Attrs) == 0 {
		return invalidDocument("set_block requires a type or attributes")
	}
	return nil
}

func validateDocumentTable(table DocumentNode) error {
	number := func(n DocumentNode, key string) int {
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
		return invalidDocument("invalid table width")
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
				return invalidDocument("invalid table spans")
			}
			for j := col; j < col+span; j++ {
				if occupied[j] > rowIndex {
					return invalidDocument("overlapping table cells")
				}
				occupied[j] = rowIndex + rows
			}
			col += span
		}
		for _, until := range occupied {
			if until <= rowIndex {
				return invalidDocument("table rows must fill the same columns")
			}
		}
	}
	return nil
}
