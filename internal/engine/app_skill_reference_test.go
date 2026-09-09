package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/skill"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestAppLoadReturnsSkillReferenceWithoutBody(t *testing.T) {
	ctx := context.Background()
	st := memstore.New()
	if err := st.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	eng := New(st, event.NewHub(), registry.Static(mock.New()), st, WithApps(&mutableAppSource{defs: app.BuiltinDefinitions()}))
	t.Cleanup(eng.Stop)
	result, _ := eng.loadApp(ctx, "s", tool.Call{Name: tool.AppLoad, Args: json.RawMessage(`{"app_id":"computer-use"}`)}, store.ModeWork)
	if !result.Ok || strings.Contains(result.Content, `"content"`) || !strings.Contains(result.Content, `"reference"`) {
		t.Fatalf("expected a body-free App skill reference: %+v", result)
	}
}

type liveSkillApps struct {
	customBody string
}

func (s *liveSkillApps) ListDefinitions(context.Context) ([]*app.Definition, error) {
	return []*app.Definition{{ID: "demo", Name: "Demo", Enabled: true, RequiredMode: "work", DefaultSkillID: "default", Skills: []app.SkillRef{{ID: "default"}, {ID: "custom"}}}}, nil
}

func (s *liveSkillApps) ReadSkill(_ context.Context, appID, id string) (*app.SkillDetail, error) {
	if appID != "demo" {
		return nil, app.ErrNotFound
	}
	switch id {
	case "custom":
		return &app.SkillDetail{ID: id, Content: s.customBody}, nil
	case "default":
		return &app.SkillDetail{ID: id, Content: "DEFAULT_LIVE_INSTRUCTIONS"}, nil
	default:
		return nil, app.ErrNotFound
	}
}

func TestSkillReferencesRefreshBetweenModelStepsWithoutReload(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	skillPath := filepath.Join(home, "skills", "writing", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeSkill := func(body string) error {
		return os.WriteFile(skillPath, []byte("---\nname: writing\ndescription: Write text.\n---\n"+body), 0o600)
	}
	if err := writeSkill("GLOBAL_LIVE_V1"); err != nil {
		t.Fatal(err)
	}
	skills := skill.NewService(home)
	apps := &liveSkillApps{customBody: "CUSTOM_LIVE_V1"}
	var requests []provider.Request
	client := &backgroundEngineClient{step: func(_ context.Context, req provider.Request, step int) (<-chan provider.Chunk, error) {
		requests = append(requests, req)
		raw, _ := json.Marshal(req.Messages)
		body := string(raw)
		check := func(want, reject string) error {
			if !strings.Contains(body, want) || (reject != "" && strings.Contains(body, reject)) {
				return fmt.Errorf("step %d: missing %q or retained %q: %s", step, want, reject, body)
			}
			if strings.Contains(req.System, "LIVE_") {
				return fmt.Errorf("skill body was inserted into system prompt")
			}
			return nil
		}
		switch step {
		case 1:
			return smokeToolStream("load-custom", tool.AppLoad, `{"app_id":"demo","skill_id":"custom"}`), nil
		case 2:
			if err := check("CUSTOM_LIVE_V1", "DEFAULT_LIVE_INSTRUCTIONS"); err != nil {
				return nil, err
			}
			apps.customBody = "CUSTOM_LIVE_V2"
			return smokeToolStream("time-1", tool.TimeGetCurrent, `{}`), nil
		case 3:
			if err := check("CUSTOM_LIVE_V2", "CUSTOM_LIVE_V1"); err != nil {
				return nil, err
			}
			return smokeToolStream("load-default", tool.AppLoad, `{"app_id":"demo"}`), nil
		case 4:
			if err := check("DEFAULT_LIVE_INSTRUCTIONS", "CUSTOM_LIVE_V2"); err != nil {
				return nil, err
			}
			return smokeToolStream("load-global", tool.SkillRead, `{"skill_id":"writing"}`), nil
		case 5:
			if err := check("GLOBAL_LIVE_V1", "CUSTOM_LIVE_V2"); err != nil {
				return nil, err
			}
			if err := writeSkill("GLOBAL_LIVE_V2"); err != nil {
				return nil, err
			}
			return smokeToolStream("time-2", tool.TimeGetCurrent, `{}`), nil
		case 6:
			if err := check("GLOBAL_LIVE_V2", "GLOBAL_LIVE_V1"); err != nil {
				return nil, err
			}
			return smokeToolStream("unload", tool.AppUnload, `{"app_id":"demo"}`), nil
		case 7:
			if err := check("GLOBAL_LIVE_V2", "DEFAULT_LIVE_INSTRUCTIONS"); err != nil {
				return nil, err
			}
			return smokeTextStream("reference test completed"), nil
		default:
			return nil, fmt.Errorf("unexpected extra model step %d", step)
		}
	}}
	st := memstore.New()
	eng := New(st, event.NewHub(), registry.Static(client), st, WithApps(apps), WithSkills(skills), WithTools(tool.NewBuiltinRunner(tool.WithSkills(skills))))
	t.Cleanup(eng.Stop)
	if err := st.CreateSession(ctx, &store.Session{ID: "s", Title: "Reference test", Provider: "mock", Model: "mock", ActiveMode: store.ModeWork, ModeLease: store.ModeLeaseSession}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutProviderProfile(ctx, &store.ProviderProfile{DisplayName: "mock", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "mock", Limits: &store.ModelLimits{MaxToolLoops: 10}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: "s", ClientMessageID: "run", Text: "exercise skill references"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, st, "s")
	messages, err := st.ListMessages(ctx, "s", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 7 {
		raw, _ := json.Marshal(messages)
		t.Fatalf("loop stopped early: requests=%d messages=%s", len(requests), raw)
	}
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Type == store.ContentPartToolResult && (part.Name == tool.AppLoad || part.Name == tool.SkillRead) && (strings.Contains(part.Content, `"content"`) || !strings.Contains(part.Content, `"reference"`)) {
				t.Fatalf("saved a body instead of a reference: %+v", part)
			}
		}
	}
	// Later projections must not mutate earlier request snapshots.
	raw, _ := json.Marshal(requests[1].Messages)
	if !strings.Contains(string(raw), "CUSTOM_LIVE_V1") || strings.Contains(string(raw), "CUSTOM_LIVE_V2") {
		t.Fatal("old provider request was mutated")
	}
}

func TestCompactionProjectsReferencesInsteadOfSkillBodies(t *testing.T) {
	parts := []store.ContentPart{{Type: store.ContentPartText, Text: "KEEP_TASK_FACT"},
		{Type: store.ContentPartToolResult, Name: tool.AppLoad, Ok: true, Content: `{"appID":"demo","skillID":"custom","content":"DO_NOT_SUMMARIZE_OLD_RULES"}`},
		{Type: store.ContentPartToolResult, Name: tool.SkillRead, Ok: true, Content: `{"id":"writing","content":"DO_NOT_SUMMARIZE_GLOBAL_RULES"}`},
	}
	message := &store.Message{ID: "m", Role: store.RoleTool, Parts: parts, Text: store.MessageTextFromParts(parts)}
	before, _ := json.Marshal(message)
	dump := compactHistoryDump([]*store.Message{message})
	if !strings.Contains(dump, "KEEP_TASK_FACT") || strings.Count(dump, `"reference"`) != 2 || strings.Contains(dump, "DO_NOT_SUMMARIZE") {
		t.Fatalf("incorrect compaction input: %s", dump)
	}
	after, _ := json.Marshal(message)
	if string(before) != string(after) {
		t.Fatal("compaction rewrote canonical history")
	}
}
