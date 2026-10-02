package tool

import (
	"encoding/json"
	"testing"
)

func TestPluginUnloadDefinitionListsLoadedPlugins(t *testing.T) {
	definition := PluginUnloadDefinition([]string{" github ", "browser", "github"})
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	got := schema.Properties["plugin_id"].Enum
	if len(got) != 2 || got[0] != "browser" || got[1] != "github" {
		t.Fatalf("plugin_id enum = %+v", got)
	}
}

func TestDecodePluginUnloadRequest(t *testing.T) {
	request, err := DecodePluginUnloadRequest(json.RawMessage(`{"plugin_id":" browser "}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.PluginID != "browser" {
		t.Fatalf("request = %+v", request)
	}
	if _, err := DecodePluginUnloadRequest(json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing plugin_id should fail")
	}
}
