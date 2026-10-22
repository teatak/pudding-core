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
func prepareNativeCreation(item *store.StudioItem, body any, author store.ContentAuthor, keys []string) (string, error) {
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
	// Both kinds share the namespace: reusing a key for another kind is an error.
	item.ID = "native_" + store.ContentHash(keys[0])
	return store.ContentHash(string(payload)), nil
}

func replayNativeCreation(ctx context.Context, tx *sql.Tx, id, fingerprint string) (*store.ContentSnapshot, error) {
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
	// Read the current head, not the initial content: a retry never undoes edits.
	return scanContentSnapshot(tx.QueryRowContext(ctx, `SELECT w.id,c.body,c.content_hash,w.head_revision,c.content_format FROM studio_items w JOIN studio_item_content c ON c.item_id=w.id WHERE w.id=?`, id))
}
