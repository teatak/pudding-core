package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/teatak/pudding-core/internal/store"
)

var nativeCreationKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

// A creation key owns a deterministic item ID. Its fingerprint is committed in
// the existing initial-save receipt, atomically with the item/body/history.
// Omitting a key retains the independent creation behavior of existing callers.
func prepareStudioCreation(item *store.StudioItem, body any, author store.ContentAuthor, keys []string) (string, error) {
	if len(keys) == 0 {
		return "", nil
	}
	if len(keys) != 1 || !nativeCreationKey.MatchString(keys[0]) {
		return "", &store.InvalidContent{Message: "clientRequestID must contain 1–200 ASCII letters, digits, underscores or hyphens"}
	}
	payload, err := json.Marshal(struct {
		Kind, Name, NameMode, NameFallback, Icon, IconColor, SourceSessionID string
		Body                                                                 any
		Author                                                               store.ContentAuthor
	}{item.Kind, item.Name, item.NameMode, item.NameFallback, item.Icon, item.IconColor, item.SourceSessionID, body, author})
	if err != nil {
		return "", err
	}
	// Page and Sheet keep their shared namespace. Widget creation has no native
	// content snapshot and uses its own namespace and initial-draft fingerprint.
	item.ID = "native_" + store.ContentHash(keys[0])
	if item.Kind == store.StudioItemKindWidget {
		item.ID = "widget_" + store.ContentHash(keys[0])
	}
	return store.ContentHash(string(payload)), nil
}

func replayStudioCreation(ctx context.Context, tx *sql.Tx, id, fingerprint string) (*store.StudioItem, error) {
	item, err := scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=?`, id))
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if item.Deleted || item.ArchivedAt != nil {
		return nil, store.ErrNotFound
	}
	var saved string
	if err := tx.QueryRowContext(ctx, `SELECT request_hash FROM studio_item_saves WHERE item_id=? AND client_request_id='create'`, id).Scan(&saved); err != nil {
		return nil, err
	}
	if saved != fingerprint {
		return nil, &store.InvalidContent{Message: "clientRequestID reused with different creation parameters"}
	}
	return item, nil
}

func replayNativeCreation(ctx context.Context, tx *sql.Tx, id, fingerprint string) (*store.ContentSnapshot, error) {
	item, err := replayStudioCreation(ctx, tx, id, fingerprint)
	if err != nil || item == nil {
		return nil, err
	}
	// Read the current head, not the initial content: a retry never undoes edits.
	return scanContentSnapshot(tx.QueryRowContext(ctx, `SELECT w.id,c.body,c.content_hash,w.head_revision,c.content_format FROM studio_items w JOIN studio_item_content c ON c.item_id=w.id WHERE w.id=?`, id))
}
