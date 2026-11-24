CREATE TABLE IF NOT EXISTS local_agent_sessions (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    state TEXT NOT NULL
);
