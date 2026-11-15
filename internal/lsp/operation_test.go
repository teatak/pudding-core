package lsp

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestOperationDoesNotReacquireUnopenedDocumentAfterCrash(t *testing.T) {
	manager := newTestManager()
	defer closeTestManager(t, manager)
	spec := testServerSpec(t, "bound-restart")
	ctx := context.Background()
	op, err := manager.BeginOperation(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	defer op.Release()
	document := Document{URI: fileURI(filepath.Join(spec.Key.LanguageRoot, "a.ts")), LanguageID: "typescript", Text: "const target = 1;\n"}
	if _, err := op.SyncDocument(ctx, spec, document); err != nil {
		t.Fatal(err)
	}
	// didOpen was delivered; crash before the position request. A new process
	// would return a successful empty result for this unopened document.
	process := op.(*operation).process
	process.forceTerminate()
	select {
	case <-process.Done():
	case <-time.After(time.Second):
		t.Fatal("helper did not exit")
	}
	params := map[string]any{"textDocument": map[string]string{"uri": document.URI}}
	var locations []any
	if err := op.Request(ctx, spec, "textDocument/definition", params, &locations); !errors.Is(err, ErrProcessClosed) {
		t.Fatalf("crashed operation returned silent empty success: %v, %+v", err, locations)
	}
	op.Release()
	retry, err := manager.BeginOperation(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	defer retry.Release()
	if _, err := retry.SyncDocument(ctx, spec, document); err != nil {
		t.Fatal(err)
	}
	if err := retry.Request(ctx, spec, "textDocument/definition", params, &locations); err != nil || len(locations) != 1 {
		t.Fatalf("whole-operation retry did not synchronize document: %v, %+v", err, locations)
	}
}

func TestOperationSerializesOverlaysAndPinsCapacity(t *testing.T) {
	manager := newTestManager()
	manager.maxProcesses = 1
	manager.idleTimeout = time.Millisecond
	defer closeTestManager(t, manager)
	spec := testServerSpec(t, "lease")
	op, err := manager.BeginOperation(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	defer op.Release()
	waitCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := manager.BeginOperation(waitCtx, spec); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second operation acquired speculative overlays: %v", err)
	}
	if err := manager.ReapIdle(context.Background()); err != nil || manager.ProcessCount() != 1 {
		t.Fatalf("leased process was reaped: %v", err)
	}
	if _, err := manager.Acquire(context.Background(), testServerSpec(t, "other")); !errors.Is(err, ErrCapacity) {
		t.Fatalf("leased process was evicted for capacity: %v", err)
	}
	op.Release()
	retry, err := manager.BeginOperation(context.Background(), spec)
	if err != nil {
		t.Fatalf("lease was not released after cancelled waiter: %v", err)
	}
	retry.Release()
}

func TestOperationAbortDiscardsSpeculativeDocuments(t *testing.T) {
	manager := newTestManager()
	defer closeTestManager(t, manager)
	spec := testServerSpec(t, "abort-overlay")
	op, err := manager.BeginOperation(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	process := op.(*operation).process
	if _, err := op.SyncDocument(context.Background(), spec, Document{URI: fileURI(filepath.Join(spec.Key.LanguageRoot, "a.ts")), LanguageID: "typescript", Text: "speculative"}); err != nil {
		t.Fatal(err)
	}
	op.Abort()
	op.Release()
	restarted, err := manager.Acquire(context.Background(), spec)
	if err != nil || restarted == process {
		t.Fatalf("aborted process was reused: %v", err)
	}
	var documents map[string]fakeLSPDocument
	if err := restarted.Request(context.Background(), "test/documents", nil, &documents); err != nil || len(documents) != 0 {
		t.Fatalf("speculative documents survived: %v, %+v", err, documents)
	}
}
