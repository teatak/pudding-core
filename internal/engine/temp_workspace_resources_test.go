package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestScratchCleanupWaitsForSessionArtifactUsers(t *testing.T) {
	eng := New(storetest.New(t), event.NewHub(), registry.Static(mock.New()), nil)
	defer eng.Stop()
	for _, call := range []tool.Call{
		{Name: tool.CommandRun, Args: json.RawMessage(`{"scope":"project","command":"printf test"}`)},
		{Name: tool.FileWrite, Args: json.RawMessage(`{"scope":"temp","path":"preview.log","content":"test"}`)},
		{Name: tool.MediaRead, Args: json.RawMessage(`{"source":"file","scope":"temp","path":"preview.png"}`)},
		{Name: tool.FileCopy, Args: json.RawMessage(`{"from":{"scope":"temp","path":"preview.png"},"to":{"scope":"project","path":"final.png"}}`)},
	} {
		t.Run(call.Name, func(t *testing.T) {
			call.SessionID = "one"
			call.ProjectDirs = []string{t.TempDir()}
			release, err := eng.acquireToolResources(context.Background(), call)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			if clearRelease, err := eng.acquireToolResources(ctx, tool.Call{SessionID: "one", Name: tool.TempWorkspace, Args: json.RawMessage(`{"action":"clear"}`)}); err == nil {
				clearRelease()
				t.Fatal("cleanup raced an active scratch user")
			}
			otherRelease, err := eng.acquireToolResources(context.Background(), tool.Call{SessionID: "two", Name: tool.TempWorkspace, Args: json.RawMessage(`{"action":"clear"}`)})
			if err != nil {
				t.Fatal(err)
			}
			otherRelease()
		})
	}
}
