package engine

import (
	"context"
	"log/slog"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/tool"
)

// ReloadCommandApprovalDiagnostics applies the saved opt-in independently of
// authorization. Turning it off clears only diagnostic state, never grants.
func (e *Engine) ReloadCommandApprovalDiagnostics(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	settings, err := e.config.Settings(ctx)
	enabled := err == nil && settings[config.SettingDeveloperApprovalDiagnostics] == "true"
	e.commandDiagnosticsEnabled = enabled
	if !enabled {
		clear(e.commandReuseDiagnostics)
		for _, state := range e.commandGrants {
			clear(state.identities)
			clear(state.reasons)
			state.reused = 0
		}
	}
	return err
}

type commandReuseDiagnostics struct {
	misses    map[string]int
	lastReset string
}

func (e *Engine) commandReuseDiagnosticLocked(sessionID string) *commandReuseDiagnostics {
	d := e.commandReuseDiagnostics[sessionID]
	if d == nil {
		d = &commandReuseDiagnostics{misses: make(map[string]int)}
		e.commandReuseDiagnostics[sessionID] = d
	}
	return d
}

func (e *Engine) commandReuseMissReasonLocked(sessionID string, state *commandApprovalState, next tool.CommandGrantIdentity) string {
	if !e.commandDiagnosticsEnabled {
		return ""
	}
	reason, rank := "no_matching_grant", 0
	for _, previous := range state.identities {
		if previous.Program != next.Program {
			continue
		}
		candidate, score := "arguments_changed", 4
		if previous.Arguments == next.Arguments {
			candidate = "directory_changed"
		}
		if previous.Context != next.Context {
			candidate, score = "scope_changed", 1
		} else if previous.Executable != next.Executable {
			candidate, score = "executable_changed", 2
		} else if previous.Environment != next.Environment {
			candidate, score = "environment_changed", 3
		}
		if score > rank {
			reason, rank = candidate, score
		}
	}
	if len(state.grants) == 0 {
		if d := e.commandReuseDiagnostics[sessionID]; d != nil && d.lastReset != "" {
			return d.lastReset
		}
	}
	return reason
}

func (e *Engine) recordCommandReuseMiss(sessionID, reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.recordCommandReuseMissLocked(sessionID, reason)
}

func (e *Engine) recordCommandReuseMissLocked(sessionID, reason string) {
	if !e.commandDiagnosticsEnabled {
		return
	}
	e.commandReuseDiagnosticLocked(sessionID).misses[reason]++
	// Opt-in only. Never include command text, paths, or environment values.
	slog.Info("command approval diagnostic", "sessionID", sessionID, "reuseMissReason", reason)
}
