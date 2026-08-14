package sqlitestore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const (
	baselineSchemaVersion      = 1
	currentSchemaLayoutVersion = 8
	currentSchemaVersion       = 11
)

var (
	ErrUnsupportedSchema = errors.New("sqlite: unsupported database schema")
	ErrSchemaTooNew      = errors.New("sqlite: database schema is newer than this application")
)

type schemaMigration func(*sql.Tx) error

// schemaMigrations is keyed by the destination version. Version 1 is the
// signed 0.1.1 baseline and is bootstrapped separately for existing databases.
var schemaMigrations = map[int]schemaMigration{
	2: func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			CREATE TABLE turn_file_changes (
				id TEXT PRIMARY KEY,
				session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
				turn_id TEXT NOT NULL REFERENCES turns(id) ON DELETE CASCADE,
				root_path TEXT NOT NULL,
				path TEXT NOT NULL,
				original_path TEXT NOT NULL DEFAULT '',
				kind TEXT NOT NULL,
				additions INTEGER NOT NULL DEFAULT 0,
				deletions INTEGER NOT NULL DEFAULT 0,
				binary INTEGER NOT NULL DEFAULT 0,
				too_large INTEGER NOT NULL DEFAULT 0,
				old_size INTEGER NOT NULL DEFAULT 0,
				new_size INTEGER NOT NULL DEFAULT 0,
				old_content TEXT NOT NULL DEFAULT '',
				new_content TEXT NOT NULL DEFAULT '',
				created_at INTEGER NOT NULL
			);
			CREATE INDEX turn_file_changes_turn ON turn_file_changes(session_id, turn_id, path);
		`)
		return err
	},
	3: func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			ALTER TABLE canvas_items RENAME TO canvas_items_v2;
			ALTER TABLE canvas_closed_items RENAME TO canvas_closed_items_v2;
			DROP INDEX canvas_items_canvas_visible_updated;
			DROP INDEX canvas_closed_items_closed_at;

			CREATE TABLE canvas_saved_items (
				id TEXT PRIMARY KEY,
				source_session_id TEXT NOT NULL DEFAULT '',
				source_item_id TEXT NOT NULL DEFAULT '',
				kind TEXT NOT NULL DEFAULT '',
				title TEXT NOT NULL DEFAULT '',
				item_json TEXT NOT NULL,
				window_json TEXT NOT NULL DEFAULT '',
				revision INTEGER NOT NULL DEFAULT 1,
				created_at INTEGER NOT NULL,
				updated_at INTEGER NOT NULL
			);

			INSERT INTO canvas_saved_items(
				id,source_session_id,source_item_id,kind,title,item_json,window_json,revision,created_at,updated_at
			)
			SELECT id,source_session_id,id,kind,title,item_json,window_json,1,created_at,updated_at
			FROM canvas_items_v2
			WHERE source_session_id='' OR NOT EXISTS(SELECT 1 FROM sessions WHERE sessions.id=canvas_items_v2.source_session_id);

			INSERT OR IGNORE INTO canvas_saved_items(
				id,source_session_id,source_item_id,kind,title,item_json,window_json,revision,created_at,updated_at
			)
			SELECT 'legacy_closed_' || id,actor_session_id,source_item_id,kind,title,item_json,window_json,1,created_at,updated_at
			FROM canvas_closed_items_v2
			WHERE actor_session_id='' OR NOT EXISTS(SELECT 1 FROM sessions WHERE sessions.id=canvas_closed_items_v2.actor_session_id);

			CREATE TABLE canvas_items (
				session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
				id TEXT NOT NULL,
				canvas_id TEXT NOT NULL DEFAULT 'default',
				source_session_id TEXT NOT NULL DEFAULT '',
				created_by_session_id TEXT NOT NULL DEFAULT '',
				updated_by_session_id TEXT NOT NULL DEFAULT '',
				kind TEXT NOT NULL DEFAULT '',
				title TEXT NOT NULL DEFAULT '',
				item_json TEXT NOT NULL,
				window_json TEXT NOT NULL DEFAULT '',
				source_saved_item_id TEXT NOT NULL DEFAULT '',
				base_saved_revision INTEGER NOT NULL DEFAULT 0,
				saved_dirty INTEGER NOT NULL DEFAULT 0,
				visible INTEGER NOT NULL DEFAULT 1,
				created_at INTEGER NOT NULL,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY(session_id,id)
			);

			INSERT INTO canvas_items(
				session_id,id,canvas_id,source_session_id,created_by_session_id,updated_by_session_id,
				kind,title,item_json,window_json,visible,created_at,updated_at
			)
			SELECT source_session_id,id,canvas_id,source_session_id,created_by_session_id,updated_by_session_id,
				kind,title,item_json,window_json,visible,created_at,updated_at
			FROM canvas_items_v2
			WHERE source_session_id<>'' AND EXISTS(SELECT 1 FROM sessions WHERE sessions.id=canvas_items_v2.source_session_id);

			CREATE TABLE canvas_closed_items (
				session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
				id TEXT NOT NULL,
				source_item_id TEXT NOT NULL,
				actor_session_id TEXT NOT NULL DEFAULT '',
				kind TEXT NOT NULL DEFAULT '',
				title TEXT NOT NULL DEFAULT '',
				item_json TEXT NOT NULL,
				window_json TEXT NOT NULL DEFAULT '',
				closed_at INTEGER NOT NULL,
				created_at INTEGER NOT NULL,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY(session_id,id),
				UNIQUE(session_id,source_item_id)
			);

			INSERT INTO canvas_closed_items(
				session_id,id,source_item_id,actor_session_id,kind,title,item_json,window_json,closed_at,created_at,updated_at
			)
			SELECT actor_session_id,id,source_item_id,actor_session_id,kind,title,item_json,window_json,closed_at,created_at,updated_at
			FROM canvas_closed_items_v2
			WHERE actor_session_id<>'' AND EXISTS(SELECT 1 FROM sessions WHERE sessions.id=canvas_closed_items_v2.actor_session_id);

			DROP TABLE canvas_items_v2;
			DROP TABLE canvas_closed_items_v2;

			CREATE INDEX canvas_items_canvas_visible_updated ON canvas_items(session_id,visible,updated_at DESC);
			CREATE UNIQUE INDEX canvas_items_session_saved ON canvas_items(session_id,source_saved_item_id) WHERE source_saved_item_id<>'';
			CREATE INDEX canvas_saved_items_updated_at ON canvas_saved_items(updated_at DESC);
			CREATE INDEX canvas_closed_items_closed_at ON canvas_closed_items(session_id,closed_at DESC);
		`)
		return err
	},
	4: func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS browser_history (
				id TEXT NOT NULL,
				url TEXT NOT NULL,
				title TEXT NOT NULL DEFAULT '',
				favicon_url TEXT NOT NULL DEFAULT '',
				visited_at INTEGER NOT NULL,
				created_at INTEGER NOT NULL,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY(id),
				UNIQUE(url)
			);
			CREATE INDEX IF NOT EXISTS browser_history_visited_at
				ON browser_history(visited_at DESC);
		`)
		return err
	},
	5: func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS usage_calibrations (
				provider TEXT NOT NULL,
				model TEXT NOT NULL,
				sample_count INTEGER NOT NULL DEFAULT 0,
				input_ratio_ewma REAL NOT NULL DEFAULT 1,
				last_estimated_input_tokens INTEGER NOT NULL DEFAULT 0,
				last_actual_input_tokens INTEGER NOT NULL DEFAULT 0,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY(provider,model)
			);
		`)
		return err
	},
	6: func(tx *sql.Tx) error {
		exists, err := tableColumnExists(tx, "turn_file_changes", "origin")
		if err != nil || exists {
			return err
		}
		_, err = tx.Exec(`
			ALTER TABLE turn_file_changes
				ADD COLUMN origin TEXT NOT NULL DEFAULT 'structured';
		`)
		return err
	},
	7: func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS project_app_bindings (
				id TEXT PRIMARY KEY,
				project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
				app_id TEXT NOT NULL,
				connection_id TEXT NOT NULL,
				resource_type TEXT NOT NULL,
				resource_id TEXT NOT NULL,
				resource_name TEXT NOT NULL,
				metadata TEXT NOT NULL DEFAULT '{}',
				is_primary INTEGER NOT NULL DEFAULT 0,
				created_at INTEGER NOT NULL,
				updated_at INTEGER NOT NULL,
				UNIQUE(project_id, app_id, resource_type, resource_id)
			);
			CREATE UNIQUE INDEX IF NOT EXISTS project_app_bindings_primary
				ON project_app_bindings(project_id, app_id) WHERE is_primary = 1;
			CREATE INDEX IF NOT EXISTS project_app_bindings_connection
				ON project_app_bindings(connection_id);
		`)
		return err
	},
	8: func(tx *sql.Tx) error {
		_, err := tx.Exec(`DROP TABLE IF EXISTS project_app_bindings`)
		return err
	},
	9: func(tx *sql.Tx) error {
		return removeLoadedAppIDs(tx, "project-files", "source-control", "code-intelligence")
	},
	10: func(tx *sql.Tx) error {
		exists, err := tableColumnExists(tx, "sessions", "archived_at")
		if err != nil {
			return err
		}
		if !exists {
			if _, err := tx.Exec(`ALTER TABLE sessions ADD COLUMN archived_at INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}
		}
		_, err = tx.Exec(`CREATE INDEX IF NOT EXISTS sessions_archived_at ON sessions(archived_at)`)
		return err
	},
	11: func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS computer_app_grants (
				session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
				app_id TEXT NOT NULL,
				created_at INTEGER NOT NULL,
				PRIMARY KEY(session_id, app_id)
			)
		`)
		return err
	},
}

func removeLoadedAppIDs(tx *sql.Tx, removedIDs ...string) error {
	removed := make(map[string]bool, len(removedIDs))
	for _, id := range removedIDs {
		if id = strings.TrimSpace(id); id != "" {
			removed[id] = true
		}
	}
	rows, err := tx.Query(`SELECT id,loaded_app_ids FROM sessions`)
	if err != nil {
		return err
	}
	type sessionApps struct {
		id  string
		ids []string
	}
	updates := make([]sessionApps, 0)
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var ids []string
		if err := json.Unmarshal([]byte(raw), &ids); err != nil {
			rows.Close()
			return err
		}
		filtered := make([]string, 0, len(ids))
		changed := false
		for _, appID := range ids {
			if removed[strings.TrimSpace(appID)] {
				changed = true
				continue
			}
			filtered = append(filtered, appID)
		}
		if changed {
			updates = append(updates, sessionApps{id: id, ids: store.NormalizeAppIDs(filtered)})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, update := range updates {
		raw, err := json.Marshal(update.ids)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE sessions SET loaded_app_ids=? WHERE id=?`, string(raw), update.id); err != nil {
			return err
		}
	}
	return nil
}

func tableColumnExists(tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func prepareSchema(db *sql.DB, path string) error {
	version, err := schemaVersion(db)
	if err != nil {
		return err
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("%w: found v%d, support ends at v%d", ErrSchemaTooNew, version, currentSchemaVersion)
	}
	if version == 0 {
		empty, err := schemaIsEmpty(db)
		if err != nil {
			return err
		}
		if empty {
			if err := createCurrentSchema(db); err != nil {
				return err
			}
			version = currentSchemaVersion
		} else {
			if err := validateSchema(db, currentSchemaContract); err == nil {
				// Version 9 is data-only and cannot be inferred from the schema.
				// Stamp the latest identifiable layout so migration 9 still runs.
				if err := setSchemaVersion(db, currentSchemaLayoutVersion); err != nil {
					return err
				}
				version = currentSchemaLayoutVersion
			} else if err := validateSchema(db, schemaV5Contract); err == nil {
				if err := setSchemaVersion(db, 5); err != nil {
					return err
				}
				version = 5
			} else if err := validateSchema(db, schemaV4Contract); err == nil {
				if err := setSchemaVersion(db, 4); err != nil {
					return err
				}
				version = 4
			} else if err := validateSchema(db, schemaV3Contract); err == nil {
				if err := setSchemaVersion(db, 3); err != nil {
					return err
				}
				version = 3
			} else if err := validateSchema(db, schemaV2Contract); err == nil {
				if err := setSchemaVersion(db, 2); err != nil {
					return err
				}
				version = 2
			} else {
				if err := validateSchema(db, schemaV1Contract); err != nil {
					return fmt.Errorf("%w: unversioned database does not match the signed 0.1.1 baseline: %v", ErrUnsupportedSchema, err)
				}
				if err := setSchemaVersion(db, baselineSchemaVersion); err != nil {
					return err
				}
				version = baselineSchemaVersion
			}
		}
	}
	migrated := version < currentSchemaVersion
	migrationBackupPath := ""
	if migrated {
		migrationBackupPath, err = backupDatabaseBeforeMigration(db, path, version)
		if err != nil {
			return err
		}
		for next := version + 1; next <= currentSchemaVersion; next++ {
			migration, ok := schemaMigrations[next]
			if !ok {
				return fmt.Errorf("sqlite: missing migration to schema v%d", next)
			}
			if err := runSchemaMigration(db, next, migration); err != nil {
				return err
			}
		}
	}
	if err := validateCurrentSchema(db); err != nil {
		return fmt.Errorf("%w: schema v%d failed validation: %v", ErrUnsupportedSchema, currentSchemaVersion, err)
	}
	if migrated {
		// Cleanup is best effort and only follows a successful schema migration.
		_ = cleanupMigrationBackups(path, migrationBackupPath)
	}
	return nil
}

func schemaVersion(db *sql.DB) (int, error) {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("sqlite: read schema version: %w", err)
	}
	return version, nil
}

func schemaIsEmpty(db *sql.DB) (bool, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&count); err != nil {
		return false, fmt.Errorf("sqlite: inspect schema: %w", err)
	}
	return count == 0, nil
}

func createCurrentSchema(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("sqlite: begin schema creation: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(store.SchemaSQL); err != nil {
		return fmt.Errorf("sqlite: create schema: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, currentSchemaVersion)); err != nil {
		return fmt.Errorf("sqlite: set schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit schema creation: %w", err)
	}
	return nil
}

func setSchemaVersion(db *sql.DB, version int) error {
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
		return fmt.Errorf("sqlite: set schema version: %w", err)
	}
	return nil
}

func runSchemaMigration(db *sql.DB, version int, migration schemaMigration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("sqlite: begin migration to v%d: %w", version, err)
	}
	defer tx.Rollback()
	if err := migration(tx); err != nil {
		return fmt.Errorf("sqlite: migrate to v%d: %w", version, err)
	}
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
		return fmt.Errorf("sqlite: record migration v%d: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit migration v%d: %w", version, err)
	}
	return nil
}

func backupDatabaseBeforeMigration(db *sql.DB, path string, version int) (string, error) {
	if path == ":memory:" {
		return "", nil
	}
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return "", fmt.Errorf("sqlite: checkpoint before migration: %w", err)
	}
	source, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("sqlite: open migration backup source: %w", err)
	}
	defer source.Close()
	backupPath := fmt.Sprintf("%s.backup-v%d-%s", path, version, time.Now().UTC().Format("20060102T150405.000000000Z"))
	target, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("sqlite: create migration backup: %w", err)
	}
	ok := false
	defer func() {
		_ = target.Close()
		if !ok {
			_ = os.Remove(backupPath)
		}
	}()
	if _, err := io.Copy(target, source); err != nil {
		return "", fmt.Errorf("sqlite: write migration backup: %w", err)
	}
	if err := target.Sync(); err != nil {
		return "", fmt.Errorf("sqlite: sync migration backup: %w", err)
	}
	if err := target.Close(); err != nil {
		return "", fmt.Errorf("sqlite: close migration backup: %w", err)
	}
	ok = true
	return backupPath, nil
}

func cleanupMigrationBackups(path, keepPath string) error {
	if path == ":memory:" {
		return nil
	}
	if keepPath == "" {
		return errors.New("sqlite: migration backup cleanup requires a backup to keep")
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("sqlite: list migration backups: %w", err)
	}
	keepName := filepath.Base(keepPath)
	backups := make([]string, 0)
	keepFound := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		_, ok := migrationBackupTimestamp(base, entry.Name())
		if ok {
			backups = append(backups, entry.Name())
			keepFound = keepFound || entry.Name() == keepName
		}
	}
	if !keepFound {
		return fmt.Errorf("sqlite: migration backup to keep not found: %s", keepName)
	}
	if len(backups) <= 1 {
		return nil
	}
	var cleanupErr error
	for _, candidate := range backups {
		if candidate == keepName {
			continue
		}
		if err := os.Remove(filepath.Join(dir, candidate)); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("sqlite: remove migration backup %s: %w", candidate, err))
		}
	}
	return cleanupErr
}

func migrationBackupTimestamp(databaseName, candidate string) (time.Time, bool) {
	const timestampFormat = "20060102T150405.000000000Z"
	prefix := databaseName + ".backup-v"
	if !strings.HasPrefix(candidate, prefix) {
		return time.Time{}, false
	}
	versionAndTimestamp := strings.TrimPrefix(candidate, prefix)
	version, timestamp, ok := strings.Cut(versionAndTimestamp, "-")
	if !ok || version == "" || timestamp == "" {
		return time.Time{}, false
	}
	for i := range version {
		if version[i] < '0' || version[i] > '9' {
			return time.Time{}, false
		}
	}
	parsed, err := time.Parse(timestampFormat, timestamp)
	return parsed, err == nil
}

type schemaContract struct {
	tables          map[string][]string
	indexes         []string
	forbiddenTables []string
}

var schemaV1Contract = schemaContract{
	tables: map[string][]string{
		"sessions":             {"id", "title", "provider", "model", "reasoning_effort", "reasoning_model_key", "active_mode", "mode_lease", "project_id", "loaded_app_ids", "pinned", "pinned_order", "created_at", "updated_at", "last_activity_at"},
		"projects":             {"id", "name", "root_dirs", "approval_mode", "created_at", "updated_at"},
		"turns":                {"id", "session_id", "client_message_id", "status", "provider", "model", "mode", "model_config", "error", "created_at", "updated_at"},
		"queued_inputs":        {"session_id", "client_message_id", "text", "parts", "status", "provider", "model", "mode", "model_config", "turn_id", "created_at", "updated_at"},
		"usage":                {"hour_start_at", "model", "request_count", "input_uncached_tokens", "input_cached_tokens", "cache_creation_tokens", "output_content_tokens", "output_reasoning_tokens", "updated_at"},
		"session_usage":        {"session_id", "request_count", "last_input_uncached_tokens", "last_input_cached_tokens", "last_cache_creation_tokens", "last_output_content_tokens", "last_output_reasoning_tokens", "cumulative_input_uncached_tokens", "cumulative_input_cached_tokens", "cumulative_cache_creation_tokens", "cumulative_output_content_tokens", "cumulative_output_reasoning_tokens", "updated_at"},
		"canvas_items":         {"id", "canvas_id", "source_session_id", "created_by_session_id", "updated_by_session_id", "kind", "title", "item_json", "window_json", "visible", "created_at", "updated_at"},
		"canvas_closed_items":  {"id", "source_item_id", "actor_session_id", "kind", "title", "item_json", "window_json", "closed_at", "created_at", "updated_at"},
		"session_browser_tabs": {"session_id", "tab_id", "url", "title", "favicon_url", "mode", "created_at", "updated_at"},
		"messages":             {"id", "session_id", "turn_id", "role", "kind", "text", "search_tokens", "parts", "turn_index", "metadata", "client_message_id", "interrupted", "created_at"},
		"events":               {"session_id", "seq", "kind", "turn_id", "payload", "created_at"},
	},
	indexes: []string{
		"turns_one_running",
		"queued_inputs_session_active",
		"canvas_items_canvas_visible_updated",
		"canvas_closed_items_closed_at",
		"session_browser_tabs_updated_at",
		"messages_session_created",
		"messages_turn_index",
	},
}

var schemaV2Contract = extendSchemaContract(schemaV1Contract, map[string][]string{
	"turn_file_changes": {"id", "session_id", "turn_id", "root_path", "path", "original_path", "kind", "additions", "deletions", "binary", "too_large", "old_size", "new_size", "old_content", "new_content", "created_at"},
}, "turn_file_changes_turn")

var schemaV3Contract = func() schemaContract {
	out := extendSchemaContract(schemaV2Contract, map[string][]string{
		"canvas_saved_items": {"id", "source_session_id", "source_item_id", "kind", "title", "item_json", "window_json", "revision", "created_at", "updated_at"},
	}, "canvas_items_session_saved", "canvas_saved_items_updated_at")
	out.tables["canvas_items"] = append(out.tables["canvas_items"], "session_id", "source_saved_item_id", "base_saved_revision", "saved_dirty")
	out.tables["canvas_closed_items"] = append(out.tables["canvas_closed_items"], "session_id")
	return out
}()

var schemaV4Contract = extendSchemaContract(schemaV3Contract, map[string][]string{
	"browser_history": {"id", "url", "title", "favicon_url", "visited_at", "created_at", "updated_at"},
}, "browser_history_visited_at")

var schemaV5Contract = extendSchemaContract(schemaV4Contract, map[string][]string{
	"usage_calibrations": {"provider", "model", "sample_count", "input_ratio_ewma", "last_estimated_input_tokens", "last_actual_input_tokens", "updated_at"},
})

var currentSchemaContract = func() schemaContract {
	out := extendSchemaContract(schemaV5Contract, map[string][]string{
		"computer_app_grants": {"session_id", "app_id", "created_at"},
	})
	out.tables["turn_file_changes"] = append(out.tables["turn_file_changes"], "origin")
	out.tables["sessions"] = append(out.tables["sessions"], "archived_at")
	out.indexes = append(out.indexes, "sessions_archived_at")
	out.forbiddenTables = []string{"project_app_bindings"}
	return out
}()

func extendSchemaContract(base schemaContract, tables map[string][]string, indexes ...string) schemaContract {
	out := schemaContract{tables: make(map[string][]string, len(base.tables)+len(tables))}
	for name, columns := range base.tables {
		out.tables[name] = append([]string(nil), columns...)
	}
	for name, columns := range tables {
		out.tables[name] = append([]string(nil), columns...)
	}
	out.indexes = append(append([]string(nil), base.indexes...), indexes...)
	return out
}

func validateCurrentSchema(db *sql.DB) error {
	return validateSchema(db, currentSchemaContract)
}

func validateSchema(db *sql.DB, contract schemaContract) error {
	for table, required := range contract.tables {
		columns, err := tableColumns(db, table)
		if err != nil {
			return err
		}
		for _, column := range required {
			if _, ok := columns[column]; !ok {
				return fmt.Errorf("table %s is missing column %s", table, column)
			}
		}
	}
	for _, index := range contract.indexes {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, index).Scan(&count); err != nil {
			return fmt.Errorf("inspect index %s: %w", index, err)
		}
		if count == 0 {
			return fmt.Errorf("missing index %s", index)
		}
	}
	for _, table := range contract.forbiddenTables {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			return fmt.Errorf("inspect removed table %s: %w", table, err)
		}
		if count != 0 {
			return fmt.Errorf("removed table %s still exists", table)
		}
	}
	return nil
}

func tableColumns(db *sql.DB, table string) (map[string]struct{}, error) {
	quoted := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
	rows, err := db.Query(`PRAGMA table_info(` + quoted + `)`)
	if err != nil {
		return nil, fmt.Errorf("inspect table %s: %w", table, err)
	}
	defer rows.Close()
	columns := make(map[string]struct{})
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("missing table %s", table)
	}
	return columns, nil
}
