package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestDirectoryEntryPointsPreserveTrailingWhitespace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not preserve trailing directory spaces")
	}
	for _, entry := range []string{"project", "session", "turn-approval", "session-approval"} {
		t.Run(entry, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			selected, neighbor := filepath.Join(base, "project "), filepath.Join(base, "project")
			for path, content := range map[string]string{selected: "selected", neighbor: "neighbor"} {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "file.txt"), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			st := storetest.New(t)
			session := &store.Session{ID: "session", Provider: "mock", Model: "mock", ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession}
			if entry != "session" {
				root := t.TempDir()
				if entry == "project" {
					root = selected
				}
				if err := st.CreateProject(ctx, &store.Project{ID: "project", RootDirs: []string{root}}); err != nil {
					t.Fatal(err)
				}
				session.ProjectID = "project"
			}
			if err := st.CreateSession(ctx, session); err != nil {
				t.Fatal(err)
			}
			hub := event.NewHub()
			eng := New(st, hub, registry.Static(mock.New()), st)
			t.Cleanup(eng.Stop)
			if entry == "session" {
				if got, err := eng.SetSessionDirectories(ctx, "session", []string{selected}); err != nil || !reflect.DeepEqual(got, []string{selected}) {
					t.Fatalf("directory selection changed: %q %v", got, err)
				}
			}
			if entry == "turn-approval" || entry == "session-approval" {
				events, unsubscribe := hub.Subscribe("session")
				defer unsubscribe()
				args, _ := json.Marshal(map[string]any{"targetMode": "code", "reason": "inspect selected directory", "projectDirs": []string{selected}})
				done := make(chan tool.Result, 1)
				go func() {
					result, _, _ := eng.requestCapabilityApproval(ctx, "session", "turn", tool.Call{SessionID: "session", TurnID: "turn", CallID: "grant", Name: tool.RequestCapability, Args: args}, store.ModeCode)
					done <- result
				}()
				var pending event.Event
				for pending.Kind != event.ApprovalRequested {
					select {
					case pending = <-events:
					case result := <-done:
						t.Fatalf("directory request completed without approval: %+v", result)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				scope := ApprovalScopeTurn
				var picked []string
				if entry == "session-approval" {
					scope, picked = ApprovalScopeSession, []string{selected}
				}
				if err := eng.ApproveApproval(ctx, "session", pending.ApprovalID, scope, picked); err != nil {
					t.Fatal(err)
				}
				select {
				case result := <-done:
					if !result.Ok {
						t.Fatal(result.Content)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				access, err := eng.DirectoryAccess(ctx, "session", "turn")
				if err != nil {
					t.Fatal(err)
				}
				granted := access.Turn
				if scope == ApprovalScopeSession {
					granted = access.Session
				}
				if !reflect.DeepEqual(granted, []string{selected}) {
					t.Fatalf("approval changed the selected directory: %q", granted)
				}
			}
			roots, err := eng.projectRootDirsForToolCall(ctx, "session", "turn", store.ModeCode)
			if err != nil {
				t.Fatal(err)
			}
			runner := tool.NewBuiltinRunner()
			defer runner.Close()
			read := func(path string) (tool.Result, map[string]any) {
				args, _ := json.Marshal(map[string]any{"scope": "project", "path": filepath.Join(path, "file.txt")})
				result := runner.Call(ctx, tool.Call{SessionID: "session", Name: tool.FileRead, Args: args, ProjectDirs: roots})
				var payload map[string]any
				if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
					t.Fatal(err)
				}
				return result, payload
			}
			if result, payload := read(selected); !result.Ok || payload["content"] != "selected" {
				t.Fatalf("selected directory was not authorized: %s", result.Content)
			}
			if result, payload := read(neighbor); result.Ok || payload["reason"] != "path_not_authorized" {
				t.Fatalf("neighbor directory was authorized: %s", result.Content)
			}
		})
	}
}
