package tool

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

func TestBuiltinToolsBelongToCoreOrApp(t *testing.T) {
	for _, def := range BuiltinDefinitions() {
		_, appTool := BuiltinAppIDForTool(def.Name)
		appTool = appTool || IsAppAPITool(def.Name)
		coreTool := IsCoreTool(def.Name)
		if appTool == coreTool {
			t.Fatalf("tool %s core=%v app=%v; want exactly one owner", def.Name, coreTool, appTool)
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

func TestCoreDefinitionsUseSmallStableCodeSurface(t *testing.T) {
	defs := BuiltinDefinitions()
	for _, removed := range []string{"builtin_project_inspect", "builtin_project_instructions"} {
		if HasDefinition(defs, removed) {
			t.Fatalf("removed project helper still exposed as a tool: %s", removed)
		}
	}
	first := CoreDefinitionsForMode(store.ModeCode, defs)
	second := CoreDefinitionsForMode(store.ModeCode, defs)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("core tool definitions are not stable")
	}
	for _, name := range []string{
		RequestCapability, PlanUpdate, CommandRun, CommandSession, WebSearch, MediaRead,
		FileList, FileRead, AttachmentExport, FileStat, FileSearch, FileSlice,
		FileWrite, FilePatch, FileDelete, FileMove, FileCopy,
		GitStatus, GitDiff, GitLog, GitStage, GitUnstage, GitCommit,
		CodeSymbols, CodeDefinition, CodeReferences, CodeDiagnostics, CodeRename,
	} {
		if !HasDefinition(first, name) {
			t.Fatalf("Code Core missing %s", name)
		}
	}
	for _, name := range []string{CameraCapture, DesktopScreenshot, BrowserOpen, RESTRequest, SkillValidate, AppSave} {
		if HasDefinition(first, name) {
			t.Fatalf("App tool %s leaked into Code Core", name)
		}
	}
	if len(first) > 35 {
		t.Fatalf("Code Core tool count = %d, want <= 35", len(first))
	}
}

func TestFileToolDescriptionsExplainLineSafePatchWorkflow(t *testing.T) {
	descriptions := make(map[string]string)
	for _, definition := range BuiltinDefinitions() {
		descriptions[definition.Name] = definition.Description
	}
	if !strings.Contains(descriptions[FileRead], "direct line counting is acceptable") || !strings.Contains(descriptions[FileRead], "long, truncated, or unfamiliar files") {
		t.Fatalf("file read must redirect line-based edits to numbered tools: %q", descriptions[FileRead])
	}
	if !strings.Contains(descriptions[FileSlice], "numberedContent") || !strings.Contains(descriptions[FileSlice], "old_lines") {
		t.Fatalf("file slice must explain how to construct patch hunks: %q", descriptions[FileSlice])
	}
	if !strings.Contains(descriptions[FilePatch], "builtin_file_slice.numberedContent") || !strings.Contains(descriptions[FilePatch], "short complete builtin_file_read") || !strings.Contains(descriptions[FilePatch], "fresh numbered slice") {
		t.Fatalf("file patch must explain the line-safe workflow: %q", descriptions[FilePatch])
	}
}

func TestPlanUpdateIsAvailableOnlyInWorkAndCode(t *testing.T) {
	defs := BuiltinDefinitions()
	if HasDefinition(CoreDefinitionsForMode(store.ModeChat, defs), PlanUpdate) {
		t.Fatal("plan update leaked into Chat Core")
	}
	for _, mode := range []store.AgentMode{store.ModeWork, store.ModeCode} {
		if !HasDefinition(CoreDefinitionsForMode(mode, defs), PlanUpdate) {
			t.Fatalf("plan update missing from %s Core", mode)
		}
	}
}

func TestMediaReadSchemaExposesFileSourceOnlyInCode(t *testing.T) {
	defs := BuiltinDefinitions()
	for _, mode := range []store.AgentMode{store.ModeChat, store.ModeWork} {
		def := definitionByName(t, CoreDefinitionsForMode(mode, defs), MediaRead)
		if strings.Contains(string(def.InputSchema), `"file"`) || strings.Contains(string(def.InputSchema), `"scope"`) {
			t.Fatalf("%s media schema exposed file access: %s", mode, def.InputSchema)
		}
	}
	def := definitionByName(t, CoreDefinitionsForMode(store.ModeCode, defs), MediaRead)
	if !strings.Contains(string(def.InputSchema), `"file"`) || !strings.Contains(string(def.InputSchema), `"scope"`) {
		t.Fatalf("Code media schema is missing file access: %s", def.InputSchema)
	}
}

func TestCoreDefinitionsIncludeRuntimeRequestUserInputOnly(t *testing.T) {
	defs := []provider.ToolDef{
		{Name: RequestUserInput, Capability: store.ModeChat},
		{Name: "canvas_markdown", Capability: store.ModeChat},
		{Name: "external_tool", Capability: store.ModeWork},
	}
	got := CoreDefinitionsForMode(store.ModeCode, defs)
	if !HasDefinition(got, RequestUserInput) {
		t.Fatalf("runtime Chat Core tool missing: %v", toolNames(got))
	}
	if HasDefinition(got, "canvas_markdown") || HasDefinition(got, "external_tool") {
		t.Fatalf("non-Core dynamic tools leaked into Core: %v", toolNames(got))
	}
}

func TestDefaultCodeToolSchemaReduction(t *testing.T) {
	defs := BuiltinDefinitions()
	defaults := append(CoreDefinitionsForMode(store.ModeCode, defs), AppLoadDefinition())
	full := []provider.ToolDef{RequestCapabilityDefinition(), AppLoadDefinition()}
	for _, def := range defs {
		if ToolDefAllowedForMode(store.ModeCode, def) {
			full = append(full, def)
		}
	}
	defaultJSON, _ := json.Marshal(defaults)
	fullJSON, _ := json.Marshal(full)
	reduction := 1 - float64(len(defaultJSON))/float64(len(fullJSON))
	t.Logf("Code tool schema: core=%d tools/%d bytes full=%d tools/%d bytes reduction=%.1f%%", len(defaults), len(defaultJSON), len(full), len(fullJSON), reduction*100)
	if reduction < 0.25 {
		t.Fatalf("Code Core schema reduction %.1f%%, want >= 25%%", reduction*100)
	}
}

func toolNames(defs []provider.ToolDef) []string {
	names := make([]string, len(defs))
	for index, def := range defs {
		names[index] = def.Name
	}
	return names
}

func definitionByName(t *testing.T, defs []provider.ToolDef, name string) provider.ToolDef {
	t.Helper()
	for _, def := range defs {
		if def.Name == name {
			return def
		}
	}
	t.Fatalf("definition %s not found", name)
	return provider.ToolDef{}
}
