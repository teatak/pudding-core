package engine

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/teatak/pudding-core/internal/acp"
	"github.com/teatak/pudding-core/internal/attachment"
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

func TestLocalAgentAttachmentInputs(t *testing.T) {
	home := t.TempDir()
	e := &Engine{attachmentHome: home}
	t.Run("directory and project references", func(t *testing.T) {
		folder := store.LocalFolder{ID: "folder", Name: "Shared", Path: filepath.Join(home, "shared folder"), Origin: "picker"}
		reference := store.ProjectReference{ID: "ref", Name: "main.go", Path: "main.go", SourcePath: filepath.Join(home, "main.go"), RootID: "root", Kind: "file", StartLine: 3, StartColumn: 1, EndLine: 5, EndColumn: 4}
		blocks, err := e.localPrompt("acp", []store.ContentPart{store.LocalFolderPart(folder), store.ProjectReferencePart(reference)}, "", &store.LocalAgentState{})
		if err != nil || len(blocks) != 2 {
			t.Fatalf("reference inputs: %+v %v", blocks, err)
		}
		_, folders, _ := strings.Cut(blocks[0].Text, "\n")
		folders, _, _ = strings.Cut(folders, "\n")
		_, references, _ := strings.Cut(blocks[1].Text, "\n")
		references, _, _ = strings.Cut(references, "\n")
		var folderData struct{ Folders []store.LocalFolder }
		var referenceData struct{ References []store.ProjectReference }
		if json.Unmarshal([]byte(folders), &folderData) != nil || len(folderData.Folders) != 1 || folderData.Folders[0] != folder {
			t.Fatalf("directory identity or path lost: %s", blocks[0].Text)
		}
		if json.Unmarshal([]byte(references), &referenceData) != nil || len(referenceData.References) != 1 || referenceData.References[0] != reference {
			t.Fatalf("project reference identity, path or selection lost: %s", blocks[1].Text)
		}
	})
	for _, tc := range []struct {
		name, mime, data, wantType string
		capabilities               store.LocalAgentState
	}{
		{"report.pdf", "application/pdf", "%PDF-1.4\nPDF fixture", "text", store.LocalAgentState{EmbeddedContext: true}},
		{"recording.wav", "audio/wav", "RIFF fixture", "text", store.LocalAgentState{}},
		{"image.png", "image/png", "image fixture", "text", store.LocalAgentState{}},
		{"inline.png", "image/png", "image fixture", "image", store.LocalAgentState{Image: true}},
		{"notes.txt", "text/plain", "attachment text", "resource", store.LocalAgentState{EmbeddedContext: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := attachment.NewService(home).StoreReader("acp", tc.name, tc.mime, strings.NewReader(tc.data))
			if err != nil {
				t.Fatal(err)
			}
			part := store.AttachmentPart(file)
			blocks, err := e.localPrompt("acp", []store.ContentPart{part}, "", &tc.capabilities)
			if err != nil || len(blocks) != 1 || blocks[0].Type != tc.wantType {
				t.Fatalf("attachment input: %+v, %v", blocks, err)
			}
			if tc.wantType == "text" {
				for _, want := range []string{file.Name, file.MIME, file.AttachmentKey, "builtin_attachment_export", "scope=temp"} {
					if !strings.Contains(blocks[0].Text, want) {
						t.Fatalf("missing usable attachment reference %q: %s", want, blocks[0].Text)
					}
				}
				if strings.Contains(blocks[0].Text, tc.data) || strings.Contains(blocks[0].Text, home) {
					t.Fatalf("fallback exposed binary content or private storage path: %s", blocks[0].Text)
				}
			}
			if err := attachment.NewService(home).Delete("acp", file.AttachmentKey); err != nil {
				t.Fatal(err)
			}
			if _, err := e.localPrompt("acp", []store.ContentPart{part}, "", &tc.capabilities); err == nil {
				t.Fatal("missing attachment must still fail instead of sending an unusable reference")
			}
		})
	}
}

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
	assistantText := func(id string) string {
		t.Helper()
		turn, err := ms.GetConversationTurn(ctx, "acp", id)
		if err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		for _, m := range turn.Messages {
			if m.Role == store.RoleAssistant {
				text.WriteString(store.TextFromParts(m.Parts))
			}
		}
		return text.String()
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
	// A visible widget/browser accompanies ordinary follow-up messages. Keep
	// the structured canonical context and send its neutral text to ACP.
	for _, ui := range []store.ContentPart{
		{Type: store.ContentPartUIContext, Surface: "studio", Resource: "studio_item", CallID: "widget_todo", Name: "Todo", ResourceKind: "widget"},
		{Type: store.ContentPartUIContext, Surface: "browser", Resource: "browser_tab", CallID: "tab_1", URL: "https://example.com/", SelectionText: "selected browser text", ResourceKind: "webview"},
	} {
		out, err := e.Submit(ctx, SubmitInput{SessionID: "acp", ClientMessageID: "ui-" + ui.Surface,
			Parts: []store.ContentPart{{Type: store.ContentPartText, Text: "continue with the visible item"}, ui}})
		if err != nil {
			t.Fatalf("%s UI context rejected: %v", ui.Surface, err)
		}
		e.Wait()
		turn, err := ms.GetConversationTurn(ctx, "acp", out.TurnID)
		if err != nil || turn.Status != store.TurnCompleted {
			t.Fatalf("UI context turn: %+v, %v", turn, err)
		}
		var response strings.Builder
		saved := false
		for _, message := range turn.Messages {
			if message.Role == store.RoleAssistant {
				response.WriteString(store.TextFromParts(message.Parts))
			}
			if message.Role == store.RoleUser {
				for _, part := range message.Parts {
					if part.Type == store.ContentPartUIContext && part.CallID == ui.CallID && part.Surface == ui.Surface && part.SelectionText == ui.SelectionText {
						saved = true
					}
				}
			}
		}
		_, payload, found := strings.Cut(response.String(), "<ui-context>")
		payload, _, closed := strings.Cut(payload, "</ui-context>")
		var got map[string]string
		if !saved || !found || !closed || json.Unmarshal([]byte(payload), &got) != nil ||
			got["surface"] != ui.Surface || got["resource"] != ui.Resource || got["id"] != ui.CallID ||
			got["kind"] != ui.ResourceKind || got["name"] != ui.Name || got["url"] != ui.URL || got["selectionText"] != ui.SelectionText {
			t.Fatalf("UI context lost between Submit, ACP prompt and history: saved=%v response=%q", saved, response.String())
		}
	}
	// Deduplication precedes CLI preparation, even when the old binary disappears.
	e.closeLocalAgents("acp")
	e.localAgentPaths = []string{"/does/not/exist"}
	duplicate := submit("first", "different text")
	if !duplicate.Duplicate || duplicate.TurnID != first.TurnID {
		t.Fatalf("duplicate: %+v", duplicate)
	}
	failedStart := submit("failed-start", "Remember STARTUP_HISTORY_481")
	e.Wait()
	checkTurn(failedStart.TurnID, store.TurnFailed, "STARTUP_HISTORY_481")
	turn, queued, err := ms.LookupSubmission(ctx, "acp", "failed-start")
	if err != nil || turn == nil || turn.ID != failedStart.TurnID || queued != nil {
		t.Fatal("preparation failure lost the accepted input")
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
	checkTurn(second.TurnID, store.TurnCompleted, "again")
	if text := assistantText(second.TurnID); !strings.Contains(text, "STARTUP_HISTORY_481") || !strings.HasSuffix(text, "again") {
		t.Fatalf("new message did not receive failed startup history: %s", text)
	}
	third := submit("third", "only the new request")
	e.Wait()
	if text := assistantText(third.TurnID); strings.Contains(text, "STARTUP_HISTORY_481") || !strings.Contains(text, "flash/low: only the new request") {
		t.Fatalf("acknowledged history sent again: %s", text)
	}
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
	checkTurn(promoted.ID, store.TurnCompleted, "next")
	crash := submit("crash", "crash")
	e.Wait()
	checkTurn(crash.TurnID, store.TurnFailed, "crash")
	continued := submit("after-crash", "continue without pressing retry")
	e.Wait()
	checkTurn(continued.TurnID, store.TurnCompleted, "continue without pressing retry")
	if text := assistantText(continued.TurnID); !strings.Contains(text, `"text":"crash"`) {
		t.Fatalf("failed RPC input missing from follow-up context: %s", text)
	}
	crash = submit("crash-for-retry", "crash")
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
	// Configuration failures leave a failed turn, not an unsent draft.
	bad := "invalid"
	_, _ = ms.UpdateSession(ctx, "acp", store.SessionUpdate{Model: &bad})
	badConfig := submit("badmodel", "Remember CONFIG_HISTORY_926")
	e.Wait()
	checkTurn(badConfig.TurnID, store.TurnFailed, "CONFIG_HISTORY_926")
	turn, _, _ = ms.LookupSubmission(ctx, "acp", "badmodel")
	if turn == nil || turn.ID != badConfig.TurnID {
		t.Fatal("bad config lost its user message")
	}
	good := "flash"
	_, _ = ms.UpdateSession(ctx, "acp", store.SessionUpdate{Model: &good})
	configRecovery := submit("after-config", "use the earlier code")
	e.Wait()
	checkTurn(configRecovery.TurnID, store.TurnCompleted, "CONFIG_HISTORY_926")
	if text := assistantText(configRecovery.TurnID); !strings.Contains(text, "CONFIG_HISTORY_926") {
		t.Fatalf("failed configuration input missing from follow-up context: %s", text)
	}
	// CWD changes require an explicit, editable handoff before creating context.
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
	failedHandoff, err := e.Submit(ctx, SubmitInput{SessionID: "acp", ClientMessageID: "handoff", Text: "continue", LocalAgentHandoff: &edited})
	if err != nil {
		t.Fatal(err)
	}
	e.Wait()
	checkTurn(failedHandoff.TurnID, store.TurnFailed, edited)
	_, _ = ms.UpdateSession(ctx, "acp", store.SessionUpdate{Model: &good})
	confirmed, err := e.Submit(ctx, SubmitInput{SessionID: "acp", ClientMessageID: "after-handoff", Text: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	e.Wait()
	checkTurn(confirmed.TurnID, store.TurnCompleted, edited)
	if text := assistantText(confirmed.TurnID); strings.Contains(text, "STARTUP_HISTORY_481") || !strings.Contains(text, edited) {
		t.Fatalf("reviewed handoff lost or omitted history reintroduced: %s", text)
	}
	state, _ = ms.GetLocalAgentState(ctx, "acp")
	if state.AgentSessionID == originalID || state.CWD != project.RootDirs[0] {
		t.Fatalf("handoff state: %+v", state)
	}
	e.closeLocalAgents("acp")
	if err = os.WriteFile(filepath.Join(state.CWD, "restore-fails"), []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	restoreFailure := submit("restore-failed", "continue")
	e.Wait()
	checkTurn(restoreFailure.TurnID, store.TurnFailed, "continue")
	// The next send can review a new context, even after restarting Pudding.
	e.Stop()
	e.Wait()
	e = makeEngine()
	_, err = e.Submit(ctx, SubmitInput{SessionID: "acp", ClientMessageID: "fresh-reset", Text: "continue"})
	if !errors.As(err, &handoff) {
		t.Fatalf("restore failure must offer reviewed handoff: %v", err)
	}
	empty := ""
	reset, err := e.Submit(ctx, SubmitInput{SessionID: "acp", ClientMessageID: "fresh-reset", Text: "fresh context", LocalAgentHandoff: &empty})
	if err != nil {
		t.Fatal(err)
	}
	e.Wait()
	checkTurn(reset.TurnID, store.TurnCompleted, "fresh context")
	if text := assistantText(reset.TurnID); strings.Contains(text, edited) || strings.Contains(text, "Earlier Pudding conversation history") {
		t.Fatalf("explicit fresh context included previous history: %s", text)
	}
	// Even the very first message survives failure before an ACP session exists.
	if err := ms.CreateSession(ctx, &store.Session{ID: "first-failure", Provider: acp.Provider, Model: "flash"}); err != nil {
		t.Fatal(err)
	}
	e.localAgentPaths = []string{"/does/not/exist"}
	initial, err := e.Submit(ctx, SubmitInput{SessionID: "first-failure", ClientMessageID: "first", Text: "Remember FIRST_HISTORY_739"})
	if err != nil {
		t.Fatal(err)
	}
	e.Wait()
	initialTurn, err := ms.GetConversationTurn(ctx, "first-failure", initial.TurnID)
	if err != nil || initialTurn.Status != store.TurnFailed || store.TextFromParts(initialTurn.Messages[0].Parts) != "Remember FIRST_HISTORY_739" {
		t.Fatalf("first failed message was withdrawn: %+v %v", initialTurn, err)
	}
	e.localAgentPaths = []string{binary}
	followup, err := e.Submit(ctx, SubmitInput{SessionID: "first-failure", ClientMessageID: "second", Text: "what was the code?"})
	if err != nil {
		t.Fatal(err)
	}
	e.Wait()
	followupTurn, err := ms.GetConversationTurn(ctx, "first-failure", followup.TurnID)
	if err != nil || followupTurn.Status != store.TurnCompleted {
		t.Fatalf("follow-up failed: %+v %v", followupTurn, err)
	}
	foundHistory := false
	for _, message := range followupTurn.Messages {
		if message.Role == store.RoleAssistant && strings.Contains(store.TextFromParts(message.Parts), "FIRST_HISTORY_739") {
			foundHistory = true
		}
	}
	if !foundHistory {
		t.Fatal("first failed message missing from ACP context")
	}
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
	secret := ""
	var turn *LocalHostTurn
	var release func()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		e.localMu.Lock()
		for key, host := range e.localHosts {
			if host.sessionID == "host" && host.turn != nil && host.turn.TurnID == first.TurnID {
				secret = key
			}
		}
		e.localMu.Unlock()
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

func TestLocalAgentToolCallUpdateRawInput(t *testing.T) {
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
	hub := event.NewHub()
	e := New(ms, hub, registry.Static(mock.New()), ms, WithAttachmentHome(t.TempDir()))
	e.localAgentPaths = []string{binary}
	defer func() { e.Stop(); e.Wait() }()
	if err := ms.CreateSession(ctx, &store.Session{ID: "acp-tool", Provider: acp.Provider, Model: "flash"}); err != nil {
		t.Fatal(err)
	}
	sub, unsubscribe := hub.Subscribe("acp-tool")
	defer unsubscribe()

	decodeArgsMap := func(t *testing.T, raw []byte, label string) map[string]any {
		t.Helper()
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("%s invalid JSON: %q (%v)", label, string(raw), err)
		}
		return got
	}
	drainToolEvents := func(turnID string) []event.Event {
		var out []event.Event
		for {
			select {
			case ev, ok := <-sub:
				if !ok {
					return out
				}
				if ev.Kind == event.TurnTool && ev.TurnID == turnID {
					out = append(out, ev)
				}
			default:
				return out
			}
		}
	}

	// 1) tool_call (no args) -> thought -> rawInput -> full snapshot replacement (drops old key) -> null -> completed (omitted)
	out, err := e.Submit(ctx, SubmitInput{SessionID: "acp-tool", ClientMessageID: "tool-raw-input", Text: "tool-update-raw-input"})
	if err != nil {
		t.Fatal(err)
	}
	e.Wait()

	turn, err := ms.GetConversationTurn(ctx, "acp-tool", out.TurnID)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Status != store.TurnCompleted {
		t.Fatalf("turn status %s: %s", turn.Status, turn.Error)
	}

	var toolUses []store.ContentPart
	var toolResults []store.ContentPart
	sawThought := false
	for _, msg := range turn.Messages {
		for _, part := range msg.Parts {
			switch part.Type {
			case store.ContentPartThought:
				if strings.Contains(part.Text, "Inspecting target file") {
					sawThought = true
				}
			case store.ContentPartToolUse:
				toolUses = append(toolUses, part)
			case store.ContentPartToolResult:
				toolResults = append(toolResults, part)
			}
		}
	}
	if !sawThought {
		t.Fatal("expected thought part to be persisted")
	}
	if len(toolUses) != 1 || len(toolResults) != 1 {
		t.Fatalf("expected 1 tool_use and 1 tool_result, got tool_use=%d tool_result=%d", len(toolUses), len(toolResults))
	}
	if toolUses[0].CallID != "acp:tool-1" || toolUses[0].Name != "antigravity.read_file" {
		t.Fatalf("unexpected canonical tool_use identity: %+v", toolUses[0])
	}
	savedArgs := decodeArgsMap(t, toolUses[0].Args, "canonical tool_use.Args")
	if len(savedArgs) != 2 || savedArgs["path"] != "README.md" || savedArgs["startLine"] != float64(1) {
		t.Errorf("canonical tool_use.Args did not match replaced snapshot: got %s, want {\"path\":\"README.md\",\"startLine\":1}", string(toolUses[0].Args))
	}

	toolEvents := drainToolEvents(out.TurnID)
	var argsSnapshots []string
	okCount := 0
	for _, ev := range toolEvents {
		if ev.CallID != "acp:tool-1" {
			t.Errorf("unexpected extra tool CallID in SSE: %q", ev.CallID)
		}
		if ev.ArgsText != "" {
			argsSnapshots = append(argsSnapshots, ev.ArgsText)
		}
		if ev.Phase == "ok" && ev.Ok != nil && *ev.Ok {
			okCount++
		}
	}
	if okCount != 1 {
		t.Errorf("expected exactly 1 completed turn.tool SSE event, got %d (events=%+v)", okCount, toolEvents)
	}
	if len(argsSnapshots) != 3 {
		t.Fatalf("expected 3 SSE args snapshots (initial {}, first rawInput, replaced snapshot), got %d: %v", len(argsSnapshots), argsSnapshots)
	}
	if first := decodeArgsMap(t, []byte(argsSnapshots[0]), "first SSE ArgsText"); len(first) != 0 {
		t.Errorf("expected initial empty args snapshot, got %s", argsSnapshots[0])
	}
	if second := decodeArgsMap(t, []byte(argsSnapshots[1]), "second SSE ArgsText"); second["path"] != "OLD.md" || second["obsolete"] != true {
		t.Errorf("unexpected second SSE args snapshot: %s", argsSnapshots[1])
	}
	lastSSEArgs := decodeArgsMap(t, []byte(argsSnapshots[len(argsSnapshots)-1]), "last SSE ArgsText")
	if len(lastSSEArgs) != 2 || lastSSEArgs["path"] != "README.md" || lastSSEArgs["startLine"] != float64(1) {
		t.Errorf("SSE turn.tool did not retain replaced snapshot: got %s, want {\"path\":\"README.md\",\"startLine\":1}", argsSnapshots[len(argsSnapshots)-1])
	}

	// 2) Both missing final updates and agy's failed-on-cancel updates close once
	// with the latest input, preserving raw failures but marking their cancellation.
	for _, prompt := range []string{"tool-update-cancel", "tool-update-cancel-failed"} {
		cancelled, err := e.Submit(ctx, SubmitInput{SessionID: "acp-tool", ClientMessageID: prompt, Text: prompt})
		if err != nil {
			t.Fatal(err)
		}
		var cancelToolEvents []event.Event
		deadline := time.After(5 * time.Second)
		ready := false
		for !ready {
			select {
			case ev, ok := <-sub:
				if !ok {
					t.Fatal("event subscription closed before CANCEL_READY")
				}
				if ev.TurnID != cancelled.TurnID {
					continue
				}
				if ev.Kind == event.TurnTool {
					cancelToolEvents = append(cancelToolEvents, ev)
				}
				if ev.Kind == event.TurnDelta && ev.Delta == "CANCEL_READY" {
					ready = true
				}
			case <-deadline:
				_ = e.Cancel("acp-tool")
				t.Fatal("timed out waiting for CANCEL_READY")
			}
		}
		if err := e.Cancel("acp-tool"); err != nil {
			t.Fatal(err)
		}
		e.Wait()
		cancelToolEvents = append(cancelToolEvents, drainToolEvents(cancelled.TurnID)...)

		cancelTurn, err := ms.GetConversationTurn(ctx, "acp-tool", cancelled.TurnID)
		if err != nil {
			t.Fatal(err)
		}
		if cancelTurn.Status != store.TurnCancelled {
			t.Fatalf("cancelled turn status %s: %s", cancelTurn.Status, cancelTurn.Error)
		}
		var cancelUses []store.ContentPart
		var cancelResults []store.ContentPart
		for _, msg := range cancelTurn.Messages {
			for _, part := range msg.Parts {
				switch part.Type {
				case store.ContentPartToolUse:
					cancelUses = append(cancelUses, part)
				case store.ContentPartToolResult:
					cancelResults = append(cancelResults, part)
				}
			}
		}
		if len(cancelUses) != 1 || len(cancelResults) != 1 {
			t.Fatalf("cancelled turn expected 1 tool_use and 1 tool_result, got tool_use=%d tool_result=%d", len(cancelUses), len(cancelResults))
		}
		if cancelUses[0].CallID != "acp:tool-cancel" || cancelResults[0].CallID != "acp:tool-cancel" || cancelResults[0].Ok || cancelResults[0].SummaryKind != tool.SummaryCancelled {
			t.Fatalf("unexpected cancelled tool records: use=%+v result=%+v", cancelUses[0], cancelResults[0])
		}
		if prompt == "tool-update-cancel" {
			if cancelResults[0].Content != "cancelled" {
				t.Fatalf("missing cancelled result: %+v", cancelResults[0])
			}
		} else {
			var raw acp.Update
			if json.Unmarshal([]byte(cancelResults[0].Content), &raw) != nil || raw.Status != "failed" {
				t.Fatalf("raw ACP cancellation update was lost: %+v", cancelResults[0])
			}
		}
		cancelArgs := decodeArgsMap(t, cancelUses[0].Args, "cancelled canonical tool_use.Args")
		if len(cancelArgs) != 1 || cancelArgs["path"] != "final.md" {
			t.Errorf("cancelled canonical tool_use.Args did not retain latest snapshot: got %s, want {\"path\":\"final.md\"}", string(cancelUses[0].Args))
		}

		var cancelLastArgs string
		cancelErrCount := 0
		for _, ev := range cancelToolEvents {
			if ev.CallID != "acp:tool-cancel" {
				t.Errorf("unexpected CallID in cancelled turn SSE: %q", ev.CallID)
			}
			if ev.ArgsText != "" {
				cancelLastArgs = ev.ArgsText
			}
			if ev.Phase == "error" && ev.Ok != nil && !*ev.Ok {
				if ev.SummaryKind != tool.SummaryCancelled {
					t.Fatalf("SSE lost cancellation: %+v", ev)
				}
				cancelErrCount++
			}
		}
		if cancelErrCount != 1 {
			t.Errorf("expected exactly 1 error turn.tool SSE event on cancel, got %d (events=%+v)", cancelErrCount, cancelToolEvents)
		}
		cancelSSEArgs := decodeArgsMap(t, []byte(cancelLastArgs), "cancelled last SSE ArgsText")
		if len(cancelSSEArgs) != 1 || cancelSSEArgs["path"] != "final.md" {
			t.Errorf("cancelled SSE turn.tool did not retain latest snapshot: got %s, want {\"path\":\"final.md\"}", cancelLastArgs)
		}
	}

	// A background tool may finish after the answer. Its use must retain the
	// original position, so history merges the late result into that row.
	late, err := e.Submit(ctx, SubmitInput{SessionID: "acp-tool", ClientMessageID: "late-failure", Text: "tool-late-failure"})
	if err != nil {
		t.Fatal(err)
	}
	e.Wait()
	lateTurn, err := ms.GetConversationTurn(ctx, "acp-tool", late.TurnID)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	failedResults := 0
	for _, msg := range lateTurn.Messages {
		for _, part := range msg.Parts {
			if part.Type == store.ContentPartToolUse {
				order = append(order, part.CallID)
			}
			if msg.Role == store.RoleAssistant && part.Type == store.ContentPartText {
				order = append(order, part.Text)
			}
			if part.Type == store.ContentPartToolResult && part.CallID == "acp:slow-command" && !part.Ok {
				failedResults++
			}
		}
	}
	if strings.Join(order, "|") != "acp:slow-command|acp:quick-read|Answer before background command finishes" || failedResults != 1 || lateTurn.Status != store.TurnCompleted {
		t.Fatalf("late failure moved the tool or lost its outcome: order=%v failures=%d status=%s", order, failedResults, lateTurn.Status)
	}
}

// Preserve one Pudding turn while agy completes each superseded prompt RPC.
// Exercise the observed wire order, direct/queued input, usage and cancellation.
func TestLocalAgentSteering(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "agy")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if output, err := exec.Command("go", "build", "-o", binary, "./testdata/acp").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	for _, scenario := range []string{"finish", "cancel", "permission"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := WithRequestSource(context.Background(), "desktop")
			ms := storetest.New(t)
			hub := event.NewHub()
			e := New(ms, hub, registry.Static(mock.New()), ms, WithAttachmentHome(t.TempDir()))
			e.localAgentPaths = []string{binary}
			defer func() { e.Stop(); e.Wait() }()
			if err := ms.CreateSession(ctx, &store.Session{ID: "steer", Provider: acp.Provider, Model: "flash"}); err != nil {
				t.Fatal(err)
			}
			events, unsubscribe := hub.Subscribe("steer")
			defer unsubscribe()
			wait := func(kind event.Kind, text string) {
				t.Helper()
				timer := time.NewTimer(5 * time.Second)
				defer timer.Stop()
				for {
					select {
					case ev := <-events:
						if ev.Kind == kind && (text == "" || ev.Delta == text) {
							return
						}
					case <-timer.C:
						t.Fatalf("missing %s %s", kind, text)
					}
				}
			}
			prompt := "steering-start"
			if scenario == "permission" {
				prompt = "permission-test"
			}
			first, err := e.Submit(ctx, SubmitInput{SessionID: "steer", ClientMessageID: "first", Text: prompt})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "permission" {
				wait(event.ApprovalRequested, "")
			} else {
				wait(event.TurnDelta, "STEERING_READY")
			}
			queued, err := e.Submit(ctx, SubmitInput{SessionID: "steer", ClientMessageID: "queued", Text: "steer-hold"})
			if err != nil || !queued.Queued {
				t.Fatalf("queue: %+v %v", queued, err)
			}
			// A remote caller cannot use either steer endpoint to drive this local agent.
			remote := WithRequestSource(ctx, "remote")
			if _, err := e.Steer(remote, SteerInput{SessionID: "steer", TurnID: first.TurnID, ClientMessageID: "remote", Text: "steer-finish"}); !errors.Is(err, ErrLocalAgentDesktop) {
				t.Fatalf("remote steer: %v", err)
			}
			if _, err := e.SteerQueuedInput(remote, "steer", first.TurnID, "queued"); !errors.Is(err, ErrLocalAgentDesktop) {
				t.Fatalf("remote queue steer: %v", err)
			}
			if scenario == "permission" {
				if _, err := e.SteerQueuedInput(ctx, "steer", first.TurnID, "queued"); !errors.Is(err, acp.ErrSteerBlocked) {
					t.Fatalf("pending permission: %v", err)
				}
				if _, err := e.Steer(ctx, SteerInput{SessionID: "steer", TurnID: first.TurnID, ClientMessageID: "direct", Text: "steer-finish"}); !errors.Is(err, acp.ErrSteerBlocked) {
					t.Fatalf("pending permission direct: %v", err)
				}
				inputs, err := ms.ListQueuedInputs(ctx, "steer")
				if err != nil || len(inputs) != 1 || inputs[0].Status != store.QueuedInputQueued {
					t.Fatalf("lost queue: %+v %v", inputs, err)
				}
				turn, _ := ms.GetConversationTurn(ctx, "steer", first.TurnID)
				for _, msg := range turn.Messages {
					if msg.ClientMessageID == "queued" || msg.ClientMessageID == "direct" {
						t.Fatal("rejected steer entered history")
					}
				}
				// Keep the queued message from auto-running in this rejection-only case.
				cancelledStatus := store.QueuedInputCancelled
				if _, err := ms.UpdateQueuedInput(ctx, store.UpdateQueuedInputInput{SessionID: "steer", ClientMessageID: "queued", Status: &cancelledStatus}); err != nil {
					t.Fatal(err)
				}
				if err := e.Cancel("steer"); err != nil {
					t.Fatal(err)
				}
				e.Wait()
				return
			}
			result, err := e.SteerQueuedInput(ctx, "steer", first.TurnID, "queued")
			if err != nil || result.TurnID != first.TurnID {
				t.Fatalf("queue steer: %+v %v", result, err)
			}
			wait(event.TurnDelta, "ACCEPTED_steer-hold")
			turn, _ := ms.GetConversationTurn(ctx, "steer", first.TurnID)
			if turn.Status != store.TurnRunning {
				t.Fatalf("old RPC closed turn early: %s", turn.Status)
			}
			duplicate, err := e.SteerQueuedInput(ctx, "steer", first.TurnID, "queued")
			if err != nil || !duplicate.Duplicate {
				t.Fatalf("duplicate: %+v %v", duplicate, err)
			}
			if scenario == "cancel" {
				if err := e.Cancel("steer"); err != nil {
					t.Fatal(err)
				}
			} else {
				result, err = e.Steer(ctx, SteerInput{SessionID: "steer", TurnID: first.TurnID, ClientMessageID: "last", Text: "steer-finish"})
				if err != nil || result.TurnID != first.TurnID {
					t.Fatalf("direct steer: %+v %v", result, err)
				}
			}
			e.Wait()
			turns, err := ms.ListTurnsPage(ctx, "steer", "", 10)
			if err != nil || len(turns.Turns) != 1 {
				t.Fatalf("extra turns: %+v %v", turns, err)
			}
			turn = turns.Turns[0]
			want := store.TurnCompleted
			if scenario == "cancel" {
				want = store.TurnCancelled
			}
			if turn.Status != want {
				t.Fatalf("status %s: %s", turn.Status, turn.Error)
			}
			inputs, _ := ms.ListQueuedInputs(ctx, "steer")
			if len(inputs) != 0 {
				t.Fatalf("queue not consumed: %+v", inputs)
			}
			users, toolUses, toolResults := 0, 0, 0
			for _, m := range turn.Messages {
				if m.Role == store.RoleUser {
					users++
				}
				for _, p := range m.Parts {
					if p.Type == store.ContentPartToolUse {
						toolUses++
					}
					if p.Type == store.ContentPartToolResult {
						toolResults++
						if scenario == "finish" && !p.Ok {
							t.Fatalf("tool prematurely failed: %+v", p)
						}
					}
				}
			}
			wantUsers := 3
			if scenario == "cancel" {
				wantUsers = 2
			}
			if users != wantUsers || toolUses != 1 || toolResults != 1 {
				t.Fatalf("duplicate/missing records: users=%d tools=%d results=%d", users, toolUses, toolResults)
			}
			if scenario == "finish" {
				usage, _ := ms.SessionUsage(ctx, "steer")
				if usage.LastInputUncachedTokens != 27 || usage.LastOutputContentTokens != 7 || usage.RequestCount != 3 {
					t.Fatalf("lost usage: %+v", usage)
				}
			}
		})
	}
}
