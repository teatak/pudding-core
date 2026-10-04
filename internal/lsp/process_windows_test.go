//go:build windows

package lsp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsCommandLauncherPreservesQuotedPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "中文 cmd & launcher")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(root, "server.cmd")
	if err := os.WriteFile(launcher, []byte("@echo off\r\n\""+os.Args[0]+"\" -test.run=^TestLSPHelperProcess$\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(WithIdleTimeout(0), WithReapInterval(0))
	t.Cleanup(func() { closeTestManager(t, manager) })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	process, err := manager.Acquire(ctx, ServerSpec{
		Key:     ProcessKey{LanguageRoot: root, ServerKind: "cmd-helper"},
		Command: os.Getenv("COMSPEC"),
		Args:    []string{"/d", "/s", "/c", `"` + launcher + `" --stdio`},
		Dir:     root,
		Env:     append(os.Environ(), helperEnv+"=1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !process.Alive() {
		t.Fatal("initialized command launcher did not retain the language server")
	}
}

func TestWindowsDiagnosticURIDriveIdentity(t *testing.T) {
	cache := newDiagnosticsCache()
	wireURI := "file:///c%3A/project/%E4%B8%AD%E6%96%87%20TS/main.ts"
	requestedURI := "file:///C:/project/%E4%B8%AD%E6%96%87%20TS/main.ts"
	raw, _ := json.Marshal(map[string]any{"uri": wireURI, "diagnostics": []Diagnostic{{Message: "real type error"}}})
	cache.update(raw)
	snapshot, ok := cache.get(requestedURI)
	if !ok || len(snapshot.Diagnostics) != 1 || cache.generationForURI(requestedURI) != snapshot.Generation {
		t.Fatalf("equivalent drive URI missed cached diagnostic: %+v, %v", snapshot, ok)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, ok, err := cache.wait(ctx, requestedURI, 0); err != nil || !ok {
		t.Fatalf("wait: %v, %v", ok, err)
	}
	if diagnosticURIKey("file:///C:/Project/main.ts") == diagnosticURIKey("file:///C:/project/main.ts") {
		t.Fatal("drive normalization must preserve folder case")
	}
}

func TestWindowsTypeScriptPublishedDiagnostics(t *testing.T) {
	if os.Getenv("PUDDING_LSP_TS_INTEGRATION") != "1" {
		t.Skip("set PUDDING_LSP_TS_INTEGRATION=1 to test the installed TypeScript server")
	}
	launcher, err := exec.LookPath("typescript-language-server.cmd")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "中文 TS")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{"compilerOptions":{"strict":true},"include":["*.ts"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	source := "export const broken: string = 1\n"
	file := filepath.Join(root, "main.ts")
	if err := os.WriteFile(file, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(WithIdleTimeout(0), WithReapInterval(0))
	t.Cleanup(func() { closeTestManager(t, manager) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	spec := ServerSpec{Key: ProcessKey{LanguageRoot: root, ServerKind: "typescript"}, Command: os.Getenv("COMSPEC"),
		Args: []string{"/d", "/s", "/c", `"` + launcher + `" --stdio`}, Dir: root}
	process, err := manager.Acquire(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	uri := fileURI(file)
	state, err := process.SyncDocument(Document{URI: uri, LanguageID: "typescript", Text: source})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok, err := manager.PublishedDiagnostics(ctx, spec, uri, state.PreviousDiagnosticGeneration)
	// Syntax and semantic publications are separate; this fixture has a known
	// type error, so continue observing generations until that error is published.
	for err == nil && ok && len(snapshot.Diagnostics) == 0 {
		snapshot, ok, err = manager.PublishedDiagnostics(ctx, spec, uri, snapshot.Generation)
	}
	if err != nil || !ok || len(snapshot.Diagnostics) == 0 {
		process.diagnostics.mu.RLock()
		defer process.diagnostics.mu.RUnlock()
		t.Fatalf("diagnostics for %q: %+v, %v; published: %+v", uri, snapshot, err, process.diagnostics.byURI)
	}
}
