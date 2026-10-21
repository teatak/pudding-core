package sqlitestore

import (
	"database/sql"
	"fmt"
)

// These are data transformations inside one v47 transaction, not independently
// committed schema destinations. Versions only select the remaining work for
// databases created by earlier development builds. v40/41 and the withdrawn v47
// experiment need no transformation; existing experimental data stays untouched.
var studioMigrationSteps = []struct {
	version int
	migrate schemaMigration
}{
	{33, migrateWidgetData},
	{34, migrateWidgetOrigins},
	{35, migrateWidgetPages},
	{36, migrateWidgetPagePins},
	{37, migrateWidgetParticipantIdentities},
	{38, migrateWireEstimates},
	{39, migrateSubtaskArchives},
	{42, migrateSheets},
	{43, migratePages},
	{44, migratePageEdits},
	{45, migratePageDocuments},
	{46, migratePageNames},
}

func migrateStudioV47(tx *sql.Tx) error {
	var source int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&source); err != nil {
		return err
	}
	if source < 32 || source > 47 {
		return fmt.Errorf("unsupported Studio migration source v%d", source)
	}
	for _, step := range studioMigrationSteps {
		if step.version <= source {
			continue
		}
		if err := step.migrate(tx); err != nil {
			return fmt.Errorf("Studio transformation v%d: %w", step.version, err)
		}
	}
	// Validate while changes are still rollbackable, before recording v47.
	contract := extendSchemaContract(currentSchemaContract, nil)
	delete(contract.tables, "session_plugin_connections")
	return validateSchemaWithRetiredColumns(tx, contract)
}

func migrateWireEstimates(tx *sql.Tx) error {
	_, err := tx.Exec(`UPDATE session_usage SET last_estimated_input_tokens=0`)
	return err
}

func migrateSubtaskArchives(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS subtask_archives (
 child_session_id TEXT PRIMARY KEY REFERENCES session_children(child_session_id) ON DELETE CASCADE,
 archived_at INTEGER NOT NULL
);`)
	return err
}
