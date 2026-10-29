package tool

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const TempWorkspace = "builtin_temp_workspace"

func tempWorkspaceDefinition() provider.ToolDef {
	return provider.ToolDef{Name: TempWorkspace, Capability: store.ModeCode,
		Description: "Open or clear this session's disposable workspace for screenshots, logs and preview copies. Use its absolutePath for command outputs, scope/path for file tools. Never place these files in the project. clear removes only this workspace without deleting exported attachments or project files. It is also cleared when session resources are released or the daemon shuts down. Save final deliverables into the project or read images as media attachments before cleanup. Do not use clear while a background process is running.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["open","clear"]}},"required":["action"],"additionalProperties":false}`),
	}
}

func (r *BuiltinRunner) tempWorkspace(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Action string `json:"action"`
	}
	decoder := json.NewDecoder(bytes.NewReader(call.Args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if decoder.Decode(new(any)) != io.EOF {
		return toolJSONError(out, "invalid_arguments", "Expected one JSON object")
	}
	if call.SessionID == "" {
		return toolJSONError(out, "session_required", "A session is required")
	}
	r.scratchMu.Lock()
	defer r.scratchMu.Unlock()
	switch args.Action {
	case "open":
		root, path, err := home.OpenSessionScratch(r.homeDir, call.SessionID)
		if err != nil {
			return toolJSONError(out, "temp_workspace_failed", err.Error())
		}
		root.Close()
		r.scratchSessions[call.SessionID] = true
		return toolJSON(out, true, map[string]any{"ok": true, "scope": "temp", "path": filepath.Join("session-artifacts", filepath.Base(filepath.Dir(path)), "scratch"), "absolutePath": path, "cleanup": "clear_or_session_release"})
	case "clear":
		if r.BackgroundProcessCount(call.SessionID) > 0 {
			return toolJSONError(out, "workspace_in_use", "Stop background processes before clearing the workspace")
		}
		if err := home.RemoveSessionScratch(r.homeDir, call.SessionID); err != nil {
			return toolJSONError(out, "temp_workspace_cleanup_failed", err.Error())
		}
		delete(r.scratchSessions, call.SessionID)
		return toolJSON(out, true, map[string]any{"ok": true, "cleared": true})
	default:
		return toolJSONError(out, "invalid_action", "action must be open or clear")
	}
}

func (r *BuiltinRunner) cleanupSessionScratch(sessionID string) {
	if strings.TrimSpace(r.homeDir) == "" || sessionID == "" {
		return
	}
	r.scratchMu.Lock()
	defer r.scratchMu.Unlock()
	if err := home.RemoveSessionScratch(r.homeDir, sessionID); err != nil {
		slog.Warn("tool: scratch cleanup failed", "sessionID", sessionID, "err", err)
		return
	}
	delete(r.scratchSessions, sessionID)
}

// Serialize cleanup with commands and file/media operations using temp paths.
// Background commands retain the lock until exit; the engine preflights cleanup.
func UsesSessionArtifacts(call Call) bool {
	if call.Name == TempWorkspace || call.Name == CommandRun {
		return true
	}
	var args struct {
		Scope string `json:"scope"`
		From  struct {
			Scope string `json:"scope"`
		} `json:"from"`
		To struct {
			Scope string `json:"scope"`
		} `json:"to"`
	}
	if json.Unmarshal(call.Args, &args) != nil {
		return false
	}
	return strings.TrimSpace(args.Scope) == managedScopeTemp || strings.TrimSpace(args.From.Scope) == managedScopeTemp || strings.TrimSpace(args.To.Scope) == managedScopeTemp
}
