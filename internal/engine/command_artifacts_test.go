package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

type artifactRecordingRunner struct {
	*tool.BuiltinRunner
	calls []tool.Call
}

func (r *artifactRecordingRunner) Call(ctx context.Context, call tool.Call) tool.Result {
	r.calls = append(r.calls, call)
	return r.BuiltinRunner.Call(ctx, call)
}

func TestSessionArtifactCommandUsesExistingAuthority(t *testing.T) {
	for _, projectID := range []string{"", "project"} {
		t.Run("project="+projectID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			homeDir, projectDir := t.TempDir(), t.TempDir()
			ms := memstore.New()
			if projectID != "" {
				if err := ms.CreateProject(ctx, &store.Project{ID: projectID, RootDirs: []string{projectDir}, ApprovalMode: store.ApprovalAuto}); err != nil {
					t.Fatal(err)
				}
			}
			for _, id := range []string{"session-a", "session-b"} {
				if err := ms.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock", ActiveMode: store.ModeCode, ProjectID: projectID}); err != nil {
					t.Fatal(err)
				}
			}
			stored, err := attachment.NewService(homeDir).StoreReader("session-a", "notes.txt", "text/plain", bytes.NewBufferString("artifact contents"))
			if err != nil {
				t.Fatal(err)
			}
			hub := event.NewHub()
			events, unsubscribe := hub.Subscribe("session-a")
			defer unsubscribe()
			runner := &artifactRecordingRunner{BuiltinRunner: tool.NewBuiltinRunner(tool.WithHomeDir(homeDir))}
			eng := New(ms, hub, registry.Static(mock.New()), ms, WithAttachmentHome(homeDir), WithTools(runner))
			t.Cleanup(eng.Stop)
			execute := func(session, turn, name string, args any) tool.Result {
				raw, _ := json.Marshal(args)
				return eng.executeAllowedTool(ctx, session, turn, store.ModeCode, tool.Call{SessionID: session, TurnID: turn, CallID: "call-" + name, Name: name, Args: raw})
			}
			exported := execute("session-a", "turn-export", tool.AttachmentExport, map[string]any{"scope": "temp", "attachmentKey": stored.AttachmentKey})
			var payload map[string]any
			if !exported.Ok || json.Unmarshal([]byte(exported.Content), &payload) != nil {
				t.Fatalf("export failed: %+v", exported)
			}
			absolute := payload["absolutePath"].(string)
			command := map[string]any{"scope": "project", "command": "cat '" + absolute + "'"}
			result := execute("session-a", "turn-read", tool.CommandRun, command)
			if !result.Ok || !strings.Contains(result.Content, "artifact contents") {
				t.Fatalf("artifact required extra permission or failed: %+v", result)
			}
			last := runner.calls[len(runner.calls)-1]
			artifactDir, exists, err := home.ExistingSessionArtifacts(homeDir, "session-a")
			if err != nil || !exists {
				t.Fatalf("artifact directory unavailable: %v %v", exists, err)
			}
			if len(last.ProjectDirs) != 1 || strings.HasPrefix(last.ProjectDirs[0], artifactDir) || last.CommandSandbox != tool.CommandSandboxEnforce {
				t.Fatalf("artifact changed project or sandbox authority: %+v", last)
			}
			if projectID != "" && last.ProjectDirs[0] != projectDir {
				t.Fatalf("project root changed: %+v", last.ProjectDirs)
			}
			session, _ := ms.GetSession(ctx, "session-a")
			if session.ProjectID != projectID {
				t.Fatalf("artifact created/rebound a project: %+v", session)
			}
			other := execute("session-b", "turn-other", tool.CommandRun, command)
			if other.Ok || !strings.Contains(other.Content, "additional_project_access_required") {
				t.Fatalf("other session inherited artifact authority: %+v", other)
			}
			outside := filepath.Join(t.TempDir(), "private.txt")
			if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(artifactDir, "outside-link")
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			escape := execute("session-a", "turn-escape", tool.CommandRun, map[string]any{"scope": "project", "command": "cat '" + link + "'"})
			if escape.Ok || !strings.Contains(escape.Content, "additional_project_access_required") {
				t.Fatalf("artifact symlink escaped command boundary: %+v", escape)
			}
			for {
				select {
				case ev := <-events:
					if ev.Kind == event.ApprovalRequested {
						t.Fatalf("artifact caused new approval: %+v", ev)
					}
				default:
					return
				}
			}
		})
	}
}
