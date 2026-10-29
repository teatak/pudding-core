package engine

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestCommandReuseDiagnosticsExplainAndSurviveRevocation(t *testing.T) {
	ms := storetest.New(t)
	if err := ms.SetSettings(context.Background(), map[string]string{"developer_approval_diagnostics": "true"}); err != nil {
		t.Fatal(err)
	}
	eng := New(ms, event.NewHub(), registry.Static(mock.New()), ms)
	defer eng.Stop()
	state := eng.commandApprovalState("one", nil, []string{t.TempDir()})
	original := tool.CommandGrantIdentity{Program: "program", Environment: "env", Context: "scope", Executable: "exe", Arguments: "args"}
	state.identities["approved"] = original
	state.grants["approved"] = nil
	for _, tc := range []struct {
		reason string
		change func(*tool.CommandGrantIdentity)
	}{
		{"arguments_changed", func(x *tool.CommandGrantIdentity) { x.Arguments = "new" }},
		{"environment_changed", func(x *tool.CommandGrantIdentity) { x.Environment = "new" }},
		{"scope_changed", func(x *tool.CommandGrantIdentity) { x.Context = "new" }},
		{"executable_changed", func(x *tool.CommandGrantIdentity) { x.Executable = "new" }},
		{"no_matching_grant", func(x *tool.CommandGrantIdentity) { x.Program = "other" }},
	} {
		next := original
		tc.change(&next)
		eng.mu.Lock()
		got := eng.commandReuseMissReasonLocked("one", state, next)
		eng.mu.Unlock()
		if got != tc.reason {
			t.Fatalf("got %s want %s", got, tc.reason)
		}
		eng.recordCommandReuseMiss("one", got)
	}
	eng.RevokeCommandApprovals("one")
	state = eng.commandApprovalState("one", nil, []string{t.TempDir()})
	eng.mu.Lock()
	reason := eng.commandReuseMissReasonLocked("one", state, original)
	eng.mu.Unlock()
	if reason != "authorization_revoked" {
		t.Fatal(reason)
	}
	status := eng.CommandApprovals("one")
	if status.ReuseMissReasons["arguments_changed"] != 1 || status.GrantCount != 0 {
		t.Fatalf("lost diagnosis or retained authority: %+v", status)
	}
	status.ReuseMissReasons["arguments_changed"] = 100
	if eng.CommandApprovals("one").ReuseMissReasons["arguments_changed"] != 1 {
		t.Fatal("status mutated counters")
	}
	if len(eng.CommandApprovals("two").ReuseMissReasons) != 0 {
		t.Fatal("diagnostics leaked to other session")
	}
}

func TestCommandApprovalDiagnosticsOptInAndClear(t *testing.T) {
	ctx := context.Background()
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	ms := storetest.New(t)
	eng := New(ms, event.NewHub(), registry.Static(mock.New()), ms)
	defer eng.Stop()
	state := eng.commandApprovalState("one", nil, []string{t.TempDir()})
	state.grants["approved"] = nil
	eng.recordCommandReuseMiss("one", "arguments_changed")
	eng.RevokeCommandApprovals("other")
	if status := eng.CommandApprovals("one"); status.DiagnosticsEnabled || len(eng.commandReuseDiagnostics) != 0 || logs.Len() != 0 {
		t.Fatalf("default diagnostics collected data: %+v, logs=%s", status, &logs)
	}
	setEnabled := func(value string) {
		t.Helper()
		if err := ms.SetSettings(ctx, map[string]string{"developer_approval_diagnostics": value}); err != nil {
			t.Fatal(err)
		}
		if err := eng.ReloadCommandApprovalDiagnostics(ctx); err != nil {
			t.Fatal(err)
		}
	}
	setEnabled("true")
	eng.recordCommandReuseMiss("one", "arguments_changed")
	state.identities["approved"] = tool.CommandGrantIdentity{Program: "opaque"}
	state.reused, state.reasons["ask_mode"] = 2, 1
	if status := eng.CommandApprovals("one"); !status.DiagnosticsEnabled || status.ReuseMissReasons["arguments_changed"] != 1 || !strings.Contains(logs.String(), "reuseMissReason=arguments_changed") {
		t.Fatalf("enabled diagnostics missing: %+v, logs=%s", status, &logs)
	}
	// Only the reason and session identifier are appended to diagnostic logs.
	if !strings.Contains(logs.String(), "sessionID=one") || strings.Contains(logs.String(), "opaque") {
		t.Fatalf("unexpected diagnostic log: %s", &logs)
	}
	logged := logs.String()
	setEnabled("false")
	eng.recordCommandReuseMiss("one", "environment_changed")
	if status := eng.CommandApprovals("one"); status.DiagnosticsEnabled || status.GrantCount != 1 || status.ReusedCount != 0 || len(status.ReuseMissReasons)+len(status.ApprovalReasons)+len(state.identities) != 0 || logs.String() != logged {
		t.Fatalf("disabling retained diagnostics or changed authorization: %+v", status)
	}
	setEnabled("true")
	if status := eng.CommandApprovals("one"); status.GrantCount != 1 || len(status.ReuseMissReasons)+len(status.ApprovalReasons) != 0 {
		t.Fatalf("re-enabling restored old diagnostics: %+v", status)
	}
}
