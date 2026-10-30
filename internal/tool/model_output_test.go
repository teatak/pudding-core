package tool

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/modeloutput"
)

func TestDeclaredToolTextIsOptInAndFailureStatusSurvives(t *testing.T) {
	raw := `{"ok":false,"exitCode":7,"stdout":"line one\nline two\n","stderr":"error\n"}`
	spec := builtinModelOutput(CommandRun)
	if got := ModelResultContent(CommandRun, false, raw, "t", "c", true, OutputPresentation{Spec: spec}); got != raw {
		t.Fatal("default-off changed result")
	}
	out := ModelResultContent(CommandRun, false, raw, "t", "c", true, OutputPresentation{Enabled: true, Spec: spec})
	for _, want := range []string{`"exitCode":7`, `"ok":false`, "line one\nline two\n", `"/stdout"`, `"/stderr"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	if got := ModelResultContent(CommandRun, false, raw, "t", "c", true, OutputPresentation{Enabled: true}); got != raw {
		t.Fatal("inferred a spec on legacy history")
	}
	invalid := &modeloutput.Spec{Version: 1, TextFields: []modeloutput.TextField{{Path: "/missing"}}}
	out = ModelResultContent(CommandRun, false, raw, "t", "c", true, OutputPresentation{Enabled: true, Spec: invalid})
	if !strings.HasPrefix(out, raw) || !strings.Contains(out, "Presentation unavailable") {
		t.Fatal("invalid declaration lost action output")
	}
}

func TestSearchPresentationPreservesCanonicalPathsAndNumberedSource(t *testing.T) {
	raw := `{"matches":[{"path":"a.go","line":8,"lineStart":7,"lineEnd":9,"text":"target","excerpt":"before\ntarget\nafter"}]}`
	out := ModelResultContent(FileSearch, true, raw, "t", "c", true, OutputPresentation{Enabled: true, Spec: builtinModelOutput(FileSearch)})
	if !strings.Contains(out, "7: before\n8: target\n9: after") || !strings.Contains(out, `"/matches/0/excerpt"`) || strings.Count(out, "target") != 1 {
		t.Fatalf("bad numbered projection: %s", out)
	}
}

func TestTextProjectionBudgetPreservesWholeBoardsAndReadback(t *testing.T) {
	board := strings.Repeat("1 . . . . .\n", 3000)
	raw, _ := json.Marshal(map[string]any{"board": board, "stateVersion": 14, "ok": true})
	spec := &modeloutput.Spec{Version: 1, TextFields: []modeloutput.TextField{{Path: "/board", Overflow: "whole"}}}
	view := ModelResultContent("widget_invoke", true, string(raw), "turn", "call", true, OutputPresentation{Enabled: true, Spec: spec})
	if utf8.RuneCountInString(view) > modelResultMaxChars || strings.Contains(view, "1 . .") || !strings.Contains(view, `"result_ref"`) || !strings.Contains(view, `"stateVersion":14`) {
		t.Fatalf("bad bounded board: %s", view)
	}
	unbounded := ModelResultContent("widget_invoke", true, string(raw), "turn", "call", false, OutputPresentation{Enabled: true, Spec: spec})
	if !strings.Contains(unbounded, board) {
		t.Fatal("truncated a result without readback")
	}
}

func TestMCPPresentationRetainsOriginalEnvelopeAndExtraText(t *testing.T) {
	raw := json.RawMessage(`{"content":[{"type":"text","text":"{\"x\":1}"},{"type":"text","text":"Additional evidence\n"}],"structuredContent":{"x":1}}`)
	r := pluginMCPToolResult(Call{Name: "mcp_example", CallID: "c"}, raw)
	if string(r.MCPResult) != string(raw) {
		t.Fatal("MCP envelope not preserved")
	}
	view := ModelResultContent(r.Name, r.Ok, r.Content, "t", "c", true, OutputPresentation{Enabled: true, MCPResult: r.MCPResult})
	if strings.Count(view, `"x":1`) != 1 || !strings.Contains(view, "Additional evidence\n") {
		t.Fatalf("lost or duplicated MCP text: %s", view)
	}
	self := json.RawMessage(`{"content":[{"type":"text","text":"expanded"}],"structuredContent":{"text":"a\nb"},"_meta":{"pudding":{"canonical":{"text":"a\nb"},"modelOutput":{"version":1,"textFields":[{"path":"/text"}]}}}}`)
	r = pluginMCPToolResult(Call{Name: "widget_invoke"}, self)
	if r.Content != `{"text":"a\nb"}` || r.ModelOutput == nil || string(r.MCPResult) != string(self) {
		t.Fatal("self MCP canonical/spec lost")
	}
}

func TestPresentationFailureAndMetadataStayWithinBudget(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"ok": false, "exitCode": 7, "stateVersion": 42, "body": strings.Repeat("x", modelResultMaxChars-60)})
	invalid := &modeloutput.Spec{Version: 1, TextFields: []modeloutput.TextField{{Path: "/missing"}}}
	out := ModelResultContent("example", false, string(raw), "t", "c", true, OutputPresentation{Enabled: true, Spec: invalid})
	for _, want := range []string{`"exitCode":7`, `"ok":false`, `"stateVersion":42`, "Presentation unavailable", "result_ref"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if utf8.RuneCountInString(out) > modelResultMaxChars {
		t.Fatal("diagnostic exceeded budget")
	}
	root, _ := json.Marshal(strings.Repeat("long line\n", 3000))
	spec := &modeloutput.Spec{Version: 1, TextFields: []modeloutput.TextField{{Path: ""}}}
	out = ModelResultContent("example", true, string(root), "t", "c", true, OutputPresentation{Enabled: true, Spec: spec})
	if utf8.RuneCountInString(out) > modelResultMaxChars || !strings.Contains(out, `"value":{"$text_ref"`) || !strings.Contains(out, `| "" ---`) {
		t.Fatal("root text lost its reference or exceeded budget")
	}
}
