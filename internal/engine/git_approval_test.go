package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestStructuredGitWorkflowApprovalModes(t *testing.T) {
	// No real user Git config, repositories, hooks, or credentials are used.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, mode := range []store.ApprovalMode{store.ApprovalAuto, store.ApprovalAsk, store.ApprovalFull} {
		t.Run(string(mode), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			root := t.TempDir()
			runEngineGitTest(t, root, "init")
			runEngineGitTest(t, root, "config", "user.name", "Pudding Test")
			runEngineGitTest(t, root, "config", "user.email", "pudding@example.test")
			for _, name := range []string{"target.txt", "unrelated.txt"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte("keep me\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ms := memstore.New()
			hub := event.NewHub()
			project := &store.Project{ID: "proj_git", RootDirs: []string{root}, ApprovalMode: mode}
			if err := ms.CreateProject(ctx, project); err != nil {
				t.Fatal(err)
			}
			const sid = "sess_git"
			if err := ms.CreateSession(ctx, &store.Session{
				ID: sid, Provider: "test", Model: "test", ActiveMode: store.ModeCode, ProjectID: project.ID,
			}); err != nil {
				t.Fatal(err)
			}
			eng := New(ms, hub, mapResolver{}, ms, WithTools(tool.NewBuiltinRunner()))
			sub, unsub := hub.Subscribe(sid)
			defer unsub()
			steps := []struct {
				id     string
				name   string
				args   map[string]any
				index  string
				reason string
				ask    bool
			}{
				{"outside_path", tool.GitStage, map[string]any{"scope": "project", "paths": []string{"../outside.txt"}}, "", "path_not_authorized", false},
				{"metadata", tool.GitStage, map[string]any{"scope": "project", "paths": []string{".git/config"}}, "", "git_metadata_forbidden", false},
				{"empty_commit", tool.GitCommit, map[string]any{"scope": "project", "message": "must not commit"}, "", "no_staged_changes", false},
				{"stage", tool.GitStage, map[string]any{"scope": "project", "paths": []string{"target.txt"}}, "target.txt", "", true},
				{"staged_diff", tool.GitDiff, map[string]any{"scope": "project", "staged": true}, "target.txt", "", false},
				{"unstage", tool.GitUnstage, map[string]any{"scope": "project", "paths": []string{"target.txt"}}, "", "", true},
				{"restage", tool.GitStage, map[string]any{"scope": "project", "paths": []string{"target.txt"}}, "target.txt", "", true},
				{"commit", tool.GitCommit, map[string]any{"scope": "project", "message": "requested commit"}, "", "", true},
				{"status", tool.GitStatus, map[string]any{"scope": "project"}, "", "", false},
				{"log", tool.GitLog, map[string]any{"scope": "project", "limit": 1}, "", "", false},
			}
			for _, step := range steps {
				call := tool.Call{SessionID: sid, TurnID: "turn_git", CallID: step.id, Name: step.name, Args: mustJSON(step.args)}
				done := make(chan tool.Result, 1)
				go func() {
					done <- eng.executeAllowedTool(ctx, sid, call.TurnID, store.ModeCode, call)
				}()
				approvals := 0
				handleEvent := func(ev event.Event) {
					if ev.Kind != event.ApprovalRequested {
						return
					}
					approvals++
					if mode != store.ApprovalAsk || !step.ask || ev.CallID != step.id {
						t.Fatalf("unexpected approval for %s in %s mode: %+v", step.id, mode, ev)
					}
					if err := eng.ApproveApproval(ctx, sid, ev.ApprovalID, ApprovalScopeTurn, nil); err != nil {
						t.Fatal(err)
					}
				}
			wait:
				for {
					select {
					case ev := <-sub:
						handleEvent(ev)
					case result := <-done:
						if result.Ok != (step.reason == "") || (step.reason != "" && !strings.Contains(result.Content, `"reason":"`+step.reason+`"`)) {
							t.Fatalf("%s in %s mode returned %+v, want reason %q", step.id, mode, result, step.reason)
						}
						break wait
					case <-ctx.Done():
						t.Fatalf("%s in %s mode did not finish: %v", step.id, mode, ctx.Err())
					}
				}
			drain:
				for {
					select {
					case ev := <-sub:
						handleEvent(ev)
					default:
						break drain
					}
				}
				wantApprovals := 0
				if mode == store.ApprovalAsk && step.ask {
					wantApprovals = 1
				}
				if approvals != wantApprovals || len(eng.PendingApprovals(sid)) != 0 {
					t.Fatalf("%s approvals=%d, want %d with none pending", step.id, approvals, wantApprovals)
				}
				if got := runEngineGitOutput(t, root, "diff", "--cached", "--name-only"); got != step.index {
					t.Fatalf("%s staged files=%q, want %q", step.id, got, step.index)
				}
			}
			if got := runEngineGitOutput(t, root, "log", "-1", "--format=%s"); got != "requested commit" {
				t.Fatalf("commit not created: %q", got)
			}
			if got := runEngineGitOutput(t, root, "ls-tree", "--name-only", "HEAD"); got != "target.txt" {
				t.Fatalf("commit included unrelated files: %q", got)
			}
			for _, name := range []string{"target.txt", "unrelated.txt"} {
				if data, err := os.ReadFile(filepath.Join(root, name)); err != nil || string(data) != "keep me\n" {
					t.Fatalf("worktree file %s changed: %q, %v", name, data, err)
				}
			}
		})
	}
}
