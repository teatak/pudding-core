package tool

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

func TestBuiltinToolsBelongToExactlyOneToolkit(t *testing.T) {
	defs := BuiltinDefinitions()
	catalog := BuildToolkitCatalog(defs)
	counts := make(map[string]int)
	for _, manifest := range catalog {
		for _, name := range manifest.ToolNames {
			counts[name]++
		}
	}
	for _, def := range defs {
		_, builtinAppTool := BuiltinAppIDForTool(def.Name)
		if builtinAppTool || IsAppAPITool(def.Name) {
			if counts[def.Name] != 0 {
				t.Fatalf("app tool %s leaked into a toolkit", def.Name)
			}
			continue
		}
		if counts[def.Name] != 1 {
			t.Fatalf("tool %s belongs to %d toolkits", def.Name, counts[def.Name])
		}
	}
}

func TestBuiltinAppDefinitionsListOwnedTools(t *testing.T) {
	definitions := make(map[string]provider.ToolDef)
	for _, def := range BuiltinDefinitions() {
		definitions[def.Name] = def
	}
	seen := make(map[string]bool)
	for _, def := range app.BuiltinDefinitions() {
		requiredMode := store.NormalizeAgentMode(store.AgentMode(def.RequiredMode))
		for _, ref := range def.Tools {
			owner, ok := BuiltinAppIDForTool(ref.Name)
			if !ok || owner != def.ID {
				t.Fatalf("app %s lists unowned tool %s", def.ID, ref.Name)
			}
			toolDef, ok := definitions[ref.Name]
			if !ok {
				t.Fatalf("app %s lists undefined tool %s", def.ID, ref.Name)
			}
			if mode := store.NormalizeAgentMode(toolDef.Capability); mode != requiredMode {
				t.Fatalf("app %s requires %s but tool %s requires %s", def.ID, requiredMode, ref.Name, mode)
			}
			seen[ref.Name] = true
		}
	}
	if len(seen) != len(builtinAppTools) {
		t.Fatalf("app definitions list %d tools, ownership map has %d", len(seen), len(builtinAppTools))
	}
}

func TestDefinitionsForTurnUsesSmallStableCodeDefault(t *testing.T) {
	defs := BuiltinDefinitions()
	first := DefinitionsForTurn(store.ModeCode, defs, nil)
	second := DefinitionsForTurn(store.ModeCode, defs, nil)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("default tool definitions are not stable")
	}
	for _, name := range []string{RequestCapability, ToolkitLoad, ProjectInspect, ProjectInstructions, CommandRun, FileRead, PatchPropose, PatchApply, WebSearch} {
		if !HasDefinition(first, name) {
			t.Fatalf("default Code tools missing %s", name)
		}
	}
	for _, name := range []string{FileList, GitStatus, CodeDiagnostics, CommandStart, BrowserOpen, RESTRequest} {
		if HasDefinition(first, name) {
			t.Fatalf("lazy tool %s leaked into default Code tools", name)
		}
	}
	if len(first) > 20 {
		t.Fatalf("default Code tool count = %d, want <= 20", len(first))
	}
}

func TestDefaultCodeToolSchemaReduction(t *testing.T) {
	defs := BuiltinDefinitions()
	catalog := BuildToolkitCatalog(defs)
	active := make(map[string]bool)
	for _, manifest := range catalog {
		if store.AgentModeRank(store.ModeCode) >= store.AgentModeRank(manifest.Capability) {
			active[manifest.ID] = true
		}
	}
	full := DefinitionsFromCatalog(store.ModeCode, defs, catalog, active)
	defaults := DefinitionsFromCatalog(store.ModeCode, defs, catalog, nil)
	fullJSON, _ := json.Marshal(full)
	defaultJSON, _ := json.Marshal(defaults)
	reduction := 1 - float64(len(defaultJSON))/float64(len(fullJSON))
	t.Logf("Code tool schema: default=%d tools/%d bytes full=%d tools/%d bytes reduction=%.1f%%", len(defaults), len(defaultJSON), len(full), len(fullJSON), reduction*100)
	if reduction < 0.5 {
		t.Fatalf("default Code tool schema reduction %.1f%%, want >= 50%%", reduction*100)
	}
}

func TestDefinitionsForTurnLoadsToolkitMonotonically(t *testing.T) {
	defs := BuiltinDefinitions()
	active := map[string]bool{"code.git-read": true}
	loaded := DefinitionsForTurn(store.ModeCode, defs, active)
	if !HasDefinition(loaded, GitStatus) || !HasDefinition(loaded, GitDiff) || !HasDefinition(loaded, GitLog) {
		t.Fatalf("Git read toolkit not loaded: %+v", toolNames(loaded))
	}
	if HasDefinition(loaded, GitCommit) {
		t.Fatal("Git write leaked from Git read toolkit")
	}
	active["code.lsp"] = true
	expanded := DefinitionsForTurn(store.ModeCode, defs, active)
	if len(expanded) <= len(loaded) || !HasDefinition(expanded, GitStatus) || !HasDefinition(expanded, CodeDiagnostics) {
		t.Fatalf("toolkit set did not grow monotonically: before=%v after=%v", toolNames(loaded), toolNames(expanded))
	}
}

func TestToolkitCatalogGroupsDynamicUIAndExcludesApps(t *testing.T) {
	defs := []provider.ToolDef{
		{Name: "canvas_markdown", Capability: store.ModeChat},
		{Name: "ui_confirm", Capability: store.ModeChat},
		{Name: "collect_user_input", Capability: store.ModeChat},
		{Name: "app_mcp__search__hash", Capability: store.ModeWork, AppID: "github"},
	}
	catalog := BuildToolkitCatalog(defs)
	ui, ok := ToolkitByID(catalog, "ui.core")
	if !ok || !ui.Default || len(ui.ToolNames) != 2 {
		t.Fatalf("UI toolkit wrong: %+v", ui)
	}
	for _, manifest := range catalog {
		if strings.HasPrefix(manifest.ID, "app.") || slicesContain(manifest.ToolNames, "app_mcp__search__hash") || slicesContain(manifest.ToolNames, "canvas_markdown") {
			t.Fatalf("App or Canvas tool leaked into toolkit: %+v", manifest)
		}
	}
}

func TestToolkitIndexIsCapabilityScopedAndStable(t *testing.T) {
	catalog := BuildToolkitCatalog(BuiltinDefinitions())
	work := ToolkitIndex(store.ModeWork, catalog)
	if strings.Contains(work, "work.browser") || strings.Contains(work, "work.api") || strings.Contains(work, "code.lsp") {
		t.Fatalf("Work toolkit index wrong: %s", work)
	}
	code := ToolkitIndex(store.ModeCode, catalog)
	if strings.Contains(code, "work.browser") || strings.Contains(code, "code.process") || !strings.Contains(code, "code.lsp") || !strings.Contains(code, "code.app") {
		t.Fatalf("Code toolkit index wrong: %s", code)
	}
	ids := make([]string, len(catalog))
	for index, manifest := range catalog {
		ids[index] = manifest.ID
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("catalog is not sorted: %v", ids)
	}
}

func TestCodeAppToolkitLoadsOnlyAppSave(t *testing.T) {
	catalog := BuildToolkitCatalog(BuiltinDefinitions())
	manifest, ok := ToolkitByID(catalog, "code.app")
	if !ok || manifest.Default || !reflect.DeepEqual(manifest.ToolNames, []string{AppSave}) {
		t.Fatalf("unexpected code.app toolkit: %+v", manifest)
	}
	loaded := DefinitionsForTurn(store.ModeCode, BuiltinDefinitions(), map[string]bool{"code.app": true})
	if !HasDefinition(loaded, AppSave) {
		t.Fatalf("code.app did not load %s", AppSave)
	}
}

func TestDecodeToolkitLoadRequest(t *testing.T) {
	request, err := DecodeToolkitLoadRequest(json.RawMessage(`{"toolkit_ids":["code.lsp","code.lsp","code.git-read"]}`))
	if err != nil || !reflect.DeepEqual(request.ToolkitIDs, []string{"code.lsp", "code.git-read"}) {
		t.Fatalf("decode toolkit load: request=%+v err=%v", request, err)
	}
}

func toolNames(defs []provider.ToolDef) []string {
	names := make([]string, len(defs))
	for index, def := range defs {
		names[index] = def.Name
	}
	return names
}

func slicesContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
