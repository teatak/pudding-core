package lsp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerRefreshesChangedAndDeletedDocuments(t *testing.T) {
	manager := newTestManager()
	defer closeTestManager(t, manager)
	spec := testServerSpec(t, "refresh")
	ctx := context.Background()
	paths := map[string]string{}
	for _, name := range []string{"changed.go", "deleted.go"} {
		path := filepath.Join(spec.Key.LanguageRoot, name)
		uri := fileURI(path)
		paths[uri] = path
		if err := os.WriteFile(path, []byte("package demo\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.SyncDocument(ctx, spec, Document{URI: uri, LanguageID: "go", Text: "package demo\n"}); err != nil {
			t.Fatal(err)
		}
	}
	changedURI := fileURI(filepath.Join(spec.Key.LanguageRoot, "changed.go"))
	deletedURI := fileURI(filepath.Join(spec.Key.LanguageRoot, "deleted.go"))
	updated := "package demo\nfunc Current() {}\n"
	if err := os.WriteFile(paths[changedURI], []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths[deletedURI]); err != nil {
		t.Fatal(err)
	}
	load := func(uri string) (string, bool, error) {
		path, ok := paths[uri]
		if !ok {
			t.Fatalf("refresh read a document that was never opened: %s", uri)
		}
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return string(data), true, err
	}
	states, err := manager.RefreshDocuments(ctx, spec, load)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || !states[changedURI].Changed || states[changedURI].Version != 2 {
		t.Fatalf("refresh states = %+v", states)
	}
	var documents map[string]fakeLSPDocument
	if err := manager.Request(ctx, spec, "test/documents", nil, &documents); err != nil {
		t.Fatal(err)
	}
	if len(documents) != 1 || documents[changedURI].Text != updated || documents[changedURI].Version != 2 {
		t.Fatalf("server retained old overlays: %+v", documents)
	}
	states, err = manager.RefreshDocuments(ctx, spec, load)
	if err != nil || states[changedURI].Changed || states[changedURI].Version != 2 {
		t.Fatalf("unchanged refresh = %+v, %v", states, err)
	}
	var counts map[string]int
	if err := manager.Request(ctx, spec, "test/documentState", nil, &counts); err != nil {
		t.Fatal(err)
	}
	if counts["opens"] != 2 || counts["changes"] != 1 || counts["closes"] != 1 {
		t.Fatalf("unexpected refresh notifications: %+v", counts)
	}
}

func TestRefreshPreservesDocumentLimitAndUsageOrder(t *testing.T) {
	manager := newTestManager()
	defer closeTestManager(t, manager)
	spec := testServerSpec(t, "refresh-capacity")
	ctx := context.Background()
	// Reverse lexical order so a refresh sorted by URI cannot replace the
	// explicit use order when choosing which document to evict.
	for index := maxOpenDocuments - 1; index >= 0; index-- {
		uri := fileURI(filepath.Join(spec.Key.LanguageRoot, fmt.Sprintf("file%03d.go", index)))
		if _, err := manager.SyncDocument(ctx, spec, Document{URI: uri, LanguageID: "go", Text: "package demo\n"}); err != nil {
			t.Fatal(err)
		}
	}
	reads := 0
	if _, err := manager.RefreshDocuments(ctx, spec, func(string) (string, bool, error) {
		reads++
		return "package demo\n", true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if reads != maxOpenDocuments {
		t.Fatalf("refresh read %d documents, want %d", reads, maxOpenDocuments)
	}
	newURI := fileURI(filepath.Join(spec.Key.LanguageRoot, "new.go"))
	if _, err := manager.SyncDocument(ctx, spec, Document{URI: newURI, LanguageID: "go", Text: "package demo\n"}); err != nil {
		t.Fatal(err)
	}
	var documents map[string]fakeLSPDocument
	if err := manager.Request(ctx, spec, "test/documents", nil, &documents); err != nil {
		t.Fatal(err)
	}
	oldestURI := fileURI(filepath.Join(spec.Key.LanguageRoot, fmt.Sprintf("file%03d.go", maxOpenDocuments-1)))
	newestURI := fileURI(filepath.Join(spec.Key.LanguageRoot, "file000.go"))
	if _, exists := documents[oldestURI]; exists {
		t.Fatal("refresh changed the eviction order")
	}
	if len(documents) != maxOpenDocuments || documents[newestURI].Text == "" || documents[newURI].Text == "" {
		t.Fatalf("document capacity was not preserved: %d documents", len(documents))
	}

	// Explicitly reusing unchanged text still makes a document most recent.
	// Refresh changed text afterward so both refresh paths preserve that use.
	touchedURI := fileURI(filepath.Join(spec.Key.LanguageRoot, fmt.Sprintf("file%03d.go", maxOpenDocuments-2)))
	state, err := manager.SyncDocument(ctx, spec, Document{URI: touchedURI, LanguageID: "go", Text: "package demo\n"})
	if err != nil || state.Changed {
		t.Fatalf("unchanged explicit use = %+v, %v", state, err)
	}
	refreshedText := "package demo\nfunc Updated() {}\n"
	if _, err := manager.RefreshDocuments(ctx, spec, func(string) (string, bool, error) {
		return refreshedText, true, nil
	}); err != nil {
		t.Fatal(err)
	}
	nextURI := fileURI(filepath.Join(spec.Key.LanguageRoot, "next.go"))
	if _, err := manager.SyncDocument(ctx, spec, Document{URI: nextURI, LanguageID: "go", Text: "package demo\n"}); err != nil {
		t.Fatal(err)
	}
	documents = nil
	if err := manager.Request(ctx, spec, "test/documents", nil, &documents); err != nil {
		t.Fatal(err)
	}
	oldestUntouchedURI := fileURI(filepath.Join(spec.Key.LanguageRoot, fmt.Sprintf("file%03d.go", maxOpenDocuments-3)))
	if _, exists := documents[oldestUntouchedURI]; exists {
		t.Fatal("unchanged explicit use or changed refresh lost the eviction order")
	}
	if len(documents) != maxOpenDocuments || documents[touchedURI].Text != refreshedText || documents[newURI].Text != refreshedText || documents[nextURI].Text == "" {
		t.Fatalf("the most recently used documents were not retained: %d documents", len(documents))
	}
}

func TestRefreshInvalidatesUnchangedDependentDiagnostics(t *testing.T) {
	manager := newTestManager()
	defer closeTestManager(t, manager)
	spec := testServerSpec(t, "refresh-diagnostics")
	ctx := context.Background()
	dependencyURI := fileURI(filepath.Join(spec.Key.LanguageRoot, "a.go"))
	dependentURI := fileURI(filepath.Join(spec.Key.LanguageRoot, "b.go"))
	for _, uri := range []string{dependencyURI, dependentURI} {
		if _, err := manager.SyncDocument(ctx, spec, Document{URI: uri, LanguageID: "go", Text: "package demo\n"}); err != nil {
			t.Fatal(err)
		}
	}
	// The response is ordered after both initial diagnostic notifications.
	var counts map[string]int
	if err := manager.Request(ctx, spec, "test/documentState", nil, &counts); err != nil {
		t.Fatal(err)
	}
	process, err := manager.Acquire(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	before, ok := process.Diagnostics(dependentURI)
	if !ok {
		t.Fatal("initial dependent diagnostics were not cached")
	}
	states, err := manager.RefreshDocuments(ctx, spec, func(uri string) (string, bool, error) {
		if uri == dependencyURI {
			return "package demo\nfunc Changed() {}\n", true, nil
		}
		return "package demo\n", true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state := states[dependentURI]
	if !state.Changed || state.Version != 1 || state.PreviousDiagnosticGeneration != before.Generation {
		t.Fatalf("unchanged dependent kept valid old diagnostics: %+v", state)
	}
	// The fake server publishes only for the changed file. Its old b.go
	// diagnostics must not satisfy the watermark returned for this refresh.
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, ok, err := manager.PublishedDiagnostics(waitCtx, spec, dependentURI, state.PreviousDiagnosticGeneration); ok || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("old dependent diagnostics were reused: ok=%v, err=%v", ok, err)
	}
	// A subsequent tool may find no new disk changes (for example after a
	// symbols query did the refresh). afterGeneration=0 must still not revive
	// the dependent's pre-change cache.
	waitAgain, cancelAgain := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancelAgain()
	if _, ok, err := manager.PublishedDiagnostics(waitAgain, spec, dependentURI, 0); ok || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a later query revived invalidated diagnostics: ok=%v, err=%v", ok, err)
	}
}
