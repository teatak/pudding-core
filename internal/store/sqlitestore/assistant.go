package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const assistantSchema = `CREATE TABLE IF NOT EXISTS assistants (
    id TEXT PRIMARY KEY CHECK (id = 'personal'),
    name TEXT NOT NULL,
    user_name TEXT NOT NULL,
    appearance TEXT NOT NULL,
    revision INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS assistant_memories (
    id TEXT PRIMARY KEY,
    assistant_id TEXT NOT NULL REFERENCES assistants(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    scope TEXT NOT NULL,
    source TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);`

const communicationPreferenceID = "communication_preference"

func (s *Store) PrepareAssistantDeletion(ctx context.Context, sessionID string) error {
	return s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := requireAssistantConversation(ctx, tx, sessionID); err != nil {
			return err
		}
		return closeSessionAdmissionTx(ctx, tx, sessionID, time.Now())
	})
}

func (s *Store) DeleteAssistant(ctx context.Context) error {
	return s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		current, err := readAssistant(ctx, tx)
		if err != nil || current.Assistant == nil {
			return err
		}
		if id := current.Assistant.SessionID; id != "" {
			if err := deleteSessionTx(ctx, tx, id); err != nil {
				return err
			}
		}
		// Cascades clear preferences, memories, their search index and binding.
		_, err = tx.ExecContext(ctx, `DELETE FROM assistants WHERE id=?`, store.PersonalAssistantID)
		return err
	})
}

const assistantSessionsSchema = `CREATE TABLE IF NOT EXISTS assistant_sessions (
    assistant_id TEXT PRIMARY KEY REFERENCES assistants(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL UNIQUE REFERENCES sessions(id) ON DELETE CASCADE
);`

func requireOrdinaryConversationTx(ctx context.Context, tx *sql.Tx, id string) error {
	var assistant bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM assistant_sessions WHERE session_id=?)`, id).Scan(&assistant); err != nil {
		return err
	}
	if assistant {
		return store.ErrAssistantConversationProtected
	}
	return nil
}

func migrateAssistantV52(tx *sql.Tx) error {
	// Early v51 development databases stored only palette. Upgrade the actual
	// saved selection once; runtime reads use one canonical appearance shape.
	legacy, err := tableColumnExists(tx, "assistants", "palette")
	if err != nil {
		return err
	}
	if legacy {
		if _, err := tx.Exec(`ALTER TABLE assistants RENAME COLUMN palette TO appearance;
UPDATE assistants SET appearance=json_object('palette',appearance,'body','classic','eyes','classic','mouth','classic','headwear','antenna','arms','classic','eyewear','none','faceSticker','blush');`); err != nil {
			return err
		}
	}
	_, err = tx.Exec(assistantSessionsSchema)
	return err
}

func migrateAssistantPaletteV55(tx *sql.Tx) error {
	_, err := tx.Exec(`UPDATE assistants SET appearance=json_set(appearance,'$.palette','apricot'),revision=revision+1,updated_at=max(updated_at+1,?) WHERE json_extract(appearance,'$.palette')='christmas'`, time.Now().UTC().UnixMilli())
	return err
}

func migrateAssistantV53(tx *sql.Tx) error {
	// The old "none" omitted goggles but still rendered the built-in mask.
	// Name that existing choice explicitly; future "none" means a bare face.
	_, err := tx.Exec(`UPDATE assistants SET appearance=json_set(appearance,'$.eyewear','classic'),revision=revision+1,updated_at=max(updated_at+1,?) WHERE json_extract(appearance,'$.eyewear')='none'`, time.Now().UTC().UnixMilli())
	return err
}

func readAssistant(ctx context.Context, tx *sql.Tx) (*store.AssistantSnapshot, error) {
	out := &store.AssistantSnapshot{Memories: []store.AssistantMemory{}}
	a := &store.Assistant{AppearanceFavorites: []store.AssistantAppearanceFavorite{}}
	var appearance, history string
	var created, updated int64
	err := tx.QueryRowContext(ctx, `SELECT a.id,a.name,a.user_name,a.appearance,a.revision,a.created_at,a.updated_at,coalesce(c.session_id,''),coalesce(h.history,'[]') FROM assistants a LEFT JOIN assistant_sessions c ON c.assistant_id=a.id LEFT JOIN assistant_appearance_history h ON h.assistant_id=a.id WHERE a.id=?`, store.PersonalAssistantID).
		Scan(&a.ID, &a.Name, &a.UserName, &appearance, &a.Revision, &created, &updated, &a.SessionID, &history)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(appearance), &a.Appearance); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(history), &a.AppearanceHistory); err != nil {
		return nil, err
	}
	a.CreatedAt, a.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	out.Assistant = a
	if err := readAssistantAppearanceFavorites(ctx, tx, a); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+assistantMemoryColumns+` FROM assistant_memories m WHERE assistant_id=? ORDER BY created_at,id`, a.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanAssistantMemory(rows)
		if err != nil {
			return nil, err
		}
		out.Memories = append(out.Memories, *m)
	}
	return out, rows.Err()
}

func (s *Store) GetAssistant(ctx context.Context) (*store.AssistantSnapshot, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out, err := readAssistant(ctx, tx)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// Opening supplies defaults only once. Concurrent opens and lost responses
// return the same identity and conversation, without resetting custom settings.
func (s *Store) OpenAssistant(ctx context.Context, defaultName, provider, model string, appearance store.AssistantAppearance) (*store.AssistantSnapshot, error) {
	initialAppearance, err := json.Marshal(appearance)
	if err != nil {
		return nil, err
	}
	var out *store.AssistantSnapshot
	err = s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		now := time.Now().UTC().UnixMilli()
		_, err := tx.ExecContext(ctx, `INSERT INTO assistants(id,name,user_name,appearance,revision,created_at,updated_at) VALUES(?,'','',?,1,?,?) ON CONFLICT(id) DO NOTHING`,
			store.PersonalAssistantID, string(initialAppearance), now, now)
		if err != nil {
			return err
		}
		out, err = readAssistant(ctx, tx)
		if err != nil || out.Assistant.SessionID != "" || provider == "" {
			return err
		}
		session := &store.Session{ID: store.NewID("sess"), Title: out.Assistant.Name, Provider: provider, Model: model}
		if session.Title == "" {
			session.Title = defaultName
		}
		if err := store.NormalizeSessionProviderModel(session); err != nil {
			return err
		}
		if err := createSessionTx(ctx, tx, session); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO assistant_sessions(assistant_id,session_id) VALUES(?,?)`, store.PersonalAssistantID, session.ID); err != nil {
			return err
		}
		out.Assistant.SessionID = session.ID
		return nil
	})
	return out, err
}

// Setup/profile and its explicit preference commit together. Replaying the same
// saved form is harmless; a stale form with different contents cannot overwrite it.
func (s *Store) SaveAssistant(ctx context.Context, in store.SaveAssistantInput) (*store.AssistantSnapshot, error) {
	var out *store.AssistantSnapshot
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		current, err := readAssistant(ctx, tx)
		if err != nil {
			return err
		}
		var revision int64
		if a := current.Assistant; a != nil {
			revision = a.Revision
			preference := ""
			for _, m := range current.Memories {
				if m.ID == communicationPreferenceID {
					preference = m.Content
				}
			}
			if a.Name == in.Name && a.UserName == in.UserName && a.Appearance.Equal(in.Appearance) && preference == in.CommunicationPreference {
				out = current
				return nil
			}
		}
		if revision != *in.ExpectedRevision {
			return store.ErrAssistantConflict
		}
		now := time.Now().UTC().UnixMilli()
		appearance, err := json.Marshal(in.Appearance)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO assistants(id,name,user_name,appearance,revision,created_at,updated_at) VALUES(?,?,?,?,1,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name,user_name=excluded.user_name,appearance=excluded.appearance,revision=assistants.revision+1,updated_at=excluded.updated_at`, store.PersonalAssistantID, in.Name, in.UserName, string(appearance), now, now)
		if err != nil {
			return err
		}
		if a := current.Assistant; a != nil && !a.Appearance.Equal(in.Appearance) {
			if err := saveAssistantAppearanceHistory(ctx, tx, a, in); err != nil {
				return err
			}
		}
		if in.CommunicationPreference == "" {
			_, err = tx.ExecContext(ctx, `DELETE FROM assistant_memories WHERE id=?`, communicationPreferenceID)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO assistant_memories(id,assistant_id,content,scope,source,created_at,updated_at) VALUES(?,?,?,'personal','user_settings',?,?)
ON CONFLICT(id) DO UPDATE SET content=excluded.content,source='user_settings',source_session_id='',source_turn_id='',revision=assistant_memories.revision+1,updated_at=excluded.updated_at WHERE content<>excluded.content`, communicationPreferenceID, store.PersonalAssistantID, in.CommunicationPreference, now, now)
		}
		if err != nil {
			return err
		}
		if in.CommunicationPreference != "" {
			if err := indexAssistantMemory(ctx, tx, communicationPreferenceID, in.CommunicationPreference); err != nil {
				return err
			}
		}
		out, err = readAssistant(ctx, tx)
		return err
	})
	return out, err
}

func migrateAssistantAppearanceV56(tx *sql.Tx) error {
	// Preserve every saved part and any client extension; only add the format version.
	_, err := tx.Exec(`UPDATE assistants SET appearance=json_set(appearance,'$.version',1) WHERE json_type(appearance,'$.version') IS NULL`)
	return err
}
