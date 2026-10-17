package sqlitestore

import (
	"context"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestStoredStudioLoadUsesArtifactsAfterReopen(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "old-studio", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	// Simulate a row written by the previous binary, bypassing new write normalization.
	if _, err := st.db.Exec(`UPDATE sessions SET loaded_plugin_ids = ? WHERE id = ?`, `["studio","browser","artifacts"]`, "old-studio"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.GetSession(ctx, "old-studio")
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(got.LoadedPluginIDs, []string{"artifact", "browser"}) {
		t.Fatalf("loaded plugins = %v", got.LoadedPluginIDs)
	}
	if _, err := reopened.UpdateSession(ctx, "old-studio", store.SessionUpdate{LoadedPluginIDs: &got.LoadedPluginIDs}); err != nil {
		t.Fatal(err)
	}
	var saved string
	if err := reopened.db.QueryRow(`SELECT loaded_plugin_ids FROM sessions WHERE id = ?`, "old-studio").Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if saved != `["artifact","browser"]` {
		t.Fatalf("saved IDs = %s", saved)
	}
}

func TestRenamedPluginLoadsReopenWithoutRewritingHistoryOrRows(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "legacy-plugins", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	old := `["studio","artifacts","markdown","note-authoring","tables","collaboration","custom"]`
	if _, err := st.db.Exec(`UPDATE sessions SET loaded_plugin_ids=? WHERE id='legacy-plugins'`, old); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.GetSession(ctx, "legacy-plugins")
	if err != nil {
		t.Fatal(err)
	}
	want := store.NormalizePluginIDs([]string{"artifact", "note-authoring", "sheet-authoring", "subagent", "custom"})
	if !sameStrings(got.LoadedPluginIDs, want) {
		t.Fatal(got.LoadedPluginIDs)
	}
	var raw string
	if err := reopened.db.QueryRow(`SELECT loaded_plugin_ids FROM sessions WHERE id='legacy-plugins'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != old {
		t.Fatal("read rewrote stored identity", raw)
	}
}
