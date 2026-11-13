package engine

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestParallelFileReadsPreserveOrderAndBarriers(t *testing.T) {
	for _, cancelBatch := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancelBatch), func(t *testing.T) {
			eng, st, _, sid := newTestEngine(t)
			defer eng.Stop()
			eng.attachmentHome = t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "batch", UserMessageID: "u", ClientMessageID: "c", UserText: "read"}); err != nil {
				t.Fatal(err)
			}
			started := make(chan string, 8)
			releases := make(map[string]chan struct{})
			var parts turnPartAccumulator
			// The fifth read must wait for the first batch. A write separates the
			// next two reads and must remain a barrier, even for different paths.
			names := []string{tool.FileRead, tool.FileSlice, tool.FileList, tool.FileSearch, tool.FileRead, tool.FileWrite, tool.FileRead, tool.FileSlice}
			for index, name := range names {
				id := fmt.Sprint(index)
				releases[id] = make(chan struct{})
				parts.AppendTool(provider.ToolCallChunk{Index: index, CallID: id, Name: name, ArgsDelta: fmt.Sprintf(`{"scope":"project","path":"%s"}`, id)})
			}
			runner := &recordingToolRunner{defs: tool.BuiltinDefinitions(), result: tool.Result{Ok: true, Content: "read"}, callFunc: func(call tool.Call) {
				started <- call.CallID
				select {
				case <-releases[call.CallID]:
				case <-ctx.Done():
				}
			}}
			eng.tools = runner
			if err := eng.commitTurnParts("batch", &parts, true); err != nil {
				t.Fatal(err)
			}
			finished := make(chan store.TurnStatus, 1)
			go func() {
				status, _, _, _ := eng.executePendingTools(ctx, sid, "batch", store.ModeCode, runner.defs, &parts)
				finished <- status
			}()
			next := func() string {
				t.Helper()
				select {
				case id := <-started:
					return id
				case <-time.After(3 * time.Second):
					t.Fatal("tool did not start")
					return ""
				}
			}
			seen := map[string]bool{}
			for range 4 {
				seen[next()] = true
			}
			for _, id := range []string{"0", "1", "2", "3"} {
				if !seen[id] {
					t.Fatalf("read batch mixed: %v", seen)
				}
			}
			select {
			case id := <-started:
				t.Fatalf("concurrency limit exceeded: %s", id)
			default:
			}
			if cancelBatch {
				cancel()
			} else {
				for _, id := range []string{"3", "2", "1", "0"} {
					close(releases[id])
				}
				for _, id := range []string{"4", "5"} {
					if got := next(); got != id {
						t.Fatalf("barrier crossed: got %s want %s", got, id)
					}
					select {
					case later := <-started:
						t.Fatalf("tool %s crossed barrier %s", later, id)
					default:
					}
					close(releases[id])
				}
				last := map[string]bool{next(): true, next(): true}
				if !last["6"] || !last["7"] {
					t.Fatalf("last batch: %v", last)
				}
				close(releases["7"])
				close(releases["6"])
			}
			select {
			case status := <-finished:
				want := store.TurnRunning
				if cancelBatch {
					want = store.TurnCancelled
				}
				if status != want {
					t.Fatalf("status=%s want %s", status, want)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("tools did not finish")
			}
			messages, err := st.ListMessages(context.Background(), sid, 0)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, message := range messages {
				for _, part := range message.Parts {
					if part.Type == store.ContentPartToolResult {
						ids = append(ids, part.CallID)
					}
				}
			}
			want := []string{"0", "1", "2", "3"}
			if !cancelBatch {
				want = append(want, "4", "5", "6", "7")
			}
			if !reflect.DeepEqual(ids, want) {
				t.Fatalf("result order/loss: %v want %v", ids, want)
			}
		})
	}
}

func TestParallelFileReadsOnlyUseAllowedBuiltins(t *testing.T) {
	for _, call := range []tool.Call{{Name: tool.CommandRun}, {Name: tool.FileWrite}, {Name: tool.PluginLoad}, {Name: tool.FileRead}} {
		if parallelFileRead(call, nil) {
			t.Fatalf("unavailable tool admitted: %s", call.Name)
		}
	}
	if parallelFileRead(tool.Call{Name: tool.FileRead}, []provider.ToolDef{{Name: tool.FileRead, PluginID: "custom"}}) {
		t.Fatal("plugin tool admitted")
	}
}
