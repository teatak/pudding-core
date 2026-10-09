package contextbuilder

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/skill"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

type referencePlugins struct {
	defs  []*plugin.Definition
	docs  map[string]string
	err   error
	reads []string
}

// Canonical results stored before plugins were renamed from Apps keep this name.
const legacyPluginLoad = "builtin_app_load"

func (s *referencePlugins) ListDefinitions(context.Context) ([]*plugin.Definition, error) {
	return s.defs, nil
}
func (s *referencePlugins) ReadSkill(_ context.Context, pluginID, skillID string) (*plugin.SkillDetail, error) {
	s.reads = append(s.reads, pluginID+":"+skillID)
	if s.err != nil {
		return nil, s.err
	}
	body, ok := s.docs[skillID]
	if !ok {
		return nil, plugin.ErrNotFound
	}
	return &plugin.SkillDetail{ID: skillID, Name: skillID, Content: body}, nil
}

type referenceSkills struct {
	body  string
	err   error
	reads int
}

func (s *referenceSkills) ReadSkill(_ context.Context, id string) (*skill.Document, error) {
	s.reads++
	if s.err != nil {
		return nil, s.err
	}
	return &skill.Document{Skill: skill.Skill{ID: id}, Content: s.body}, nil
}

func referenceSources() (*referencePlugins, *referenceSkills) {
	return &referencePlugins{defs: []*plugin.Definition{{ID: "demo", Enabled: true, RequiredMode: "work"}}, docs: map[string]string{
		"default": "CURRENT_DEFAULT_BODY", "custom": "CURRENT_CUSTOM_BODY",
	}}, &referenceSkills{body: "CURRENT_GLOBAL_BODY"}
}

func referencePart(name, callID, content string) provider.Part {
	return provider.Part{Type: provider.PartToolResult, Name: name, CallID: callID, Ok: true, Content: content}
}

func TestStoredStudioSkillResolvesCurrentArtifactInstructions(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	if err := st.CreateSession(ctx, &store.Session{ID: "renamed", Provider: "mock", Model: "mock", LoadedPluginIDs: []string{"studio"}}); err != nil {
		t.Fatal(err)
	}
	plugins := &referencePlugins{
		defs: []*plugin.Definition{{ID: "artifacts", Enabled: true, RequiredMode: "chat"}},
		docs: map[string]string{"artifacts": "CURRENT_ARTIFACT_INSTRUCTIONS"},
	}
	b := New(st, nil, WithSkillSources(plugins, nil))
	req := provider.Request{Messages: []provider.Message{{Role: provider.RoleAssistant, Parts: []provider.Part{
		referencePart(tool.PluginLoad, "load", `{"reference":{"kind":"plugin_skill","pluginID":"studio","skillID":"studio"}}`),
	}}}}
	resolved, err := b.ResolveSkillReferences(ctx, "renamed", "chat", req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resolved.Messages[0].Parts[0].Content, "CURRENT_ARTIFACT_INSTRUCTIONS") {
		t.Fatalf("old reference not resolved: %+v", resolved.Messages)
	}
	if !reflect.DeepEqual(plugins.reads, []string{"artifacts:artifacts"}) {
		t.Fatalf("skill reads = %v", plugins.reads)
	}
	// Renaming must not bypass disabled-plugin enforcement.
	plugins.defs[0].Enabled = false
	resolved, err = b.ResolveSkillReferences(ctx, "renamed", "chat", req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(resolved.Messages[0].Parts[0].Content, "CURRENT_ARTIFACT_INSTRUCTIONS") {
		t.Fatal("disabled plugin instructions leaked")
	}
}

func TestResolveSkillReferencesUsesCurrentBodiesAtOriginalPositions(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	if err := st.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock", LoadedPluginIDs: []string{"demo"}}); err != nil {
		t.Fatal(err)
	}
	plugins, skills := referenceSources()
	b := New(st, nil, WithSkillSources(plugins, skills))
	req := provider.Request{System: "UNCHANGED_SYSTEM", Messages: []provider.Message{{Role: provider.RoleAssistant, Parts: []provider.Part{
		referencePart(legacyPluginLoad, "a", `{"ok":true,"appID":"demo","skillID":"default","content":"OLD_APP_BODY"}`),
		{Type: provider.PartText, Text: "a normal task fact"},
		referencePart(tool.SkillRead, "b", `{"ok":true,"id":"writing","content":"OLD_GLOBAL_BODY"}`),
	}}}}
	before, _ := json.Marshal(req)
	resolved, err := b.ResolveSkillReferences(ctx, "s", "work", req)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.System != req.System || len(resolved.Messages[0].Parts) != 3 {
		t.Fatal("reference resolution moved context")
	}
	if !strings.Contains(resolved.Messages[0].Parts[0].Content, "CURRENT_DEFAULT_BODY") || !strings.Contains(resolved.Messages[0].Parts[2].Content, "CURRENT_GLOBAL_BODY") {
		t.Fatalf("not hydrated: %+v", resolved.Messages)
	}
	again, err := b.ResolveSkillReferences(ctx, "s", "work", req)
	if err != nil || !reflect.DeepEqual(again, resolved) {
		t.Fatalf("unchanged source must produce stable context: %v", err)
	}
	plugins.docs["default"], skills.body = "UPDATED_APP_BODY", "UPDATED_GLOBAL_BODY"
	updated, err := b.ResolveSkillReferences(ctx, "s", "work", req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(updated)
	if strings.Contains(string(raw), "OLD_") || strings.Contains(string(raw), "CURRENT_") || !strings.Contains(string(raw), "UPDATED_APP_BODY") || !strings.Contains(string(raw), "UPDATED_GLOBAL_BODY") {
		t.Fatalf("stale body: %s", raw)
	}
	after, _ := json.Marshal(req)
	if string(before) != string(after) {
		t.Fatal("resolution mutated the unresolved source")
	}
	if !strings.Contains(resolved.Messages[0].Parts[0].Content, "CURRENT_DEFAULT_BODY") {
		t.Fatal("later request mutated the prior provider request")
	}
}

func TestResolveSkillReferencesSelectsLatestPluginSkillAndDeduplicates(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	_ = st.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock", LoadedPluginIDs: []string{"demo"}})
	plugins, skills := referenceSources()
	b := New(st, nil, WithSkillSources(plugins, skills))
	req := provider.Request{Messages: []provider.Message{{Role: provider.RoleAssistant, Parts: []provider.Part{
		referencePart(tool.PluginLoad, "default", `{"reference":{"kind":"plugin_skill","pluginID":"demo","skillID":"default"}}`),
		referencePart(tool.SkillRead, "global1", `{"reference":{"kind":"skill","skillID":"writing"}}`),
		referencePart(legacyPluginLoad, "custom", `{"reference":{"kind":"app_skill","appID":"demo","skillID":"custom"}}`),
		referencePart(tool.SkillRead, "global2", `{"reference":{"kind":"skill","skillID":"writing"}}`),
	}}}}
	resolved, err := b.ResolveSkillReferences(ctx, "s", "work", req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(resolved)
	if strings.Contains(string(raw), "CURRENT_DEFAULT_BODY") || strings.Count(string(raw), "CURRENT_CUSTOM_BODY") != 1 || strings.Count(string(raw), "CURRENT_GLOBAL_BODY") != 1 {
		t.Fatalf("wrong selection: %s", raw)
	}
	if !reflect.DeepEqual(plugins.reads, []string{"demo:custom"}) || skills.reads != 1 {
		t.Fatalf("redundant source reads: %+v / %d", plugins.reads, skills.reads)
	}
}

func TestResolveSkillReferencesNeverFallsBackToHistoricalBodies(t *testing.T) {
	for _, scenario := range []string{"unloaded", "disabled", "disconnected", "mode", "missing", "read-error", "global-missing", "invalid"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			st := storetest.New(t)
			loaded := []string{"demo"}
			plugins, skills := referenceSources()
			mode := "work"
			part := referencePart(legacyPluginLoad, "app", `{"appID":"demo","skillID":"custom","content":"HISTORICAL_DO_NOT_USE"}`)
			switch scenario {
			case "unloaded":
				loaded = nil
			case "disabled":
				plugins.defs[0].Enabled = false
			case "disconnected":
				plugins.defs = nil
			case "mode":
				mode = "chat"
			case "missing":
				delete(plugins.docs, "custom")
			case "read-error":
				plugins.err = errors.New("read refused")
			case "global-missing":
				skills.err = skill.ErrNotFound
				part = referencePart(tool.SkillRead, "global", `{"id":"writing","content":"HISTORICAL_DO_NOT_USE"}`)
			case "invalid":
				part.Content = `{"reference":{"kind":"file","skillID":"/private/file"},"content":"HISTORICAL_DO_NOT_USE"}`
			}
			_ = st.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock", LoadedPluginIDs: loaded})
			b := New(st, nil, WithSkillSources(plugins, skills))
			resolved, err := b.ResolveSkillReferences(ctx, "s", mode, provider.Request{Messages: []provider.Message{{Parts: []provider.Part{part}}}})
			if err != nil {
				t.Fatal(err)
			}
			result := resolved.Messages[0].Parts[0]
			if strings.Contains(result.Content, "HISTORICAL_DO_NOT_USE") || strings.Contains(result.Content, `"content"`) {
				t.Fatalf("stale fallback: %+v", result)
			}
			if scenario != "mode" && scenario != "unloaded" && (result.Ok || !strings.Contains(result.Content, "unavailable")) {
				t.Fatalf("missing explicit error: %+v", result)
			}
			if scenario == "invalid" && len(plugins.reads) != 0 {
				t.Fatal("invalid reference reached the reader")
			}
		})
	}
}

func TestSkillReferencesSurviveSQLiteRestartCompactionAndClone(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock", LoadedPluginIDs: []string{"demo"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: "s", TurnID: "t", UserMessageID: "u", ClientMessageID: "c", UserText: "use the custom skill"}); err != nil {
		t.Fatal(err)
	}
	parts := []store.ContentPart{
		{Type: store.ContentPartText, Text: "OLD_SURROUNDING_CONVERSATION"},
		{Type: store.ContentPartToolUse, Name: legacyPluginLoad, CallID: "app", Args: json.RawMessage(`{"app_id":"demo","skill_id":"custom"}`)},
		{Type: store.ContentPartToolResult, Name: legacyPluginLoad, CallID: "app", Ok: true, Content: `{"ok":true,"appID":"demo","skillID":"custom","content":"LEGACY_APP_BODY"}`},
		{Type: store.ContentPartToolUse, Name: tool.SkillRead, CallID: "global", Args: json.RawMessage(`{"skill_id":"writing"}`)},
		{Type: store.ContentPartToolResult, Name: tool.SkillRead, CallID: "global", Ok: true, Content: `{"ok":true,"reference":{"kind":"skill","skillID":"writing"}}`},
	}
	if _, err := db.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "t", Parts: parts}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: "t", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	all, err := db.ListMessages(ctx, "s", 0)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, message := range all {
		ids = append(ids, message.ID)
	}
	if _, err := db.AppendCompactSummary(ctx, store.AppendCompactSummaryInput{ExpectedLastMessageID: all[len(all)-1].ID, SessionID: "s", TurnID: "compact", MessageID: "summary", ClientMessageID: "compact-c", Text: "A historical task summary", Provider: "mock", Model: "mock", Mode: store.ModeWork, Metadata: store.CompactMessageMetadata(ids, nil)}); err != nil {
		t.Fatal(err)
	}
	before, _ := db.ListMessages(ctx, "s", 0)
	beforeJSON, _ := json.Marshal(before)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CloneSession(ctx, store.CloneSessionInput{SourceSessionID: "s", ThroughMessageID: "summary", TargetSessionID: "clone", TitleSuffix: " copy"}); err != nil {
		t.Fatal(err)
	}
	plugins, skills := referenceSources()
	b := New(db, nil, WithSkillSources(plugins, skills))
	for _, sessionID := range []string{"s", "clone"} {
		req, err := b.BuildForProviderWithTools(ctx, sessionID, "mock", "mock", "work", []provider.ToolDef{{Name: tool.PluginLoad}, {Name: tool.SkillRead}})
		if err != nil {
			t.Fatal(err)
		}
		req, err = b.ResolveSkillReferences(ctx, sessionID, "work", req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(req)
		if !strings.Contains(string(raw), "CURRENT_CUSTOM_BODY") || !strings.Contains(string(raw), "CURRENT_GLOBAL_BODY") || strings.Contains(string(raw), "LEGACY_APP_BODY") || strings.Contains(string(raw), "OLD_SURROUNDING_CONVERSATION") {
			t.Fatalf("%s: incorrect restored references: %s", sessionID, raw)
		}
		calls, results := 0, 0
		for _, message := range req.Messages {
			for _, part := range message.Parts {
				if part.Type == provider.PartToolUse {
					calls++
				}
				if part.Type == provider.PartToolResult {
					results++
				}
			}
		}
		if calls != 2 || results != 2 {
			t.Fatalf("unpaired restored references: %d / %d", calls, results)
		}
	}
	after, _ := db.ListMessages(ctx, "s", 0)
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) != string(afterJSON) {
		t.Fatal("old canonical rows were rewritten")
	}
}
