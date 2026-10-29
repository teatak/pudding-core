package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func waitAssistantTaskApproval(t *testing.T, eng *Engine, sid string) ApprovalRequest {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		if approvals := eng.PendingApprovals(sid); len(approvals) == 1 {
			return approvals[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("dispatch approval did not appear in the assistant conversation")
	return ApprovalRequest{}
}

func TestAssistantExistingTaskApproval(t *testing.T) {
	for _, inProject := range []bool{false, true} {
		for _, mode := range []store.ApprovalMode{store.ApprovalAuto, store.ApprovalAsk, store.ApprovalFull} {
			for _, destination := range []string{"existing", "follow-up"} {
				for _, decision := range []string{"approve", "deny", "cancel", "move", "capability", "archive", "delete"} {
					if decision != "approve" && decision != "deny" && decision != "cancel" && (mode != store.ApprovalAuto || destination != "existing") {
						continue
					}
					t.Run(fmt.Sprintf("project=%t/%s/%s/%s", inProject, mode, destination, decision), func(t *testing.T) {
						ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
						defer cancel()
						db := storetest.New(t)
						client := &nextPromptClient{}
						eng := New(db, event.NewHub(), mapResolver{"capture": client}, db)
						t.Cleanup(func() { eng.Stop(); eng.Wait() })
						if err := db.PutProviderProfile(ctx, &store.ProviderProfile{ID: "capture", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "test", Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
							t.Fatal(err)
						}
						profile, err := db.OpenAssistant(ctx, "小布", "capture", "test", store.AssistantAppearance{"version": 1})
						if err != nil {
							t.Fatal(err)
						}
						sid := profile.Assistant.SessionID
						if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "source", UserMessageID: "source-message", ClientMessageID: "source-client", UserText: "Arrange work"}); err != nil {
							t.Fatal(err)
						}
						project := &store.Project{ID: "project", Name: "Project name", RootDirs: []string{t.TempDir()}, ApprovalMode: mode}
						if err := db.CreateProject(ctx, project); err != nil {
							t.Fatal(err)
						}
						worker := &store.Session{ID: "worker", Title: "Existing work", Provider: "capture", Model: "test", ProjectID: project.ID, ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession}
						if !inProject {
							worker.ProjectID = ""
						}
						if err := db.CreateSession(ctx, worker); err != nil {
							t.Fatal(err)
						}
						const prompt = "Implement the requested change.\nPreserve unrelated files and verify the result."
						args := map[string]string{"title": "Existing work", "prompt": prompt, "session_id": worker.ID}
						call := tool.Call{CallID: "dispatch", Name: tool.AssistantTaskDispatch}
						beforeRuns := 0

						if destination == "follow-up" {
							seed, err := db.AssignAssistantTask(ctx, store.AssignAssistantTaskInput{AssistantSessionID: sid, SourceTurnID: "source", CallID: "seed", Title: worker.Title, Session: worker, Input: store.QueueInputInput{SessionID: worker.ID, ClientMessageID: "seed", Text: "Previous task", Provider: "capture", Model: "test", Mode: store.ModeCode}})
							if err != nil {
								t.Fatal(err)
							}
							if _, err := db.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: worker.ID, TurnID: "seed-turn", UserMessageID: "seed-message"}); err != nil {
								t.Fatal(err)
							}
							if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: "seed-turn", Status: store.TurnCompleted}); err != nil {
								t.Fatal(err)
							}
							args = map[string]string{"task_id": seed.TaskID, "prompt": prompt}
							call.Name = tool.AssistantTaskSend
							beforeRuns = 1
						}
						call.Args = mustJSON(args)
						done := make(chan tool.Result, 1)
						go func() { done <- eng.executeAllowedTool(ctx, sid, "source", store.ModeChat, call) }()
						approval := waitAssistantTaskApproval(t, eng, sid)
						var details struct {
							ProjectID, ProjectName, TargetSessionID, TargetSessionTitle, TaskPrompt string
							CreateSession                                                           bool
						}
						if err := json.Unmarshal(approval.Payload, &details); err != nil {
							t.Fatal(err)
						}
						if details.ProjectID != worker.ProjectID || (inProject && details.ProjectName != project.Name) || details.TaskPrompt != prompt || details.CreateSession || approval.TurnID != "source" || approval.CallID != call.CallID {
							t.Fatalf("approval must describe the exact destination and task: %+v / %+v", approval, details)
						}
						if details.TargetSessionID != worker.ID || details.TargetSessionTitle != worker.Title {
							t.Fatal("wrong approval target", details)
						}
						if runs, err := db.ListAssistantTaskRuns(ctx, sid); err != nil || len(runs) != beforeRuns {
							t.Fatal("task queued before approval", runs, err)
						}
						if len(client.snapshot()) != 0 || len(eng.PendingApprovals(worker.ID)) != 0 {
							t.Fatal("worker ran or owns the dispatch approval")
						}
						switch decision {
						case "deny":
							err = eng.DenyApproval(ctx, sid, approval.ID, "Not this task")
						case "cancel":
							cancel()
						default:
							switch decision {
							case "move":
								movedProject := ""
								if !inProject {
									movedProject = project.ID
								}
								_, err = db.UpdateSession(ctx, worker.ID, store.SessionUpdate{ProjectID: &movedProject})
							case "capability":
								work := store.ModeWork
								_, err = db.UpdateSession(ctx, worker.ID, store.SessionUpdate{ActiveMode: &work})
							case "archive":
								_, err = db.ArchiveSession(ctx, worker.ID)
							case "delete":
								err = db.DeleteSession(ctx, worker.ID)
							}
							if err != nil {
								t.Fatal(err)
							}
							// A forged session scope or directories must not turn dispatch
							// approval into a capability or persistent permission grant.
							err = eng.ApproveApproval(ctx, sid, approval.ID, ApprovalScopeSession, []string{t.TempDir()})
						}
						if err != nil {
							t.Fatal(err)
						}
						var result tool.Result
						select {
						case result = <-done:
						case <-time.After(3 * time.Second):
							t.Fatal("approval did not settle")
						}
						if len(eng.PendingApprovals(sid)) != 0 {
							t.Fatal("stale approval after resolution")
						}
						if result.Ok != (decision == "approve") {
							t.Fatal("unexpected dispatch result", decision, result)
						}
						if decision != "approve" {
							if len(client.snapshot()) != 0 {
								t.Fatal("rejected or invalidated dispatch executed")
							}
							if runs, err := db.ListAssistantTaskRuns(context.Background(), sid); err != nil || len(runs) != beforeRuns {
								t.Fatal("failed dispatch left a task", runs, err)
							}

							return
						}
						var task store.AssistantTaskRun
						if err := json.Unmarshal([]byte(result.Content), &task); err != nil {
							t.Fatal(err)
						}
						for deadline := time.Now().Add(3 * time.Second); ; {
							turn, ok := turnByClientID(t, db, task.SessionID, task.ClientMessageID)
							if ok && turn.Status == store.TurnCompleted {
								break
							}
							if time.Now().After(deadline) {
								t.Fatal("approved worker did not finish")
							}
							time.Sleep(5 * time.Millisecond)
						}
						current, err := db.GetSession(ctx, sid)
						if err != nil || current.ActiveMode != store.ModeChat || current.ProjectID != "" {
							t.Fatal("dispatch approval changed assistant permissions", current, err)
						}
						follow := tool.Call{CallID: "follow", Name: tool.AssistantTaskSend, Args: mustJSON(map[string]string{"task_id": task.TaskID, "prompt": "A new follow-up"})}
						go func() { done <- eng.executeAllowedTool(ctx, sid, "source", store.ModeChat, follow) }()
						next := waitAssistantTaskApproval(t, eng, sid)
						if next.ID == approval.ID || next.CallID != "follow" {
							t.Fatal("approval was reused for different work")
						}
						if err := eng.DenyApproval(ctx, sid, next.ID, "Stop here"); err != nil {
							t.Fatal(err)
						}
						if followResult := <-done; followResult.Ok || !strings.Contains(followResult.Content, "denied") {
							t.Fatal("follow-up rejection ignored", followResult)
						}
						if len(client.snapshot()) != 1 {
							t.Fatal("dispatch executed more than the approved task")
						}
					})
				}
			}
		}
	}

}

func TestAssistantCreatedTaskNeedsNoDispatchApproval(t *testing.T) {
	for _, mode := range []store.ApprovalMode{store.ApprovalAuto, store.ApprovalAsk, store.ApprovalFull} {
		for _, inProject := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/project=%t", mode, inProject), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				db := storetest.New(t)
				client := &nextPromptClient{}
				eng := New(db, event.NewHub(), mapResolver{"capture": client}, db)
				t.Cleanup(func() { eng.Stop(); eng.Wait() })
				if err := db.PutProviderProfile(ctx, &store.ProviderProfile{ID: "capture", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "test", Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
					t.Fatal(err)
				}
				profile, err := db.OpenAssistant(ctx, "小布", "capture", "test", store.AssistantAppearance{"version": 1})
				if err != nil {
					t.Fatal(err)
				}
				sid := profile.Assistant.SessionID
				if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "source", UserMessageID: "message", ClientMessageID: "client", UserText: "Arrange work"}); err != nil {
					t.Fatal(err)
				}
				args := map[string]string{"title": "New work", "prompt": "Complete this task"}
				if inProject {
					if err := db.CreateProject(ctx, &store.Project{ID: "project", Name: "Project", RootDirs: []string{t.TempDir()}, ApprovalMode: mode}); err != nil {
						t.Fatal(err)
					}
					args["project_id"] = "project"
				}
				var task store.AssistantTaskRun
				for i := 0; i < 2; i++ {
					call := tool.Call{CallID: fmt.Sprint("dispatch", i), Name: tool.AssistantTaskDispatch, Args: mustJSON(args)}
					if i > 0 {
						call.Name = tool.AssistantTaskSend
						call.Args = mustJSON(map[string]string{"task_id": task.TaskID, "prompt": "Follow up"})
					}
					result := eng.executeAllowedTool(ctx, sid, "source", store.ModeChat, call)
					if !result.Ok {
						t.Fatal("assistant-created work must not wait for dispatch approval", result)
					}
					if len(eng.PendingApprovals(sid)) != 0 {
						t.Fatal("unexpected dispatch approval")
					}
					if err := json.Unmarshal([]byte(result.Content), &task); err != nil {
						t.Fatal(err)
					}
					for deadline := time.Now().Add(2 * time.Second); ; {
						turn, ok := turnByClientID(t, db, task.SessionID, task.ClientMessageID)
						if ok && turn.Status == store.TurnCompleted {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("worker did not finish")
						}
						time.Sleep(5 * time.Millisecond)
					}
					if created, err := db.IsAssistantCreatedSession(ctx, sid, task.SessionID); err != nil || !created {
						t.Fatal("creation origin missing", created, err)
					}
				}
				if len(client.snapshot()) != 2 {
					t.Fatal("expected initial work and follow-up")
				}
			})
		}
	}
}

func TestAssistantSubagentNeedsNoDispatchApproval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := &subagentResetExecutionProbe{}
	eng, db, _ := newCollaborationEngine(t, client)
	profile, err := db.OpenAssistant(ctx, "小布", client.Name(), "model", store.AssistantAppearance{"version": 1})
	if err != nil {
		t.Fatal(err)
	}
	sid := profile.Assistant.SessionID
	mode, lease, ids := store.ModeWork, store.ModeLeaseSession, []string{plugin.BuiltinSubagentID}
	if _, err := db.UpdateSession(ctx, sid, store.SessionUpdate{ActiveMode: &mode, ModeLease: &lease, LoadedPluginIDs: &ids}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "source", UserMessageID: "message", ClientMessageID: "client", UserText: "Do a small subtask"}); err != nil {
		t.Fatal(err)
	}
	result := eng.executeAllowedTool(ctx, sid, "source", mode, tool.Call{CallID: "subagent", Name: tool.SubtaskDispatch, Args: mustJSON(map[string]string{"title": "Small task", "prompt": "Do this small task"})})
	if !result.Ok || len(eng.PendingApprovals(sid)) != 0 {
		t.Fatal("subagent must not need dispatch approval", result)
	}
	children, err := eng.ListChildSessions(ctx, sid)
	if err != nil || len(children) != 1 {
		t.Fatal("subagent not created", children, err)
	}
}
