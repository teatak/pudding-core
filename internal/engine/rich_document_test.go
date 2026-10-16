package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestRichDocumentReadOnlyReferences(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	e := New(st, event.NewHub(), nil, st, WithPlugins(plugin.NewService(t.TempDir(), nil)))
	if err := st.CreateSession(ctx, &store.Session{ID: "reader", Provider: "mock", Model: "mock", LoadedPluginIDs: []string{plugin.BuiltinDOCXID}}); err != nil {
		t.Fatal(err)
	}
	body := store.EmptyRichDocument()
	body.Body.Content[0].Content = []store.RichNode{{Type: "text", Text: "你好😀正文"}}
	doc, err := st.CreateRichDocument(ctx, &store.StudioItem{ID: "rich", Name: "DOCX", Kind: store.StudioItemKindRichDocument}, body, nil)
	if err != nil {
		t.Fatal(err)
	}
	latestBody := body
	latestBody.Body = store.RichNode{Type: "doc", Content: []store.RichNode{{Type: "paragraph", Content: []store.RichNode{{Type: "text", Text: "Updated"}}}}}
	latest, err := st.WriteRichDocument(ctx, "rich", store.RichDocumentWrite{ClientRequestID: "update", ExpectedHash: doc.ContentHash, Body: &latestBody})
	if err != nil {
		t.Fatal(err)
	}
	run := func(args any) tool.Result {
		raw, _ := json.Marshal(args)
		return e.executeAllowedTool(ctx, "reader", "turn", store.ModeChat, tool.Call{Name: tool.RichDocumentRead, CallID: "read", Args: raw})
	}
	out := run(map[string]any{"item_id": "rich", "revision_id": doc.RevisionID, "offset": 1, "limit": 2})
	var result map[string]any
	if !out.Ok || json.Unmarshal([]byte(out.Content), &result) != nil {
		t.Fatal(out.Content)
	}
	if result["body"] != "好😀" || result["nextOffset"] != float64(3) || result["revisionID"] != doc.RevisionID || result["readOnly"] != true {
		t.Fatal(result)
	}
	out = run(map[string]any{"item_id": "rich"})
	if !out.Ok || !strings.Contains(out.Content, "Updated") {
		t.Fatal(out.Content)
	}
	for _, args := range []map[string]any{
		{"item_id": "rich", "revision_id": "another-document-revision"},
		{"item_id": "rich", "offset": -1}, {"item_id": "rich", "limit": 16385},
		{"item_id": "rich", "region": "unknown"}, {"item_id": "rich", "variant": "even"},
	} {
		if out := run(args); out.Ok {
			t.Fatal("invalid read accepted", args)
		}
	}
	if _, err := st.CreateDocument(ctx, &store.StudioItem{ID: "md", Name: "Markdown", Kind: store.StudioItemKindDoc}, "original", store.ContentAuthor{Kind: "user"}); err != nil {
		t.Fatal(err)
	}
	if out := run(map[string]any{"item_id": "md"}); out.Ok {
		t.Fatal("Markdown accepted by rich read")
	}
	if _, err := st.GetDocument(ctx, "rich"); err == nil {
		t.Fatal("rich document exposed as Markdown")
	}
	after, _ := st.GetRichDocument(ctx, "rich")
	revisions, _ := st.ListStudioItemRevisions(ctx, "rich")
	if after.ContentHash != latest.ContentHash || len(revisions) != 2 {
		t.Fatal("read changed document")
	}
	item, _ := st.GetStudioItem(ctx, "rich")
	if _, err := st.SetStudioItemArchived(ctx, "rich", item.Revision, true); err != nil {
		t.Fatal(err)
	}
	if out := run(map[string]any{"item_id": "rich", "revision_id": doc.RevisionID}); out.Ok {
		t.Fatal("archived history bypassed active check")
	}
}

func TestRichDocumentTextProjection(t *testing.T) {
	p := func(text string) store.RichNode {
		return store.RichNode{Type: "paragraph", Content: []store.RichNode{{Type: "text", Text: text}}}
	}
	node := store.RichNode{Type: "doc", Content: []store.RichNode{p("Intro"), {Type: "table", Content: []store.RichNode{{Type: "tableRow", Content: []store.RichNode{{Type: "tableCell", Content: []store.RichNode{p("A")}}, {Type: "tableCell", Content: []store.RichNode{p("B"), p("C")}}}}}}, {Type: "image", Attrs: map[string]any{"alt": "diagram"}}, {Type: "pageNumber"}, {Type: "totalPages"}}}
	if text := richDocumentText(node); text != "Intro\nA\tB\nC\n[image: diagram]\n[PAGE]\n[NUMPAGES]" {
		t.Fatal(text)
	}
}

func TestRichDocumentCreateStructuredEdit(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	e := New(st, event.NewHub(), nil, st, WithPlugins(plugin.NewService(t.TempDir(), nil)))
	if err := st.CreateSession(ctx, &store.Session{ID: "writer", Provider: "mock", Model: "mock", LoadedPluginIDs: []string{plugin.BuiltinDOCXID}}); err != nil {
		t.Fatal(err)
	}
	run := func(name, call string, args any) tool.Result {
		raw, _ := json.Marshal(args)
		return e.executeAllowedTool(ctx, "writer", "turn", store.ModeChat, tool.Call{Name: name, CallID: call, Args: raw})
	}
	decode := func(r tool.Result) map[string]any {
		t.Helper()
		var v map[string]any
		if !r.Ok || json.Unmarshal([]byte(r.Content), &v) != nil {
			t.Fatal(r.Content)
		}
		return v
	}
	p := func(text string) store.RichNode {
		return store.RichNode{Type: "paragraph", Content: []store.RichNode{{Type: "text", Text: text}}}
	}
	created := decode(run(tool.RichDocumentCreate, "create", map[string]any{"name": "AI DOCX", "content": []store.RichNode{p("Original 😀"), p("Second")}}))
	id := created["itemID"].(string)
	if created["mountID"] == "" {
		t.Fatal("not opened")
	}
	original, _ := st.GetRichDocument(ctx, id)
	blocks := decode(run(tool.RichDocumentRead, "blocks", map[string]any{"item_id": id, "format": "blocks", "limit": 1}))
	if blocks["nextOffset"] != float64(1) || blocks["contentHash"] != original.ContentHash {
		t.Fatal(blocks)
	}
	first := blocks["blocks"].([]any)[0].(map[string]any)
	blockID := first["blockID"].(string)
	if first["node"].(map[string]any)["type"] != "paragraph" {
		t.Fatal(first)
	}
	edited := decode(run(tool.RichDocumentEdit, "edit", map[string]any{"item_id": id, "expected_hash": original.ContentHash, "operations": []map[string]any{{"kind": "replace_text", "block_id": blockID, "old_text": "Original 😀", "text": "Revised 😀"}, {"kind": "format_text", "block_id": blockID, "old_text": "Revised", "marks": []map[string]any{{"type": "bold"}}}}}))
	latest, _ := st.GetRichDocument(ctx, id)
	if latest.RevisionID != edited["revisionID"] || len(latest.Body.Body.Content[0].Content[0].Marks) != 1 {
		t.Fatal(latest)
	}
	past := decode(run(tool.RichDocumentRead, "past", map[string]any{"item_id": id, "format": "blocks", "revision_id": original.RevisionID, "block_ids": []string{blockID}}))
	if past["contentHash"] != original.ContentHash || !strings.Contains(run(tool.RichDocumentRead, "text", map[string]any{"item_id": id}).Content, "Revised") {
		t.Fatal(past)
	}
	invalid := run(tool.RichDocumentEdit, "invalid", map[string]any{"item_id": id, "expected_hash": latest.ContentHash, "operations": []map[string]any{{"kind": "set_block", "block_id": blockID, "node_type": "heading"}, {"kind": "delete_block", "block_id": "missing"}}})
	if invalid.Ok {
		t.Fatal("invalid batch accepted")
	}
	unchanged, _ := st.GetRichDocument(ctx, id)
	revisions, _ := st.ListStudioItemRevisions(ctx, id)
	if unchanged.RevisionID != latest.RevisionID || len(revisions) != 2 {
		t.Fatal("failed batch wrote history")
	}
	for _, name := range []string{tool.DocCreate, tool.TableCreate} {
		if r := run(name, "foreign", map[string]any{}); r.Ok {
			t.Fatal("unloaded plugin ran", name)
		}
	}
	if r := run(tool.RichDocumentRead, "missing", map[string]any{"item_id": id, "format": "blocks", "block_ids": []string{"missing"}}); r.Ok {
		t.Fatal("missing target silently ignored")
	}
}
