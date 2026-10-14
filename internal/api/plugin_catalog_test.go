package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestPluginCatalogInstallUpgradeAndRemoveSource(t *testing.T) {
	home := t.TempDir()
	st := storetest.New(t)
	handler := New(nil, st, config.NewManager(home), nil).WithHome(home).WithPlugins(plugin.NewService(home, nil)).Handler("test-token", nil)
	call := func(method, path string, body any, status int) []byte {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	sourceURL := "https://third.test/plugins/registry.json"
	var source config.PluginSource
	if err := json.Unmarshal(call("POST", "/plugins/sources", map[string]string{"url": sourceURL}, 200), &source); err != nil {
		t.Fatal(err)
	}
	install := func(version, source, expectedID, expectedVersion string, status int) {
		t.Helper()
		pkg := plugin.Package{Kind: plugin.PluginPackageKind, SchemaVersion: 1, Plugin: plugin.PackagePlugin{ID: "external", Version: version}, Files: []plugin.PackageFile{{Path: "plugin.yaml", Content: "id: external\nname: External\nversion: " + version + "\n"}}}
		data, _ := json.Marshal(pkg)
		call("POST", "/plugins/install", map[string]string{"packageJSON": string(data), "sourceURL": source, "pluginID": expectedID, "pluginVersion": expectedVersion}, status)
	}
	install("1.0.0", sourceURL, "wrong", "1.0.0", 400)
	install("1.0.0", sourceURL, "external", "2.0.0", 400)
	install("1.0.0", sourceURL, "external", "1.0.0", 200)
	// Reopening the config and package service models a daemon restart.
	handler = New(nil, st, config.NewManager(home), nil).WithHome(home).WithPlugins(plugin.NewService(home, nil)).Handler("test-token", nil)
	install("2.0.0", sourceURL, "external", "2.0.0", 200)
	install("9.0.0", "https://other.test/plugins/registry.json", "external", "9.0.0", 409)
	call("DELETE", "/plugins/sources/"+source.ID, nil, 200)
	var list struct {
		Plugins []*plugin.Definition `json:"plugins"`
	}
	if err := json.Unmarshal(call("GET", "/plugins", nil, 200), &list); err != nil {
		t.Fatal(err)
	}
	for _, p := range list.Plugins {
		if p.ID == "external" {
			if p.Version != "2.0.0" || p.SourceURL != sourceURL {
				t.Fatalf("removed source changed plugin: %+v", p)
			}
			if _, err := os.Stat(filepath.Join(home, "plugins", "external", "plugin.yaml")); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("removing source removed installed plugin")
}
