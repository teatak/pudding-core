package tool

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	ToolkitLoad        = "builtin_toolkit_load"
	maxToolkitsPerLoad = 4
)

type ToolkitManifest struct {
	ID         string          `json:"id"`
	Capability store.AgentMode `json:"capability"`
	Summary    string          `json:"summary"`
	Keywords   []string        `json:"keywords,omitempty"`
	ToolNames  []string        `json:"toolNames"`
	Default    bool            `json:"default,omitempty"`
}

type ToolkitLoadRequest struct {
	ToolkitIDs []string `json:"toolkit_ids"`
}

var builtinToolkitTemplates = []ToolkitManifest{
	{
		ID: "chat.core", Capability: store.ModeChat, Default: true,
		Summary:  "Current information, history, skills, attachments, and desktop context.",
		Keywords: []string{"time", "weather", "web", "history", "attachment", "desktop"},
		ToolNames: []string{
			TimeGetCurrent, WebSearch, WebFetch, HistorySearch, HistoryGetMessage, SkillRead,
			AttachmentReadImage, WeatherGet, DesktopScreenshot,
		},
	},
	{
		ID: "code.core", Capability: store.ModeCode, Default: true,
		Summary:  "Project orientation, CLI, focused file reading, and reviewable patch application.",
		Keywords: []string{"project", "instructions", "command", "read", "patch", "test", "build"},
		ToolNames: []string{
			ProjectInspect, ProjectInstructions, CommandRun, FileRead, PatchPropose, PatchApply,
		},
	},
	{
		ID: "code.files-read", Capability: store.ModeCode,
		Summary:   "Structured file listing, metadata, text search, and line slices when CLI fallback is needed.",
		Keywords:  []string{"files", "list", "stat", "search", "slice"},
		ToolNames: []string{FileList, FileStat, FileSearch, FileSlice},
	},
	{
		ID: "code.files-write", Capability: store.ModeCode,
		Summary:   "Direct file write, exact patch, delete, move, and copy operations.",
		Keywords:  []string{"write", "delete", "move", "copy", "rename"},
		ToolNames: []string{FileWrite, FilePatch, FileDelete, FileMove, FileCopy},
	},
	{
		ID: "code.git-read", Capability: store.ModeCode,
		Summary:   "Structured Git status, diff, and log when Git CLI output is unavailable or insufficient.",
		Keywords:  []string{"git", "status", "diff", "log", "history"},
		ToolNames: []string{GitStatus, GitDiff, GitLog},
	},
	{
		ID: "code.git-write", Capability: store.ModeCode,
		Summary:   "Structured Git staging, unstaging, and commits with approval and drift checks.",
		Keywords:  []string{"git", "stage", "unstage", "commit"},
		ToolNames: []string{GitStage, GitUnstage, GitCommit},
	},
	{
		ID: "code.lsp", Capability: store.ModeCode,
		Summary:   "Language-server symbols, definitions, references, diagnostics, and semantic rename.",
		Keywords:  []string{"symbols", "definition", "references", "diagnostics", "rename", "lsp"},
		ToolNames: []string{CodeSymbols, CodeDefinition, CodeReferences, CodeDiagnostics, CodeRename},
	},
	{
		ID: "code.skill", Capability: store.ModeCode,
		Summary:   "Validate and submit staged skill packages for user review.",
		Keywords:  []string{"skill", "validate", "publish"},
		ToolNames: []string{SkillValidate, SkillSubmit},
	},
	{
		ID: "work.camera", Capability: store.ModeWork,
		Summary:   "Capture a local camera photo when explicitly requested by the user.",
		Keywords:  []string{"camera", "photo"},
		ToolNames: []string{CameraCapture},
	},
}

func ToolkitLoadDefinition() provider.ToolDef {
	return provider.ToolDef{
		Name:        ToolkitLoad,
		Description: "Load one or more non-App toolkits for the current turn. Use only ids from the Available Toolkits index, then call tools advertised in the next model step. This never loads Apps such as Browser, Terminal, or Canvas; use builtin_app_load for an App. Loaded toolkits remain active only for this turn.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"toolkit_ids":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":4,"description":"Toolkit ids from the Available Toolkits index."}},"required":["toolkit_ids"],"additionalProperties":false}`),
		Capability:  store.ModeChat,
	}
}

func DecodeToolkitLoadRequest(raw json.RawMessage) (ToolkitLoadRequest, error) {
	var request ToolkitLoadRequest
	if len(raw) == 0 || json.Unmarshal(raw, &request) != nil {
		return request, errors.New("toolkit load arguments must be a JSON object")
	}
	seen := make(map[string]bool)
	ids := make([]string, 0, len(request.ToolkitIDs))
	for _, id := range request.ToolkitIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return request, errors.New("toolkit_ids must contain at least one toolkit id")
	}
	if len(ids) > maxToolkitsPerLoad {
		return request, fmt.Errorf("toolkit_ids must contain at most %d toolkit ids", maxToolkitsPerLoad)
	}
	request.ToolkitIDs = ids
	return request, nil
}

func BuildToolkitCatalog(defs []provider.ToolDef) []ToolkitManifest {
	definitions := make(map[string]provider.ToolDef, len(defs))
	for _, def := range defs {
		if strings.TrimSpace(def.Name) != "" {
			definitions[def.Name] = def
		}
	}
	assigned := make(map[string]bool, len(definitions))
	catalog := make([]ToolkitManifest, 0, len(builtinToolkitTemplates)+4)
	for _, template := range builtinToolkitTemplates {
		manifest := cloneToolkitManifest(template)
		manifest.ToolNames = existingToolNames(manifest.ToolNames, definitions, assigned)
		if len(manifest.ToolNames) > 0 {
			catalog = append(catalog, manifest)
		}
	}

	dynamic := make(map[string]*ToolkitManifest)
	for name, def := range definitions {
		if assigned[name] || name == RequestCapability || name == ToolkitLoad {
			continue
		}
		if strings.HasPrefix(name, "canvas_") {
			continue
		}
		if _, appTool := BuiltinAppIDForTool(name); appTool || IsAppAPITool(name) || def.AppID != "" || strings.HasPrefix(name, appMCPToolPrefix) {
			continue
		}
		id, summary, defaultToolkit := dynamicToolkit(name, def)
		manifest := dynamic[id]
		if manifest == nil {
			manifest = &ToolkitManifest{
				ID:         id,
				Capability: normalizedToolCapability(def),
				Summary:    summary,
				Default:    defaultToolkit,
			}
			dynamic[id] = manifest
		}
		if store.AgentModeRank(normalizedToolCapability(def)) > store.AgentModeRank(manifest.Capability) {
			manifest.Capability = normalizedToolCapability(def)
		}
		manifest.ToolNames = append(manifest.ToolNames, name)
	}
	for _, manifest := range dynamic {
		sort.Strings(manifest.ToolNames)
		catalog = append(catalog, *manifest)
	}
	sort.Slice(catalog, func(i, j int) bool { return catalog[i].ID < catalog[j].ID })
	return catalog
}

func DefinitionsForTurn(mode store.AgentMode, defs []provider.ToolDef, active map[string]bool) []provider.ToolDef {
	return DefinitionsFromCatalog(mode, defs, BuildToolkitCatalog(defs), active)
}

func DefinitionsFromCatalog(mode store.AgentMode, defs []provider.ToolDef, catalog []ToolkitManifest, active map[string]bool) []provider.ToolDef {
	mode = normalizedMode(mode)
	byName := make(map[string]provider.ToolDef, len(defs))
	for _, def := range defs {
		byName[def.Name] = def
	}
	out := []provider.ToolDef{RequestCapabilityDefinition(), ToolkitLoadDefinition()}
	seen := map[string]bool{RequestCapability: true, ToolkitLoad: true}
	for _, manifest := range catalog {
		if !manifest.Default && !active[manifest.ID] {
			continue
		}
		if store.AgentModeRank(mode) < store.AgentModeRank(manifest.Capability) {
			continue
		}
		names := append([]string(nil), manifest.ToolNames...)
		sort.Strings(names)
		for _, name := range names {
			def, ok := byName[name]
			if !ok || seen[name] || !ToolDefAllowedForMode(mode, def) {
				continue
			}
			seen[name] = true
			out = append(out, def)
		}
	}
	return out
}

func ToolkitByID(catalog []ToolkitManifest, id string) (ToolkitManifest, bool) {
	id = strings.TrimSpace(id)
	for _, manifest := range catalog {
		if manifest.ID == id {
			return manifest, true
		}
	}
	return ToolkitManifest{}, false
}

func ToolkitForTool(catalog []ToolkitManifest, name string) (ToolkitManifest, bool) {
	for _, manifest := range catalog {
		for _, toolName := range manifest.ToolNames {
			if toolName == name {
				return manifest, true
			}
		}
	}
	return ToolkitManifest{}, false
}

func ToolkitIndex(mode store.AgentMode, catalog []ToolkitManifest) string {
	mode = normalizedMode(mode)
	var lines []string
	for _, manifest := range catalog {
		if manifest.Default || store.AgentModeRank(mode) < store.AgentModeRank(manifest.Capability) {
			continue
		}
		lines = append(lines, fmt.Sprintf("- `%s`: %s", manifest.ID, manifest.Summary))
	}
	if len(lines) == 0 {
		return ""
	}
	return "## Available Toolkits\n\nLoad a listed non-App toolkit with `builtin_toolkit_load` only when the task needs it. Loaded tools appear on the next model step and reset after this turn. `builtin_toolkit_load` never loads Apps, including Browser, Terminal, and Canvas; use `builtin_app_load` with an id from Available Apps.\n\n" + strings.Join(lines, "\n")
}

func ActiveToolkitIDs(active map[string]bool) []string {
	ids := make([]string, 0, len(active))
	for id, enabled := range active {
		if enabled {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func cloneToolkitManifest(manifest ToolkitManifest) ToolkitManifest {
	manifest.Keywords = append([]string(nil), manifest.Keywords...)
	manifest.ToolNames = append([]string(nil), manifest.ToolNames...)
	return manifest
}

func existingToolNames(names []string, defs map[string]provider.ToolDef, assigned map[string]bool) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := defs[name]; !ok || assigned[name] {
			continue
		}
		assigned[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func dynamicToolkit(name string, def provider.ToolDef) (string, string, bool) {
	switch {
	case strings.HasPrefix(name, "ui_"), name == "collect_user_input":
		return "ui.core", "Structured interaction tools exposed by the current UI.", true
	default:
		mode := normalizedToolCapability(def)
		return "external." + string(mode), "Other connected tools available in " + string(mode) + " capability.", false
	}
}

func normalizedToolCapability(def provider.ToolDef) store.AgentMode {
	mode := store.NormalizeAgentMode(def.Capability)
	if !store.ValidAgentMode(mode) {
		return store.ModeCode
	}
	return mode
}

func normalizedMode(mode store.AgentMode) store.AgentMode {
	mode = store.NormalizeAgentMode(mode)
	if !store.ValidAgentMode(mode) {
		return store.ModeChat
	}
	return mode
}
