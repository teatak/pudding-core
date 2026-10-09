package tool

import "testing"

func TestCurrentToolNameProjectsRetiredNamesOnlyInHistory(t *testing.T) {
	for old, current := range map[string]string{
		"builtin_collaboration_list":     SubtaskList,
		"builtin_collaboration_dispatch": SubtaskDispatch,
		"builtin_collaboration_send":     SubtaskSend,
		"builtin_collaboration_wait":     SubtaskWait,
		"builtin_collaboration_stop":     SubtaskStop,
		"builtin_studio_list":            ArtifactList,
		"builtin_studio_open":            ArtifactOpen,
	} {
		if got := CurrentToolName(old); got != current {
			t.Errorf("historical %s = %s, want %s", old, got, current)
		}
		if CurrentToolName(current) != current || HasDefinition(BuiltinDefinitions(), old) {
			t.Errorf("noncanonical executable tool: %s", old)
		}
		if _, ok := BuiltinPluginIDForTool(old); ok {
			t.Errorf("retired name has executable ownership: %s", old)
		}
	}
}

func TestReferencedStudioSkillUsesArtifactIdentity(t *testing.T) {
	for _, kind := range []string{PluginSkillReference, legacyPluginSkillReference} {
		content := `{"reference":{"kind":"` + kind + `","pluginID":"studio","appID":"studio","skillID":"studio"}}`
		got, ok := ReferencedSkill(PluginLoad, true, content)
		want := SkillReference{Kind: PluginSkillReference, PluginID: "artifacts", SkillID: "artifacts"}
		if !ok || got != want {
			t.Fatalf("reference = %+v, %v", got, ok)
		}
	}
	// A global or unrelated plugin's skill with the same name is not renamed.
	got, _ := ReferencedSkill(SkillRead, true, `{"reference":{"kind":"skill","skillID":"studio"}}`)
	if got.SkillID != "studio" {
		t.Fatal(got)
	}
	got, _ = ReferencedSkill(PluginLoad, true, `{"reference":{"kind":"plugin_skill","pluginID":"custom","skillID":"studio"}}`)
	if got.SkillID != "studio" {
		t.Fatal(got)
	}
}

func TestReferencedSkillReadsPluginResultsStoredUnderAppNames(t *testing.T) {
	for _, tt := range []struct {
		name, content string
		want          SkillReference
	}{
		{PluginLoad, `{"reference":{"kind":"plugin_skill","pluginID":"github","skillID":"issues"}}`, SkillReference{Kind: PluginSkillReference, PluginID: "github", SkillID: "issues"}},
		{"builtin_app_load", `{"reference":{"kind":"app_skill","appID":"github","skillID":"issues"}}`, SkillReference{Kind: PluginSkillReference, PluginID: "github", SkillID: "issues"}},
		{"builtin_app_load", `{"reference":{"kind":"app_skill","appID":"app-authoring","skillID":"app-creator"}}`, SkillReference{Kind: PluginSkillReference, PluginID: "plugin-authoring", SkillID: "plugin-creator"}},
		{"builtin_app_load", `{"appID":"canvas","skillID":"canvas-code","content":"old body"}`, SkillReference{Kind: PluginSkillReference, PluginID: "widget-authoring", SkillID: "canvas-code"}},
	} {
		got, ok := ReferencedSkill(tt.name, true, tt.content)
		if !ok || got != tt.want {
			t.Fatalf("%s %s: got %+v %v, want %+v", tt.name, tt.content, got, ok, tt.want)
		}
	}
	// A new result never carries a body without a reference.
	if _, ok := ReferencedSkill(PluginLoad, true, `{"pluginID":"github","skillID":"issues","content":"body"}`); ok {
		t.Fatal("unreferenced plugin result was projected")
	}
}
