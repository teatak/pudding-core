package tool

import "testing"

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
