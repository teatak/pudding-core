package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/store"
)

type fakeResultHistory struct {
	fakeHistorySource
	turn *store.ConversationTurn
}

func (f *fakeResultHistory) GetConversationTurn(_ context.Context, sessionID, turnID string) (*store.ConversationTurn, error) {
	if sessionID != f.turn.SessionID || turnID != f.turn.ID {
		return nil, store.ErrNotFound
	}
	return f.turn, nil
}

func TestResultReadSelectsOriginalFieldsAndDetectsAmbiguousCalls(t *testing.T) {
	raw := `{"ok":true,"content":"原始正文🙂","matches":[{"line":2,"text":"命中"}]}`
	src := &fakeResultHistory{turn: &store.ConversationTurn{ID: "t1", SessionID: "s1", Messages: []*store.Message{{
		Parts: []store.ContentPart{{Type: store.ContentPartToolResult, CallID: "c1", Name: FileSearch, Ok: true, Content: raw}},
	}}}}
	runner := NewBuiltinRunner(WithHistorySearch(src))
	read := func(fields map[string]any) Result {
		fields["result_ref"] = ResultReference{TurnID: "t1", CallID: "c1"}
		args, _ := json.Marshal(fields)
		return runner.Call(context.Background(), Call{SessionID: "s1", Name: HistoryGetMessage, Args: args})
	}
	for _, tc := range []struct{ field, want string }{
		{"", raw}, {"content", "原始正文🙂"}, {"matches", `[{"line":2,"text":"命中"}]`}, {"ok", "true"},
	} {
		args := map[string]any{}
		if tc.field != "" {
			args["field"] = tc.field
		}
		result := read(args)
		if !result.Ok {
			t.Fatal(result.Content)
		}
		page := decodeToolResult(t, result)
		if page["content"] != tc.want || page["has_more"] != false {
			t.Fatalf("unexpected field page: %+v", page)
		}
		if _, exists := page["next_offset"]; exists {
			t.Fatal("EOF must not advertise a next page")
		}
	}
	end := read(map[string]any{"field": "content", "offset": 5})
	if !end.Ok || decodeToolResult(t, end)["content"] != "" {
		t.Fatalf("empty EOF page failed: %+v", end)
	}
	src.turn.Messages[0].Parts = append(src.turn.Messages[0].Parts, src.turn.Messages[0].Parts[0])
	result := read(map[string]any{})
	if result.Ok || decodeToolResult(t, result)["reason"] != "ambiguous_result" {
		t.Fatalf("ambiguous snapshot accepted: %+v", result)
	}
}

func TestResultReadPlainTextAndMissingSource(t *testing.T) {
	src := &fakeResultHistory{turn: &store.ConversationTurn{ID: "t1", SessionID: "s1", Messages: []*store.Message{{
		Parts: []store.ContentPart{{Type: store.ContentPartToolResult, CallID: "c1", Content: "plain text"}},
	}}}}
	call := Call{SessionID: "s1", Name: HistoryGetMessage, Args: json.RawMessage(`{"result_ref":{"turn_id":"t1","call_id":"c1"}}`)}
	result := NewBuiltinRunner(WithHistorySearch(src)).Call(context.Background(), call)
	if !result.Ok || decodeToolResult(t, result)["content"] != "plain text" {
		t.Fatal(result.Content)
	}
	result = NewBuiltinRunner().Call(context.Background(), call)
	if result.Ok || decodeToolResult(t, result)["reason"] != "history_unavailable" {
		t.Fatal(result.Content)
	}
}

func TestResultReadFocusedLinesAndItems(t *testing.T) {
	log := strings.Repeat("routine output\n", 300) + "ERROR 中文🙂\r\n" + strings.Repeat("routine output\n", 300) + "ERROR second"
	raw, _ := json.Marshal(map[string]any{"stdout": log, "matches": []any{map[string]any{"line": 5, "text": "first"}, map[string]any{"line": 9, "text": "second"}}})
	src := &fakeResultHistory{turn: &store.ConversationTurn{ID: "t1", SessionID: "s1", Messages: []*store.Message{{
		Parts: []store.ContentPart{{Type: store.ContentPartToolResult, CallID: "c1", Ok: false, Content: string(raw)}},
	}}}}
	runner := NewBuiltinRunner(WithHistorySearch(src))
	read := func(extra string) Result {
		return runner.Call(context.Background(), Call{SessionID: "s1", Name: HistoryGetMessage, Args: json.RawMessage(`{"result_ref":{"turn_id":"t1","call_id":"c1"},` + extra + `}`)})
	}
	result := read(`"field":"stdout","unit":"lines","query":"ERROR","limit":1`)
	page := decodeToolResult(t, result)
	if !result.Ok || page["result_ok"] != false || page["total_lines"] != float64(602) || page["has_more"] != true || page["next_offset"] != float64(601) {
		t.Fatalf("unexpected search page: %s", result.Content)
	}
	lines := page["lines"].([]any)
	if len(lines) != 1 || lines[0].(map[string]any)["line"] != float64(301) || lines[0].(map[string]any)["text"] != "ERROR 中文🙂\r\n" {
		t.Fatalf("lost exact line or source position: %s", result.Content)
	}
	if len(result.Content) >= len(log)/4 {
		t.Fatal("focused search still returned the routine log")
	}
	result = read(`"field":"stdout","unit":"lines","query":"ERROR","offset":601,"limit":1`)
	page = decodeToolResult(t, result)
	if !result.Ok || page["has_more"] != false || page["lines"].([]any)[0].(map[string]any)["line"] != float64(602) {
		t.Fatalf("search continuation lost a match: %s", result.Content)
	}
	result = read(`"field":"stdout","unit":"lines","offset":300,"limit":1`)
	if !result.Ok || decodeToolResult(t, result)["lines"].([]any)[0].(map[string]any)["text"] != "ERROR 中文🙂\r\n" {
		t.Fatal(result.Content)
	}
	result = read(`"field":"matches","unit":"items","offset":1,"limit":1`)
	page = decodeToolResult(t, result)
	if !result.Ok || page["total_items"] != float64(2) || page["has_more"] != false || page["items"].([]any)[0].(map[string]any)["text"] != "second" {
		t.Fatalf("array record was not preserved: %s", result.Content)
	}
	for _, extra := range []string{
		`"field":"stdout","query":"ERROR"`,
		`"field":"stdout","unit":"lines","query":""`,
		`"field":"stdout","unit":"lines","query":"a\nb"`,
		`"field":"stdout","unit":"lines","limit":201`,
		`"field":"stdout","unit":"lines","offset":603`,
		`"field":"stdout","unit":"items"`,
		`"field":"matches","unit":"lines"`,
		`"field":"matches","unit":"items","query":"first"`,
		`"field":"stdout","unit":"bytes"`,
	} {
		if result := read(extra); result.Ok {
			t.Fatalf("invalid focused read accepted: %s", extra)
		}
	}
}

func TestResultReadCompleteRecordsRespectBudget(t *testing.T) {
	for _, unit := range []string{"lines", "items"} {
		t.Run(unit, func(t *testing.T) {
			var source strings.Builder
			var items []string
			for i := 0; i < 12; i++ {
				line := fmt.Sprintf("%d %s\n", i, strings.Repeat("中文🙂", 600))
				source.WriteString(line)
				items = append(items, line)
			}
			content := source.String()
			if unit == "items" {
				raw, _ := json.Marshal(items)
				content = string(raw)
			}
			src := &fakeResultHistory{turn: &store.ConversationTurn{ID: "t1", SessionID: "s1", Messages: []*store.Message{{
				Parts: []store.ContentPart{{Type: store.ContentPartToolResult, CallID: "c1", Content: content}},
			}}}}
			runner := NewBuiltinRunner(WithHistorySearch(src))
			var restored []string
			offset := 0
			for {
				args, _ := json.Marshal(map[string]any{"result_ref": ResultReference{TurnID: "t1", CallID: "c1"}, "unit": unit, "offset": offset, "limit": 200})
				result := runner.Call(context.Background(), Call{SessionID: "s1", Name: HistoryGetMessage, Args: args})
				if !result.Ok {
					t.Fatal(result.Content)
				}
				page := decodeToolResult(t, result)
				encoded, _ := json.Marshal(page[unit])
				if utf8.RuneCount(encoded) > resultPageMaxChars {
					t.Fatal("encoded records exceeded the page budget")
				}
				for _, record := range page[unit].([]any) {
					if unit == "lines" {
						restored = append(restored, record.(map[string]any)["text"].(string))
					} else {
						restored = append(restored, record.(string))
					}
				}
				if page["has_more"] == false {
					break
				}
				next := int(page["next_offset"].(float64))
				if next <= offset || len(page[unit].([]any)) >= len(items) {
					t.Fatal("budget was ignored or pagination did not advance")
				}
				offset = next
			}
			if strings.Join(restored, "") != source.String() {
				t.Fatal("paging split, lost or duplicated a complete record")
			}
		})
	}
}

func TestResultReadRecordEdges(t *testing.T) {
	for _, tc := range []struct {
		name, raw, extra, reason string
	}{
		{name: "empty text", raw: `{"text":""}`, extra: `"field":"text","unit":"lines"`},
		{name: "empty array", raw: `{"data":[]}`, extra: `"field":"data","unit":"items"`},
		{name: "no matches", raw: `{"text":"ordinary\ntext\n"}`, extra: `"field":"text","unit":"lines","query":"ERROR"`},
		{name: "case sensitive", raw: `{"text":"error\n"}`, extra: `"field":"text","unit":"lines","query":"ERROR"`},
		{name: "line EOF", raw: `{"text":"first\nlast"}`, extra: `"field":"text","unit":"lines","offset":2`},
		{name: "array EOF", raw: `{"data":[1]}`, extra: `"field":"data","unit":"items","offset":1`},
		{name: "string is not array", raw: `{"data":"[1,2]"}`, extra: `"field":"data","unit":"items"`, reason: "invalid_field"},
		{name: "object is not array", raw: `{"data":{}}`, extra: `"field":"data","unit":"items"`, reason: "invalid_field"},
		{name: "null is not array", raw: `{"data":null}`, extra: `"field":"data","unit":"items"`, reason: "invalid_field"},
		{name: "oversized line", raw: jsonString(map[string]any{"text": strings.Repeat("x", 8001)}), extra: `"field":"text","unit":"lines"`, reason: "record_too_large"},
		{name: "oversized item", raw: jsonString(map[string]any{"data": []string{strings.Repeat("x", 8001)}}), extra: `"field":"data","unit":"items"`, reason: "record_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &fakeResultHistory{turn: &store.ConversationTurn{ID: "t1", SessionID: "s1", Messages: []*store.Message{{
				Parts: []store.ContentPart{{Type: store.ContentPartToolResult, CallID: "c1", Content: tc.raw}},
			}}}}
			result := NewBuiltinRunner(WithHistorySearch(src)).Call(context.Background(), Call{SessionID: "s1", Name: HistoryGetMessage,
				Args: json.RawMessage(`{"result_ref":{"turn_id":"t1","call_id":"c1"},` + tc.extra + `}`)})
			page := decodeToolResult(t, result)
			if tc.reason != "" {
				if result.Ok || page["reason"] != tc.reason {
					t.Fatal(result.Content)
				}
				return
			}
			if !result.Ok || page["has_more"] != false {
				t.Fatal(result.Content)
			}
			if _, exists := page["next_offset"]; exists {
				t.Fatal("empty/EOF result must not offer another page")
			}
			if records := page[page["unit"].(string)].([]any); len(records) != 0 {
				t.Fatalf("expected empty page: %s", result.Content)
			}
		})
	}
}

func TestResultReadItemsPreserveNumbersAndLinesPreserveBlankEndings(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ unit, content, want string }{
		{"items", `[{ "number": 9007199254740993, "text":"a\"b" }, null, true, [1,2]]`, `[{"number":9007199254740993,"text":"a\"b"},null,true,[1,2]]`},
		{"lines", "\n\r\nlast\n", `[{"line":1,"text":"\n"},{"line":2,"text":"\r\n"},{"line":3,"text":"last\n"}]`},
	} {
		out := resultRecordPage(ctx, Result{}, map[string]any{}, tc.content, tc.unit, nil, 0, 20)
		var page map[string]json.RawMessage
		if !out.Ok || json.Unmarshal([]byte(out.Content), &page) != nil || string(page[tc.unit]) != tc.want {
			t.Fatalf("record contents changed: %s", out.Content)
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	result := resultRecordPage(ctx, Result{}, map[string]any{}, "text\n", "lines", nil, 0, 20)
	if result.Ok || decodeToolResult(t, result)["reason"] != "cancelled" {
		t.Fatal(result.Content)
	}
}
