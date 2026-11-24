package engine

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/teatak/pudding-core/internal/acp"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLocalAgentConversation(t *testing.T) {
	name := "agy"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	if output, err := exec.Command("go", "build", "-o", binary, "./testdata/acp").CombinedOutput(); err != nil {
		t.Fatalf("build fake ACP: %v: %s", err, output)
	}
	ctx := WithRequestSource(context.Background(), "desktop")
	ms := storetest.New(t)
	home := t.TempDir()
	hub := event.NewHub()
	makeEngine := func() *Engine {
		e := New(ms, hub, registry.Static(mock.New()), ms, WithAttachmentHome(home))
		e.localAgentPaths = []string{filepath.Join(t.TempDir(), "missing-agy"), binary}
		return e
	}
	e := makeEngine()
	defer func() { e.Stop(); e.Wait() }()
	if err := ms.CreateSession(ctx, &store.Session{ID: "acp", Provider: acp.Provider, Model: "flash"}); err != nil {
		t.Fatal(err)
	}
	submit := func(id, text string) *SubmitResult {
		t.Helper()
		out, err := e.Submit(ctx, SubmitInput{SessionID: "acp", ClientMessageID: id, Text: text, ReasoningEffort: "low"})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	checkTurn := func(id string, status store.TurnStatus, contains string) {
		t.Helper()
		turn, err := ms.GetConversationTurn(ctx, "acp", id)
		if err != nil {
			t.Fatal(err)
		}
		if turn.Status != status {
			t.Fatalf("turn status %s: %s", turn.Status, turn.Error)
		}
		var text strings.Builder
		for _, m := range turn.Messages {
			for _, p := range m.Parts {
				text.WriteString(p.Text)
			}
		}
		if !strings.Contains(text.String(), contains) {
			t.Fatalf("missing %q: %s", contains, text.String())
		}
	}
	first := submit("first", "hello")
	e.Wait()
	checkTurn(first.TurnID, store.TurnCompleted, "flash/low: hello")
	state, err := ms.GetLocalAgentState(ctx, "acp")
	if err != nil {
		t.Fatal(err)
	}
	originalID := state.AgentSessionID
	// Catalog discovery must not touch the active ACP session or execute prompts.
	catalog, err := e.LocalAgentCatalog(ctx)
	if err != nil || catalog.Status != "ready" || len(catalog.Models) != 2 || !catalog.Image || catalog.Audio {
		t.Fatalf("catalog: %+v %v", catalog, err)
	}
	if len(catalog.Models[0].ReasoningOptions) != 2 || len(catalog.Models[1].ReasoningOptions) != 3 || catalog.Models[0].DefaultReasoning != "high" {
		t.Fatalf("model-specific options: %+v", catalog.Models)
	}
	unchanged, _ := ms.GetLocalAgentState(ctx, "acp")
	if *unchanged != *state {
		t.Fatal("catalog changed active conversation")
	}
	entries, _ := os.ReadDir(filepath.Join(home, "local-agents"))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "catalog-") {
			t.Fatal("catalog directory leaked")
		}
	}
	if _, err := e.LocalAgentCatalog(WithRequestSource(ctx, "remote")); !errors.Is(err, ErrLocalAgentDesktop) {
		t.Fatalf("remote catalog: %v", err)
	}
	paths := e.localAgentPaths
	e.localAgentPaths = []string{}
	missing, err := e.LocalAgentCatalog(ctx)
	if err != nil || missing.Status != "not_installed" || len(missing.Models) != 0 {
		t.Fatalf("missing CLI: %+v %v", missing, err)
	}
	e.localAgentPaths = []string{"/does/not/exist"}
	if _, err := e.LocalAgentCatalog(ctx); err == nil {
		t.Fatal("startup error disguised as an empty catalog")
	}
	e.localAgentPaths = paths
	session, _ := ms.GetSession(ctx, "acp")
	if session.Provider != acp.Provider || session.Title == "" {
		t.Fatalf("session changed or no provisional title: %+v", session)
	}
	usage, err := e.SessionUsage(ctx, "acp")
	if err != nil || usage.ContextWindow != 10000 || usage.ContextEstimatedTokens != 123 || usage.RequestCount != 1 || usage.LastPromptTokens != 20 || usage.LastOutputTokens != 10 {
		t.Fatalf("usage: %+v %v", usage, err)
	}
	if _, err = e.GenerateCommitMessage(ctx, "acp", "diff", "en"); !errors.Is(err, ErrNoModel) {
		t.Fatalf("auxiliary model: %v", err)
	}
	// Deduplication precedes CLI preparation, even when the old binary disappears.
	e.closeLocalAgents("acp")
	e.localAgentPaths = []string{"/does/not/exist"}
	duplicate := submit("first", "different text")
	if !duplicate.Duplicate || duplicate.TurnID != first.TurnID {
		t.Fatalf("duplicate: %+v", duplicate)
	}
	_, err = e.Submit(ctx, SubmitInput{SessionID: "acp", ClientMessageID: "unaccepted", Text: "hello"})
	if !errors.Is(err, ErrLocalAgent) {
		t.Fatalf("startup failure: %v", err)
	}
	turn, queued, err := ms.LookupSubmission(ctx, "acp", "unaccepted")
	if err != nil || turn != nil || queued != nil {
		t.Fatal("preparation failure accepted input")
	}
	e.localAgentPaths = []string{binary}
	// Resume/load replays must not enter Pudding history; configuration is reapplied.
	if err = os.WriteFile(filepath.Join(state.CWD, "load-only"), []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	e.Stop()
	e.Wait()
	e = makeEngine()
	second := submit("second", "again")
	e.Wait()
	checkTurn(second.TurnID, store.TurnCompleted, "flash/low: again")
	state, _ = ms.GetLocalAgentState(ctx, "acp")
	if state.AgentSessionID != originalID {
		t.Fatal("restored a different agent session")
	}
	messages, _ := ms.ListMessages(ctx, "acp", 0)
	for _, m := range messages {
		if strings.Contains(store.TextFromParts(m.Parts), "OLD REPLAY") {
			t.Fatal("replayed history persisted")
		}
	}
	// The next submission queues behind a real pending prompt; stop finalizes
	// that turn before the next prompt starts.
	held := submit("held", "hold")
	next := submit("queued", "next")
	if !next.Queued {
		t.Fatalf("not queued: %+v", next)
	}
	if err = e.Cancel("acp"); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	checkTurn(held.TurnID, store.TurnCancelled, "")
	promoted, _, err := ms.LookupSubmission(ctx, "acp", "queued")
	if err != nil || promoted == nil {
		t.Fatal("queue did not drain")
	}
	checkTurn(promoted.ID, store.TurnCompleted, "flash/low: next")
	crash := submit("crash", "crash")
	e.Wait()
	checkTurn(crash.TurnID, store.TurnFailed, "crash")
	retried, err := e.Retry(ctx, "acp", crash.TurnID, "retry-crash")
	if err != nil {
		t.Fatal(err)
	}
	e.Wait()
	checkTurn(retried.TurnID, store.TurnCompleted, "Original request for this retry")
	after := submit("after", "recovered")
	e.Wait()
	checkTurn(after.TurnID, store.TurnCompleted, "recovered")
	for _, source := range []string{"remote", ""} {
		_, err = e.Submit(WithRequestSource(context.Background(), source), SubmitInput{SessionID: "acp", ClientMessageID: "blocked" + source, Text: "hello"})
		if !errors.Is(err, ErrLocalAgentDesktop) {
			t.Fatalf("source %q accepted: %v", source, err)
		}
	}
	// Unsupported model configuration must leave the draft unaccepted.
	bad := "invalid"
	_, _ = ms.UpdateSession(ctx, "acp", store.SessionUpdate{Model: &bad})
	_, err = e.Submit(ctx, SubmitInput{SessionID: "acp", ClientMessageID: "badmodel", Text: "hello"})
	if !errors.Is(err, ErrLocalAgent) {
		t.Fatalf("invalid config: %v", err)
	}
	turn, _, _ = ms.LookupSubmission(ctx, "acp", "badmodel")
	if turn != nil {
		t.Fatal("bad config accepted a turn")
	}
	// CWD changes require an explicit, editable handoff before creating context.
	good := "flash"
	project := &store.Project{ID: "other", Name: "Other", RootDirs: []string{t.TempDir()}}
	if err = ms.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	_, _ = ms.UpdateSession(ctx, "acp", store.SessionUpdate{Model: &good, ProjectID: &project.ID})
	_, err = e.Submit(ctx, SubmitInput{SessionID: "acp", ClientMessageID: "handoff", Text: "continue"})
	var handoff *LocalAgentHandoffRequired
	if !errors.As(err, &handoff) || handoff.Text == "" {
		t.Fatalf("missing handoff: %v", err)
	}
	edited := "Keep only this reviewed context"
	_, _ = ms.UpdateSession(ctx, "acp", store.SessionUpdate{Model: &bad})
	_, err = e.Submit(ctx, SubmitInput{SessionID: "acp", ClientMessageID: "handoff", Text: "continue", LocalAgentHandoff: &edited})
	if !errors.Is(err, ErrLocalAgent) {
		t.Fatalf("handoff preparation error: %v", err)
	}
	_, _ = ms.UpdateSession(ctx, "acp", store.SessionUpdate{Model: &good})
	_, err = e.Submit(ctx, SubmitInput{SessionID: "acp", ClientMessageID: "handoff", Text: "continue"})
	if !errors.As(err, &handoff) {
		t.Fatalf("failed preparation lost the unsent handoff: %v", err)
	}

	confirmed, err := e.Submit(ctx, SubmitInput{SessionID: "acp", ClientMessageID: "handoff", Text: "continue", LocalAgentHandoff: &edited})
	if err != nil {
		t.Fatal(err)
	}
	e.Wait()
	checkTurn(confirmed.TurnID, store.TurnCompleted, edited)
	state, _ = ms.GetLocalAgentState(ctx, "acp")
	if state.AgentSessionID == originalID || state.CWD != project.RootDirs[0] {
		t.Fatalf("handoff state: %+v", state)
	}
	e.closeLocalAgents("acp")
	if err = os.WriteFile(filepath.Join(state.CWD, "restore-fails"), []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = e.Submit(ctx, SubmitInput{SessionID: "acp", ClientMessageID: "restore-failed", Text: "continue"})
	if !errors.As(err, &handoff) {
		t.Fatalf("restore failure must offer reviewed handoff: %v", err)
	}
	empty := ""
	reset, err := e.Submit(ctx, SubmitInput{SessionID: "acp", ClientMessageID: "restore-failed", Text: "fresh context", LocalAgentHandoff: &empty})
	if err != nil {
		t.Fatal(err)
	}
	e.Wait()
	checkTurn(reset.TurnID, store.TurnCompleted, "fresh context")
	deadline, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err = e.WaitSession(deadline, "acp"); err != nil {
		t.Fatal(err)
	}
}

// Host calls share a user turn, inherit its tool policy, survive normal next
// turns, and lose access on cancellation or owned CLI shutdown.
func TestLocalAgentHostTools(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "agy")
	if output, err := exec.Command("go", "build", "-o", binary, "./testdata/acp").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	ctx := WithRequestSource(context.Background(), "desktop")
	ms := storetest.New(t)
	home := t.TempDir()
	e := New(ms, event.NewHub(), registry.Static(mock.New()), ms, WithAttachmentHome(home), WithTools(tool.NewBuiltinRunner(tool.WithHomeDir(home))))
	e.localAgentPaths = []string{binary}
	e.SetLocalHostEndpoint("http://127.0.0.1:1234/local-agent-mcp")
	defer func() { e.Stop(); e.Wait() }()
	if err := ms.CreateSession(ctx, &store.Session{ID: "host", Provider: acp.Provider, Model: "flash", ActiveMode: store.ModeChat}); err != nil {
		t.Fatal(err)
	}
	submit := func(id, text string) *SubmitResult {
		t.Helper()
		out, err := e.Submit(ctx, SubmitInput{SessionID: "host", ClientMessageID: id, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	first := submit("one", "hold")
	r := e.localRuntime("host")
	secret := r.hostSecret
	var turn *LocalHostTurn
	var release func()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		turn, release, err = e.AcquireLocalHost(secret)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if turn == nil {
		t.Fatal("host turn unavailable")
	}
	if turn.TurnID != first.TurnID {
		t.Fatal("host created an extra turn")
	}
	for i := 0; i < 2; i++ {
		reply, err := e.CallLocalHost(ctx, turn, tool.TimeGetCurrent, json.RawMessage(`{"timezone":"UTC"}`))
		if err != nil || !reply.Result.Ok {
			t.Fatalf("tool %d: %+v %v", i, reply.Result, err)
		}
	}
	reply, err := e.CallLocalHost(ctx, turn, "nonexistent", json.RawMessage(`{}`))
	if err != nil || reply.Result.Ok {
		t.Fatalf("unavailable tool: %+v %v", reply, err)
	}
	release()
	next := submit("two", "hello")
	if !next.Queued {
		t.Fatal("expected queue")
	}
	if err = e.Cancel("host"); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	if _, _, err = e.AcquireLocalHost(secret); err == nil {
		t.Fatal("finished turn accepts tools")
	}
	if !e.HasLocalHost(secret) {
		t.Fatal("normal cancel revoked reusable connection")
	}
	saved, err := ms.GetConversationTurn(ctx, "host", first.TurnID)
	if err != nil || saved.Status != store.TurnCancelled {
		t.Fatalf("first: %+v %v", saved, err)
	}
	count := 0
	for _, m := range saved.Messages {
		for _, part := range m.Parts {
			if part.Type == store.ContentPartToolResult && part.Name == tool.TimeGetCurrent {
				count++
			}
		}
	}
	if count != 2 {
		t.Fatalf("saved time tool calls: %d", count)
	}
	queued, _, _ := ms.LookupSubmission(ctx, "host", "two")
	if queued == nil || queued.Status != store.TurnCompleted {
		t.Fatal("queue failed")
	}
	// Both owned gateway confirmations are answered without a pending approval,
	// including a new turn after cancellation on the same connection.
	for _, text := range []string{"host-permission-tools", "host-permission-call"} {
		sent := submit(text, text)
		waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := e.WaitSession(waitCtx, "host")
		cancel()
		if err != nil {
			_ = e.Cancel("host")
			t.Fatalf("gateway confirmation did not finish: %v", err)
		}
		view, err := ms.GetConversationTurn(ctx, "host", sent.TurnID)
		if err != nil || view.Status != store.TurnCompleted || len(e.PendingApprovals("host")) != 0 {
			t.Fatalf("gateway confirmation: %+v %v", view, err)
		}
		var response string
		for _, m := range view.Messages {
			if m.Role == store.RoleAssistant {
				response += store.TextFromParts(m.Parts)
			}
		}
		if !strings.Contains(response, `"optionId":"allow-this"`) {
			t.Fatalf("did not return offered allow_once: %s", response)
		}
	}
	// Native permissions, even a Pudding-looking name, require explicit choice.
	submit("permission", "permission-test")
	var approvals []ApprovalRequest
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		approvals = e.PendingApprovals("host")
		if len(approvals) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(approvals) != 1 || approvals[0].Kind != ApprovalKindAgentPermission {
		t.Fatalf("permissions: %+v", approvals)
	}
	if _, err = e.ResolveLocalPermission(ctx, "host", approvals[0].ID, "invented"); err == nil {
		t.Fatal("accepted invented option")
	}
	if _, err = e.ResolveLocalPermission(ctx, "host", approvals[0].ID, "allow-this"); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	permissionTurn, _, _ := ms.LookupSubmission(ctx, "host", "permission")
	view, _ := ms.GetConversationTurn(ctx, "host", permissionTurn.ID)
	text := ""
	for _, message := range view.Messages {
		text += store.TextFromParts(message.Parts)
	}
	if !strings.Contains(text, "allow-this") {
		t.Fatalf("option not returned to ACP: %s", text)
	}
	submit("permission-cancel", "permission-test")
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(e.PendingApprovals("host")) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = e.Cancel("host"); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	if len(e.PendingApprovals("host")) != 0 {
		t.Fatal("pending permission leaked")
	}
	e.closeLocalAgents("host")
	if e.HasLocalHost(secret) {
		t.Fatal("closed CLI credential still valid")
	}
}

func TestLocalAgentHostPermissionScope(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := &localAgentHost{sessionID: "session", name: "pudding_host_current", turn: &LocalHostTurn{TurnID: "turn", Context: ctx}}
	e := &Engine{localHosts: map[string]*localAgentHost{"owned": host}}
	permission := acp.Permission{ToolCall: acp.Update{Name: "mcp_pudding_host_current_pudding_call"}, Options: []acp.PermissionOption{{ID: "one", Kind: "allow_once"}, {ID: "forever", Kind: "allow_always"}}}
	for _, tc := range []struct {
		name, sid, tid, secret, tool string
		want                         bool
	}{
		{"current gateway", "session", "turn", "owned", permission.ToolCall.Name, true},
		{"catalog", "session", "turn", "owned", "mcp_pudding_host_current_pudding_tools", true},
		{"other session", "other", "turn", "owned", permission.ToolCall.Name, false},
		{"previous turn", "session", "old", "owned", permission.ToolCall.Name, false},
		{"revoked connection", "session", "turn", "revoked", permission.ToolCall.Name, false},
		{"previous server", "session", "turn", "owned", "mcp_pudding_host_old_pudding_call", false},
		{"other tool", "session", "turn", "owned", "mcp_pudding_host_current_shell", false},
		{"native title looks like host", "session", "turn", "owned", "run_command", false},
		{"empty name", "session", "turn", "owned", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := permission
			p.ToolCall.Name, p.ToolCall.Title = tc.tool, permission.ToolCall.Name
			out, ok := e.localHostPermission(tc.sid, tc.tid, tc.secret, p)
			if ok != tc.want || ok && (out.Outcome != "selected" || out.OptionID != "one") {
				t.Fatalf("outcome %+v, handled %v", out, ok)
			}
		})
	}
	noOnce := permission
	noOnce.Options = []acp.PermissionOption{{ID: "forever", Kind: "allow_always"}}
	if _, ok := e.localHostPermission("session", "turn", "owned", noOnce); ok {
		t.Fatal("silently selected a persistent permission")
	}
	cancel()
	if _, ok := e.localHostPermission("session", "turn", "owned", permission); ok {
		t.Fatal("cancelled turn allowed gateway")
	}
	if out := e.requestLocalPermission(ctx, "session", "turn", "owned", permission); out.Outcome != "cancelled" {
		t.Fatalf("cancelled request: %+v", out)
	}
	host.turn = nil
	if _, ok := e.localHostPermission("session", "turn", "owned", permission); ok {
		t.Fatal("idle connection allowed gateway")
	}
}
