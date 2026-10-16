package store

import (
	"encoding/json"
	"errors"
	"testing"
)

func rp(id, text string) RichNode {
	return RichNode{Type: "paragraph", Attrs: map[string]any{"blockId": id}, Content: []RichNode{{Type: "text", Text: text}}}
}
func rbody(nodes ...RichNode) RichDocumentBody {
	b := EmptyRichDocument()
	b.Body.Content = nodes
	return b
}
func rstr(s string) *string          { return &s }
func rraw(b RichDocumentBody) string { raw, _ := json.Marshal(b); return string(raw) }

func TestRichOperationsAtomicFormattingAndObjects(t *testing.T) {
	b := rbody(rp("a", "开头😀需要改写结尾"), rp("b", "人工段落"))
	b.Body.Content[0].Content[0].Marks = []RichMark{{Type: "bold"}}
	b.Body.Content[0].Attrs["textAlign"] = "center"
	original := rraw(b)
	out, err := ApplyRichOperations(b, []RichOperation{{Kind: "replace_text", BlockID: "a", OldText: "😀需要改写", Text: rstr("😀已润色")}, {Kind: "format_text", BlockID: "b", OldText: "人工", Marks: []RichMark{{Type: "italic"}}}}, "request")
	if err != nil {
		t.Fatal(err)
	}
	if rraw(b) != original {
		t.Fatal("mutated input")
	}
	if out.Body.Content[0].Content[1].Text != "😀已润色" || out.Body.Content[0].Content[1].Marks[0].Type != "bold" || out.Body.Content[0].Attrs["textAlign"] != "center" {
		t.Fatal(out)
	}
	if _, err = ApplyRichOperations(b, []RichOperation{{Kind: "replace_text", BlockID: "a", OldText: "需要改写", Text: rstr("修改")}, {Kind: "delete_block", BlockID: "missing"}}, "bad"); err == nil {
		t.Fatal("partial invalid batch accepted")
	}
	if rraw(b) != original {
		t.Fatal("failed batch mutated input")
	}
	mixed := rbody(rp("a", "A"))
	mixed.Body.Content[0].Content = append(mixed.Body.Content[0].Content, RichNode{Type: "text", Text: "B", Marks: []RichMark{{Type: "bold"}}})
	if _, err = ApplyRichOperations(mixed, []RichOperation{{Kind: "replace_text", BlockID: "a", OldText: "AB", Text: rstr("C")}}, "mixed"); err == nil {
		t.Fatal("mixed marks silently lost")
	}
	if _, err = ApplyRichOperations(mixed, []RichOperation{{Kind: "replace_text", BlockID: "a", OldText: "AB", Content: []RichNode{{Type: "text", Text: "C", Marks: []RichMark{{Type: "bold"}}}}}}, "mixed"); err != nil {
		t.Fatal(err)
	}
	for _, ops := range [][]RichOperation{
		{{Kind: "replace_text", BlockID: "a", OldText: "A", Content: []RichNode{{Type: "paragraph"}}}},
		{{Kind: "set_block", BlockID: "a", Attrs: map[string]any{"blockId": "b"}}},
		{{Kind: "format_text", BlockID: "a", OldText: "A", Marks: []RichMark{{Type: "link", Attrs: map[string]any{"href": "javascript:alert(1)"}}}}},
		{{Kind: "delete_block", BlockID: "a"}},
	} {
		if _, err = ApplyRichOperations(mixed, ops, "invalid"); err == nil {
			t.Fatal("invalid edit accepted", ops)
		}
	}
}
func TestRichOperationsStructureAndLegacyAnchors(t *testing.T) {
	b := rbody(RichNode{Type: "paragraph", Content: []RichNode{{Type: "text", Text: "legacy"}}}, rp("b", "keep"))
	a := AnchorRichBody(b)
	id := richID(a.Body.Content[0])
	if id == "" || id != richID(AnchorRichBody(b).Body.Content[0]) || richID(b.Body.Content[0]) != "" {
		t.Fatal("unstable/mutating read anchor")
	}
	ops := []RichOperation{{Kind: "insert_blocks", BlockID: id, Placement: "after", Content: []RichNode{rp("b", "new")}}, {Kind: "set_block", BlockID: id, NodeType: "heading", Attrs: map[string]any{"level": 2}}, {Kind: "replace_block", BlockID: "b", Content: []RichNode{rp("ignored", "replacement")}}}
	out, err := ApplyRichOperations(b, ops, "structure")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Body.Content) != 3 || richID(out.Body.Content[1]) == "b" || richID(out.Body.Content[2]) != "b" || out.Body.Content[0].Type != "heading" {
		t.Fatal(out)
	}
	retry, err := ApplyRichOperations(b, ops, "structure")
	if err != nil || !richEqual(out, retry) {
		t.Fatal("nondeterministic retry", err)
	}
}
func TestRichUndoPreservesLaterContentAndRejectsOverlap(t *testing.T) {
	before := rbody(rp("a", "old"), rp("b", "human"), rp("c", "tail"))
	after, err := ApplyRichOperations(before, []RichOperation{{Kind: "replace_text", BlockID: "a", OldText: "old", Text: rstr("AI")}, {Kind: "insert_blocks", BlockID: "a", Placement: "after", Content: []RichNode{rp("", "inserted")}}, {Kind: "delete_block", BlockID: "c"}}, "ai")
	if err != nil {
		t.Fatal(err)
	}
	current := richCopy(after)
	current.Body.Content[2].Content[0].Text = "new human text"
	current.Page.Left = 1200
	undone, err := UndoRichDocument(rraw(before), rraw(after), current)
	if err != nil {
		t.Fatal(err)
	}
	if len(undone.Body.Content) != 3 || undone.Body.Content[0].Content[0].Text != "old" || undone.Body.Content[1].Content[0].Text != "new human text" || richID(undone.Body.Content[2]) != "c" || undone.Page.Left != 1200 {
		t.Fatal(undone)
	}
	if _, err = UndoRichDocument(rraw(before), rraw(after), undone); !errors.Is(err, ErrUndoConflict) {
		t.Fatal("repeated undo should not reapply", err)
	}
	current.Body.Content[0].Content[0].Text = "human overlap"
	if _, err = UndoRichDocument(rraw(before), rraw(after), current); !errors.Is(err, ErrUndoConflict) {
		t.Fatal("overlap overwritten", err)
	}
}
func TestRichUndoNestedTable(t *testing.T) {
	table := RichNode{Type: "table", Attrs: map[string]any{"blockId": "table"}, Content: []RichNode{{Type: "tableRow", Content: []RichNode{{Type: "tableCell", Content: []RichNode{rp("a", "old")}}, {Type: "tableCell", Content: []RichNode{rp("b", "other")}}}}}}
	before := rbody(table)
	after, err := ApplyRichOperations(before, []RichOperation{{Kind: "replace_text", BlockID: "a", OldText: "old", Text: rstr("AI")}}, "table")
	if err != nil {
		t.Fatal(err)
	}
	current := richCopy(after)
	current.Body.Content[0].Content[0].Content[1].Content[0].Content[0].Text = "human"
	undone, err := UndoRichDocument(rraw(before), rraw(after), current)
	if err != nil {
		t.Fatal(err)
	}
	if undone.Body.Content[0].Content[0].Content[1].Content[0].Content[0].Text != "human" {
		t.Fatal("other cell overwritten")
	}
}

func TestRichUndoAcceptsEditorNormalizationOnly(t *testing.T) {
	before := rbody(rp("a", "Old"), rp("b", "Other"))
	after, err := ApplyRichOperations(before, []RichOperation{{Kind: "replace_text", BlockID: "a", OldText: "Old", Content: []RichNode{{Type: "text", Text: "A", Marks: []RichMark{{Type: "italic"}, {Type: "bold"}}}, {Type: "text", Text: "I", Marks: []RichMark{{Type: "italic"}, {Type: "bold"}}}}}, {Kind: "insert_blocks", BlockID: "a", Placement: "after", Content: []RichNode{rp("", "Inserted")}}}, "normalized")
	if err != nil {
		t.Fatal(err)
	}
	current := richCopy(after)
	current.Body.Content[0].Content = []RichNode{{Type: "text", Text: "AI", Marks: []RichMark{{Type: "bold"}, {Type: "italic"}}}}
	current.Body.Content[0].Attrs["textAlign"] = nil
	current.Body.Content[1].Attrs["textAlign"] = "left"
	current.Body.Content[2].Content[0].Text = "Human"
	undone, err := UndoRichDocument(rraw(before), rraw(after), current)
	if err != nil || len(undone.Body.Content) != 2 || undone.Body.Content[0].Content[0].Text != "Old" || undone.Body.Content[1].Content[0].Text != "Human" {
		t.Fatal(undone, err)
	}
	current.Body.Content[0].Content[0].Marks = []RichMark{{Type: "bold"}}
	if _, err = UndoRichDocument(rraw(before), rraw(after), current); !errors.Is(err, ErrUndoConflict) {
		t.Fatal("real formatting change was overwritten", err)
	}
	if _, err = NewRichBody([]RichNode{{Type: "paragraph", Attrs: map[string]any{"level": 2}}}, "invalid-attrs"); err == nil {
		t.Fatal("unsupported paragraph attribute accepted")
	}
}
