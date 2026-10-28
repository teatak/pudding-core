package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/teatak/pudding-core/contracts"
	"os"
	"path/filepath"
	"testing"
)

func apiPackage(t *testing.T, api, yamlAPI string, schema int) []byte {
	t.Helper()
	yaml := "id: sample\nname: Sample\nversion: 1.0.0\n"
	if yamlAPI != "" {
		yaml += "api_version: " + yamlAPI + "\n"
	}
	pkg := Package{Kind: PluginPackageKind, SchemaVersion: schema, Plugin: PackagePlugin{ID: "sample", Version: "1.0.0", APIVersion: api}, Files: []PackageFile{{Path: PluginFileName, Content: yaml}}}
	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestPluginAPIPackageCompatibilityAndAtomicUpdate(t *testing.T) {
	root := t.TempDir()
	source := "https://fixture.test/plugins/registry.json"
	old := apiPackage(t, "", "", 1)
	if _, err := InstallPackage(root, old, "", source); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "sample", PluginFileName))
	for _, bad := range [][]byte{apiPackage(t, "2.0.0", "2.0.0", 2), apiPackage(t, "1.0.0", "2.0.0", 2), apiPackage(t, "", "", 2), apiPackage(t, "1.1.0", "1.0.0", 2)} {
		if _, err := InstallPackage(root, bad, "", source); err == nil {
			t.Fatal("accepted incompatible or inconsistent package")
		}
		after, _ := os.ReadFile(filepath.Join(root, "sample", PluginFileName))
		if string(before) != string(after) {
			t.Fatal("rejected update changed original plugin")
		}
	}
	next := apiPackage(t, contracts.PluginAPIVersion(), contracts.PluginAPIVersion(), 2)
	if _, err := InstallPackage(root, next, "", source); err != nil {
		t.Fatal(err)
	}
	def, err := LoadDefinitionDir(filepath.Join(root, "sample"))
	if err != nil || def.APIVersion != contracts.PluginAPIVersion() {
		t.Fatal(def, err)
	}
}
func TestInstalledUnsupportedPluginStaysVisibleButCannotEnableOrExecute(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "plugins", "sample")
	os.MkdirAll(dir, 0700)
	yaml := "id: sample\nname: Sample\nversion: 1.0.0\napi_version: 2.0.0\nendpoints:\n  api:\n    kind: rest\n    url: https://fixture.test\n"
	os.WriteFile(filepath.Join(dir, PluginFileName), []byte(yaml), 0600)
	config := &fakePluginConfig{enabled: map[string]bool{"sample": true}}
	svc := NewService(root, config)
	defs, err := svc.ListDefinitions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	def := definitionByID(defs, "sample")
	if def == nil || def.Enabled || def.APIVersion != "2.0.0" {
		t.Fatal(def)
	}
	if _, _, err := svc.ResolveBoundEndpoint(context.Background(), "sample", "api", ""); err == nil {
		t.Fatal("incompatible plugin endpoint was executable")
	}
	if !config.enabled["sample"] {
		t.Fatal("overwrote saved enablement preference")
	}
	if _, err := svc.SetEnabled(context.Background(), "sample", true); !errors.Is(err, ErrIncompatibleAPI) {
		t.Fatal(err)
	}
	for _, d := range defs {
		if d.Source == "builtin" && !contracts.SupportsPluginAPI(d.APIVersion) {
			t.Fatalf("host builtin marked incompatible: %s", d.ID)
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, PluginFileName))
	if string(after) != yaml {
		t.Fatal("rewrote installed plugin")
	}
}

func TestPluginCatalogPathMigrationDoesNotChangeInstallOwnership(t *testing.T) {
	root := t.TempDir()
	legacy := "https://teatak.github.io/pudding-hub/plugins/registry.json"
	reference := "https://teatak.github.io/pudding-hub/index.json?source=teatak%2Fpudding-hub"
	raw := apiPackage(t, "", "", 1)
	if _, err := InstallPackage(root, raw, "", legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallPackage(root, raw, "", reference); err != nil {
		t.Fatal("index migration lost install ownership", err)
	}
	if _, err := InstallPackage(root, raw, "", "https://other.test/index.json?source=teatak%2Fpudding-hub"); !errors.Is(err, ErrSourceConflict) {
		t.Fatal("different index took over installed plugin", err)
	}
}
