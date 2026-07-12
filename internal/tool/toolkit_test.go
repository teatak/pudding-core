package tool

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

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
		if counts[def.Name] != 1 {
			t.Fatalf("tool %s belongs to %d toolkits", def.Name, counts[def.Name])
		}
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

func TestToolkitCatalogGroupsDynamicUIAndApps(t *testing.T) {
	defs := []provider.ToolDef{
		{Name: "canvas_markdown", Capability: store.ModeChat},
		{Name: "ui_confirm", Capability: store.ModeChat},
		{Name: "app_mcp__github__default__search__hash", Capability: store.ModeWork},
	}
	catalog := BuildToolkitCatalog(defs)
	ui, ok := ToolkitByID(catalog, "ui.canvas")
	if !ok || !ui.Default || len(ui.ToolNames) != 2 {
		t.Fatalf("UI toolkit wrong: %+v", ui)
	}
	app, ok := ToolkitByID(catalog, "app.github")
	if !ok || app.Default || len(app.ToolNames) != 1 || app.Capability != store.ModeWork {
		t.Fatalf("app toolkit wrong: %+v", app)
	}
}

func TestToolkitIndexIsCapabilityScopedAndStable(t *testing.T) {
	catalog := BuildToolkitCatalog(BuiltinDefinitions())
	work := ToolkitIndex(store.ModeWork, catalog)
	if !strings.Contains(work, "work.browser") || strings.Contains(work, "code.lsp") {
		t.Fatalf("Work toolkit index wrong: %s", work)
	}
	code := ToolkitIndex(store.ModeCode, catalog)
	if !strings.Contains(code, "work.browser") || !strings.Contains(code, "code.lsp") {
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
