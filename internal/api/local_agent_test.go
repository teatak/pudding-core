package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/teatak/pudding-core/internal/acp"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/skill"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalAgentRemoteBoundary(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "local", Provider: acp.Provider, Model: "flash"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "local", ClientMessageID: "queue", Text: "original", Provider: acp.Provider, Model: "flash"}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ method, path, body string }{
		{"GET", "/desktop/local-agent-catalog", ""},
		{"POST", "/sessions", `{"provider":"local_agent:antigravity","model":"flash"}`},
		{"PATCH", "/sessions/local", `{"provider":"local_agent:antigravity"}`},
		{"POST", "/sessions/local/submit", `{"clientMessageID":"remote","text":"hello"}`},
		{"POST", "/sessions/local/turns/old/retry", `{"clientMessageID":"retry"}`},
		{"PATCH", "/sessions/local/queued-inputs/queue", `{"text":"remote edit"}`},
	} {
		t.Run(test.path+test.method, func(t *testing.T) {
			request, err := http.NewRequest(test.method, srv.URL+test.path, bytes.NewBufferString(test.body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+testToken)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(engine.RequestSourceHeader, "remote")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var body map[string]any
			_ = json.NewDecoder(response.Body).Decode(&body)
			if response.StatusCode != 403 || body["error"] != "local_agent_desktop_required" {
				t.Fatalf("%d: %+v", response.StatusCode, body)
			}
		})
	}
	_, queued, err := st.LookupSubmission(ctx, "local", "queue")
	if err != nil || queued.Text != "original" {
		t.Fatalf("remote modified local queue: %+v %v", queued, err)
	}
	// Existing history remains readable remotely.
	request, _ := http.NewRequest("GET", srv.URL+"/sessions/local", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set(engine.RequestSourceHeader, "remote")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
}

func TestLocalAgentHostMCP(t *testing.T) {
	// Use the same subprocess fixture as engine tests, selected through the actual
	// standalone-install discovery path; no developer login or real model needed.
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin", "agy")
	if err := os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("go", "build", "-o", bin, "../engine/testdata/acp").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	t.Setenv("HOME", home)
	ms := storetest.New(t)
	hub := event.NewHub()
	skills := skill.NewService(home)
	plugins := plugin.NewService(home, ms)
	builtins := tool.NewBuiltinRunner(tool.WithHomeDir(home), tool.WithSkills(skills), tool.WithPluginEndpoints(plugins))
	t.Cleanup(func() { _ = builtins.Close() })
	plugins.WithBuiltinToolAvailability(builtins.ToolAvailable)
	eng := engine.New(ms, hub, registry.Static(mock.New()), ms, engine.WithAttachmentHome(home), engine.WithTools(builtins), engine.WithSkills(skills), engine.WithPlugins(plugins))
	srv := httptest.NewServer(New(eng, ms, ms, hub).WithHome(home).WithSkills(skills).WithPlugins(plugins).Handler(testToken, nil))
	defer srv.Close()
	defer func() { eng.Stop(); eng.Wait() }()
	eng.SetLocalHostEndpoint(srv.URL + "/local-agent-mcp")
	ctx := engine.WithRequestSource(context.Background(), "desktop")
	if err := ms.CreateSession(ctx, &store.Session{ID: "host", Provider: acp.Provider, Model: "flash", ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := eng.SetSessionDirectories(ctx, "host", []string{root}); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(home, "skills", "host-refresh")
	if err := os.MkdirAll(skillDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeGuides := func(version string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("HOST_RULE_"+version), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: host-refresh\ndescription: Test live host instructions.\n---\nHOST_SKILL_"+version), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeGuides("V1")
	submit, err := eng.Submit(ctx, engine.SubmitInput{SessionID: "host", ClientMessageID: "one", Text: "hold"})
	if err != nil {
		t.Fatal(err)
	}
	state, _ := ms.GetLocalAgentState(ctx, "host")
	log, err := os.ReadFile(filepath.Join(state.CWD, "rpc.log"))
	if err != nil {
		t.Fatal(err)
	}
	secret := ""
	for _, line := range bytes.Split(log, []byte("\n")) {
		var msg struct {
			Params struct {
				Servers []acp.MCPServer `json:"mcpServers"`
			}
		}
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		for _, server := range msg.Params.Servers {
			for _, env := range server.Env {
				if env.Name == "PUDDING_MCP_HOST_TOKEN" {
					secret = env.Value
				}
			}
		}
	}
	if secret == "" {
		t.Fatal("no scoped MCP configured")
	}
	call := func(token, method string, params any) (int, map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		request, _ := http.NewRequest("POST", srv.URL+"/local-agent-mcp", bytes.NewReader(raw))
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(response.Body).Decode(&out)
		return response.StatusCode, out
	}
	if status, _ := call(testToken, "tools/list", map[string]any{}); status != 401 {
		t.Fatal("general daemon token opened scoped host")
	}
	if status, out := call(secret, "tools/list", map[string]any{}); status != 200 || len(out["result"].(map[string]any)["tools"].([]any)) != 2 {
		t.Fatal("host catalog")
	}
	// Wait for the user prompt to become active without sending a second prompt.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		turn, release, err := eng.AcquireLocalHost(secret)
		if err == nil {
			if turn.TurnID != submit.TurnID {
				t.Fatal("wrong turn")
			}
			release()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, params := range []any{
		map[string]any{"name": "pudding_tools", "arguments": map[string]any{"query": "builtin_time_get_current"}},
		map[string]any{"name": "pudding_call", "arguments": map[string]any{"tool": "builtin_time_get_current", "arguments": map[string]string{"timezone": "UTC"}}},
	} {
		status, out := call(secret, "tools/call", params)
		result, ok := out["result"].(map[string]any)
		if status != 200 || !ok || result["isError"] == true {
			t.Fatalf("host call: %d %+v", status, out)
		}
	}
	// Assert the standard MCP text blocks that clients actually consume, not
	// extension fields. Selected skills and project rules must resolve from disk.
	hostText := func(name string, arguments any) string {
		t.Helper()
		status, out := call(secret, "tools/call", map[string]any{"name": name, "arguments": arguments})
		result, ok := out["result"].(map[string]any)
		if status != 200 || !ok || result["isError"] == true {
			t.Fatalf("host text: %d %+v", status, out)
		}
		var text strings.Builder
		for _, block := range result["content"].([]any) {
			if b := block.(map[string]any); b["type"] == "text" {
				text.WriteString(b["text"].(string))
			}
		}
		return text.String()
	}
	business := func(name string, args any) string {
		return hostText("pudding_call", map[string]any{"tool": name, "arguments": args})
	}
	if text := hostText("pudding_tools", map[string]any{}); !strings.Contains(text, "HOST_RULE_V1") || !strings.Contains(text, "host-refresh") {
		t.Fatalf("initial rules or skill catalog missing: %s", text)
	}
	if text := business(tool.SkillRead, map[string]string{"skill_id": "host-refresh"}); strings.Count(text, "HOST_SKILL_V1") != 1 {
		t.Fatalf("skill body must be delivered once: %s", text)
	}
	writeGuides("V2")
	for _, text := range []string{
		hostText("pudding_tools", map[string]any{}),
		business(tool.FileList, map[string]string{"scope": "project", "path": "."}),
	} {
		if strings.Contains(text, "HOST_RULE_V1") || strings.Contains(text, "HOST_SKILL_V1") || !strings.Contains(text, "HOST_RULE_V2") || strings.Count(text, "HOST_SKILL_V2") != 1 {
			t.Fatalf("refreshed instructions missing or stale: %s", text)
		}
	}
	query := map[string]string{"query": tool.SkillValidate}
	if text := hostText("pudding_tools", query); strings.Contains(text, `"name":"builtin_skill_validate"`) {
		t.Fatalf("unloaded plugin tool visible: %s", text)
	}
	business(tool.PluginLoad, map[string]string{"plugin_id": plugin.BuiltinSkillAuthoringID})
	if text := hostText("pudding_tools", query); !strings.Contains(text, `"name":"builtin_skill_validate"`) || !strings.Contains(text, "inputSchema") {
		t.Fatalf("loaded plugin tool unavailable: %s", text)
	}
	// Verify strict schema separation and rejection of mixed/unknown fields.
	for i, invalid := range []any{
		map[string]any{"name": "pudding_tools", "arguments": map[string]any{"tool": "builtin_time_get_current"}},
		map[string]any{"name": "pudding_call", "arguments": map[string]any{"tool": "builtin_time_get_current", "arguments": map[string]string{"timezone": "UTC"}, "query": "extra"}},
	} {
		status, out := call(secret, "tools/call", invalid)
		result, ok := out["result"].(map[string]any)
		if status != 200 || !ok || result["isError"] != true {
			t.Fatalf("expected mixed fields rejected: %d %+v", status, out)
		}
		content, _ := json.Marshal(result["content"])
		field := []string{"tool", "query"}[i]
		if !strings.Contains(string(content), "unknown field") || !strings.Contains(string(content), field) || !strings.Contains(string(content), "Example:") {
			t.Fatalf("field error and corrected example missing: %s", content)
		}
	}
	if err := eng.Cancel("host"); err != nil {
		t.Fatal(err)
	}
	eng.Wait()
	_, out := call(secret, "tools/call", map[string]any{"name": "pudding_call", "arguments": map[string]any{"tool": "builtin_time_get_current", "arguments": map[string]string{"timezone": "UTC"}}})
	if out["result"].(map[string]any)["isError"] != true {
		t.Fatal("idle host executed a call")
	}
	writeGuides("V3")
	if _, err := eng.Submit(ctx, engine.SubmitInput{SessionID: "host", ClientMessageID: "next", Text: "hold"}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, release, err := eng.AcquireLocalHost(secret); err == nil {
			release()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if text := hostText("pudding_tools", map[string]any{}); !strings.Contains(text, "HOST_RULE_V3") || !strings.Contains(text, "HOST_SKILL_V3") || strings.Contains(text, "HOST_SKILL_V2") {
		t.Fatalf("next turn did not refresh selected instructions: %s", text)
	}
	if err := eng.Cancel("host"); err != nil {
		t.Fatal(err)
	}
	eng.Wait()
}
