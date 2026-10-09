package sqlitestore

import "database/sql"

// Business roles now live in author-owned page data. Preserve caller IDs and
// session bindings, removing only the obsolete host role field.
func migrateWidgetParticipantIdentities(tx *sql.Tx) error {
	_, err := tx.Exec(`UPDATE widget_pages SET interaction=json_set(interaction, '$.participants',
   json((SELECT json_group_array(json_remove(value, '$.roles')) FROM json_each(widget_pages.interaction, '$.participants'))))
   WHERE interaction<>''`)
	return err
}
