CREATE TABLE IF NOT EXISTS external_agent_bindings (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    runtime_id TEXT NOT NULL,
    agent_name TEXT NOT NULL DEFAULT '',
    invite_hash TEXT UNIQUE,
    expires_at INTEGER NOT NULL,
    credential_hash TEXT UNIQUE
);
CREATE INDEX IF NOT EXISTS external_agent_session ON external_agent_bindings(session_id);
