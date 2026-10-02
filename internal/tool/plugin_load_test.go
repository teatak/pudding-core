package tool

import (
	"encoding/json"
	"testing"
)

func TestDecodePluginLoadRequest(t *testing.T) {
	request, err := DecodePluginLoadRequest(json.RawMessage(`{"plugin_id":" widget-authoring ","skill_id":" widget-authoring "}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.PluginID != "widget-authoring" || request.SkillID != "widget-authoring" {
		t.Fatalf("request = %+v", request)
	}
	if _, err := DecodePluginLoadRequest(json.RawMessage(`{"skill_id":"widget-authoring"}`)); err == nil {
		t.Fatal("missing plugin_id should fail")
	}
}
