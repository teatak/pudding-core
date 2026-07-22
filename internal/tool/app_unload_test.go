package tool

import (
	"encoding/json"
	"testing"
)

func TestAppUnloadDefinitionListsLoadedApps(t *testing.T) {
	definition := AppUnloadDefinition([]string{" github ", "browser", "github"})
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	got := schema.Properties["app_id"].Enum
	if len(got) != 2 || got[0] != "browser" || got[1] != "github" {
		t.Fatalf("app_id enum = %+v", got)
	}
}

func TestDecodeAppUnloadRequest(t *testing.T) {
	request, err := DecodeAppUnloadRequest(json.RawMessage(`{"app_id":" browser "}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.AppID != "browser" {
		t.Fatalf("request = %+v", request)
	}
	if _, err := DecodeAppUnloadRequest(json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing app_id should fail")
	}
}
