package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestNextPromptValidation(t *testing.T) {
	runner := NewBuiltinRunner()
	defer runner.Close()
	for _, mode := range []store.AgentMode{store.ModeChat, store.ModeWork, store.ModeCode} {
		if !HasDefinition(CoreDefinitionsForMode(mode, BuiltinDefinitions()), SetNextPrompt) {
			t.Fatalf("missing tool in %s", mode)
		}
	}
	for _, tc := range []struct {
		args string
		ok   bool
		text string
	}{
		{`{"text":"  帮我\n 补上测试  "}`, true, "帮我 补上测试"},
		{`{"text":""}`, false, ""},
		{`{"text":null}`, false, ""},
		{`{"text":123}`, false, ""},
		{`{"text":`, false, ""},
		{`{"text":"` + strings.Repeat("中", 200) + `"}`, true, strings.Repeat("中", 200)},
		{`{"text":"` + strings.Repeat("中", 201) + `"}`, false, ""},
	} {
		result := runner.Call(context.Background(), Call{Name: SetNextPrompt, CallID: "tip", Args: json.RawMessage(tc.args)})
		if result.Ok != tc.ok {
			t.Fatalf("args=%s: %+v", tc.args, result)
		}
		if tc.ok {
			var value struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(result.Content), &value); err != nil || value.Text != tc.text {
				t.Fatalf("unexpected result: %+v", result)
			}
		}
	}
}

func TestTurnTopicValidation(t *testing.T) {
	runner := NewBuiltinRunner()
	defer runner.Close()
	for _, mode := range []store.AgentMode{store.ModeChat, store.ModeWork, store.ModeCode} {
		if !HasDefinition(CoreDefinitionsForMode(mode, BuiltinDefinitions()), SetTurnTopic) {
			t.Fatalf("missing topic tool in %s", mode)
		}
	}
	for _, tc := range []struct {
		args  string
		ok    bool
		title string
	}{
		{`{"title":"  表单\n 校验  "}`, true, "表单 校验"},
		{`{"title":""}`, false, ""}, {`{"title":null}`, false, ""},
		{`{"title":123}`, false, ""}, {`{"title":`, false, ""},
		{`{"title":"` + strings.Repeat("中", 80) + `"}`, true, strings.Repeat("中", 80)},
		{`{"title":"` + strings.Repeat("中", 81) + `"}`, false, ""},
	} {
		result := runner.Call(context.Background(), Call{Name: SetTurnTopic, CallID: "topic", Args: json.RawMessage(tc.args)})
		if result.Ok != tc.ok {
			t.Fatalf("args=%s: %+v", tc.args, result)
		}
		if tc.ok {
			var value struct {
				Title string `json:"title"`
			}
			if err := json.Unmarshal([]byte(result.Content), &value); err != nil || value.Title != tc.title {
				t.Fatalf("unexpected topic: %+v", result)
			}
		}
	}
}
