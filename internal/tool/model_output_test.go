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

func TestCommandProcessOutputTextKeepsStreamsOffsetsAndReadback(t *testing.T) {
	for _, name := range []string{CommandRun, CommandSession} {
		t.Run(name, func(t *testing.T) {
			payload := map[string]any{
				"ok": true, "processID": "process-1", "status": "exited", "exitCode": 7,
				"nextOffset": 42, "hasMore": false,
				"output": []BackgroundProcessOutputChunk{
					{Offset: 0, Stream: "stdout", Content: "line one\n第二行\n"},
					{Offset: 23, Stream: "stderr", Content: "test failed\nreason\n"},
				},
			}
			raw := jsonString(payload)
			spec := builtinModelOutput(name)
			out := ModelResultContent(name, true, raw, "turn", "call", true, OutputPresentation{Enabled: true, Spec: spec})
			for _, want := range []string{`"processID":"process-1"`, `"exitCode":7`, `"offset":23`, `"stream":"stderr"`, `"nextOffset":42`, `"hasMore":false`, `"/output/0/content"`, `"/output/1/content"`, "line one\n第二行\n", "test failed\nreason\n"} {
				if !strings.Contains(out, want) {
					t.Fatalf("missing process output or metadata %q: %s", want, out)
				}
			}
			if got := ModelResultContent(name, true, raw, "turn", "call", true, OutputPresentation{Enabled: false, Spec: spec}); got != raw {
				t.Fatal("disabled text expansion changed canonical JSON")
			}
			payload["output"] = []BackgroundProcessOutputChunk{{Stream: "stdout", Content: strings.Repeat("long log line\n", modelResultMaxChars)}}
			out = ModelResultContent(name, true, jsonString(payload), "turn", "call", true, OutputPresentation{Enabled: true, Spec: spec})
			if utf8.RuneCountInString(out) > modelResultMaxChars || !strings.Contains(out, `"result_ref"`) || !strings.Contains(out, `"exitCode":7`) || !strings.Contains(out, `"processID":"process-1"`) || !strings.Contains(out, `"nextOffset":42`) {
				t.Fatal("large process output lost budget, status, cursor or readback")
			}
			for _, empty := range []string{`{"ok":true,"output":[]}`, `{"ok":true,"processes":[]}`, `{"ok":false,"reason":"process_not_found"}`} {
				if got := ModelResultContent(name, true, empty, "turn", "call", true, OutputPresentation{Enabled: true, Spec: spec}); got != empty {
					t.Fatalf("optional log fields broke a non-log response: %s", got)
				}
			}
		})
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

func TestCommandPreviewPreservesTailAcrossOutputChunks(t *testing.T) {
	var output backgroundProcessOutputBuffer
	for i := 0; i < 239; i++ {
		output.Append("stdout", []byte("INFO "+strings.Repeat("字\\\"", 80)+"\n"))
	}
	output.Append("stderr", []byte("--- FAIL: TestTail (0.00s)\nlast failure detail\n"))
	chunks, next, _, _ := output.Read(0, backgroundProcessPollMax)
	payload := map[string]any{"ok": true, "processID": "process-1", "running": false, "exitCode": 1, "output": chunks, "nextOffset": next, "tailOffset": next, "hasMore": false}
	canonical := jsonString(payload)
	for _, name := range []string{CommandRun, CommandSession} {
		for _, expanded := range []bool{false, true} {
			view := ModelResultContent(name, true, canonical, "turn", "call", true, OutputPresentation{Enabled: expanded, Spec: builtinModelOutput(name)})
			for _, want := range []string{"TestTail", "last failure detail", `"exitCode":1`, `"tailOffset":`, `"nextOffset":`, `"result_ref":`, "stderr"} {
				if !strings.Contains(view, want) {
					t.Fatalf("%s expanded=%v missing %s: %s", name, expanded, want, view)
				}
			}
			if utf8.RuneCountInString(view) > modelResultMaxChars {
				t.Fatal("preview exceeded budget")
			}
			if !expanded && !json.Valid([]byte(view)) {
				t.Fatal("JSON preview is invalid")
			}
		}
	}
	if jsonString(payload) != canonical {
		t.Fatal("preview mutated original output")
	}
	full := ModelResultContent(CommandSession, true, canonical, "turn", "call", false, OutputPresentation{Enabled: true, Spec: builtinModelOutput(CommandSession)})
	if strings.Contains(full, "text omitted") || !strings.Contains(full, "/output/239/content") {
		t.Fatal("result without readback was shortened")
	}
}

func TestCompletedPollPreviewKeepsStreamsAndFailureMetadata(t *testing.T) {
	payload := map[string]any{"ok": true, "processID": "process-1", "running": false, "exitCode": 1, "stdout": strings.Repeat("ordinary log\n", 3000) + "--- FAIL: TestValue (0.00s)\n", "stderr": "linker detail\n", "tailOffset": 123456, "nextOffset": 65536, "hasMore": true, "testFailures": []map[string]any{{"name": "TestValue", "message": "actual mismatch", "excerpt": strings.Repeat("assertion detail\n", 2000)}}}
	for _, expanded := range []bool{false, true} {
		view := ModelResultContent(CommandSession, true, jsonString(payload), "turn", "poll", true, OutputPresentation{Enabled: expanded, Spec: builtinModelOutput(CommandSession)})
		for _, want := range []string{"--- FAIL: TestValue", "linker detail", "actual mismatch", `"tailOffset":123456`, `"hasMore":true`} {
			if !strings.Contains(view, want) {
				t.Fatalf("missing %s: %s", want, view)
			}
		}
		if utf8.RuneCountInString(view) > modelResultMaxChars {
			t.Fatal("completed preview exceeded budget")
		}
	}
}

func TestCompletedPollPreviewKeepsRequestedPage(t *testing.T) {
	log := "first capture line\n" + strings.Repeat("x", 20000-len("first capture line\n")) + "requested middle line\n" + strings.Repeat("y", 22000) + "last capture line\n"
	process := &backgroundProcess{id: "paged", foreground: &foregroundCommand{}, finalResult: &Result{Content: jsonString(map[string]any{"stdout": log, "stderr": "", "exitCode": 7})}}
	process.output.Append("stdout", []byte(log))
	for _, offset := range []int64{0, 20000, int64(len(log))} {
		payload := process.pollPayload(offset, 2048, 0)
		canonical := jsonString(payload)
		for _, expanded := range []bool{false, true} {
			view := ModelResultContent(CommandSession, true, canonical, "turn", "poll", true, OutputPresentation{Enabled: expanded, Spec: builtinModelOutput(CommandSession)})
			for _, key := range []string{"nextOffset", "tailOffset", "hasMore", "exitCode"} {
				if !strings.Contains(view, `"`+key+`":`+jsonString(payload[key])) {
					t.Fatalf("offset=%d: lost %s", offset, key)
				}
			}
			if offset == 0 {
				if !strings.Contains(view, "first capture line") || !strings.Contains(view, "last capture line") {
					t.Fatal("initial poll lost completion summary")
				}
			} else {
				if strings.Contains(view, "first capture line") || strings.Contains(view, "last capture line") {
					t.Fatalf("offset=%d: replayed full capture instead of requested page", offset)
				}
				if offset == 20000 && !strings.Contains(view, "requested middle line") {
					t.Fatal("missing requested middle page")
				}
			}
			if utf8.RuneCountInString(view) > modelResultMaxChars {
				t.Fatal("page preview exceeded budget")
			}
			// The direct command adapter still needs the final captured streams.
			commandView := ModelResultContent(CommandRun, true, canonical, "turn", "run", true, OutputPresentation{Enabled: expanded, Spec: builtinModelOutput(CommandRun)})
			if !strings.Contains(commandView, "first capture line") || !strings.Contains(commandView, "last capture line") {
				t.Fatal("direct command result lost completion summary")
			}
		}
		if payload["stdout"] != log || jsonString(payload) != canonical {
			t.Fatal("model preview changed the canonical completion result")
		}
	}
}
