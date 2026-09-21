-- schema 契约(docs/technology-decisions.md 第 6 节)。
-- SQLite 实现(轨道 A)嵌入本文件执行;memstore 以此为语义参照。
-- 运行参数由实现负责:PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; 单 writer。

CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    title      TEXT    NOT NULL DEFAULT '',
    provider   TEXT    NOT NULL, -- provider profile 名;创建 session 时必须显式写入
    model      TEXT    NOT NULL,
    reasoning_effort TEXT NOT NULL DEFAULT '',
    reasoning_model_key TEXT NOT NULL DEFAULT '',
    active_mode TEXT   NOT NULL DEFAULT 'chat',
    mode_lease  TEXT   NOT NULL DEFAULT 'none',
    project_id TEXT NOT NULL DEFAULT '',
    loaded_app_ids TEXT NOT NULL DEFAULT '[]',
    pinned     INTEGER NOT NULL DEFAULT 0,
    pinned_order INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL, -- unix ms
    updated_at INTEGER NOT NULL,
    last_activity_at INTEGER NOT NULL,
    archived_at INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS sessions_archived_at
    ON sessions(archived_at);

-- Immutable ownership only; messages and execution state stay on Session/Turn.
CREATE TABLE IF NOT EXISTS session_children (
    child_session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    parent_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    CHECK (child_session_id <> parent_session_id)
);
CREATE INDEX IF NOT EXISTS session_children_parent
    ON session_children(parent_session_id);

CREATE TABLE IF NOT EXISTS computer_app_grants (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    app_id     TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (session_id, app_id)
);

CREATE TABLE IF NOT EXISTS projects (
    id            TEXT PRIMARY KEY,
    name          TEXT    NOT NULL DEFAULT '',
    root_dirs     TEXT    NOT NULL DEFAULT '[]',
    approval_mode TEXT    NOT NULL DEFAULT 'auto',
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    last_activity_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS turns (
    id                TEXT PRIMARY KEY,
    session_id        TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    client_message_id TEXT    NOT NULL,
    status            TEXT    NOT NULL,
    provider          TEXT    NOT NULL DEFAULT '', -- BeginTurn 时刻快照
    model             TEXT    NOT NULL DEFAULT '',
    mode              TEXT    NOT NULL DEFAULT 'chat',
    model_config      TEXT    NOT NULL DEFAULT '{}',
    error             TEXT    NOT NULL DEFAULT '',
    retry_of_turn_id  TEXT    NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    -- submit 幂等键:同 session 内 clientMessageID 唯一
    UNIQUE (session_id, client_message_id)
);

-- 第一阶段不允许并发 turn:每个 session 至多一个 running(开放问题第 14 节)
CREATE UNIQUE INDEX IF NOT EXISTS turns_one_running
    ON turns(session_id) WHERE status = 'running';

CREATE UNIQUE INDEX IF NOT EXISTS turns_one_retry
    ON turns(session_id,retry_of_turn_id) WHERE retry_of_turn_id <> '';

CREATE TABLE IF NOT EXISTS turn_file_changes (
    id            TEXT PRIMARY KEY,
    session_id    TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    turn_id       TEXT    NOT NULL REFERENCES turns(id) ON DELETE CASCADE,
    root_path     TEXT    NOT NULL,
    path          TEXT    NOT NULL,
    original_path TEXT    NOT NULL DEFAULT '',
    kind          TEXT    NOT NULL,
    origin        TEXT    NOT NULL DEFAULT 'structured',
    additions     INTEGER NOT NULL DEFAULT 0,
    deletions     INTEGER NOT NULL DEFAULT 0,
    binary        INTEGER NOT NULL DEFAULT 0,
    too_large     INTEGER NOT NULL DEFAULT 0,
    old_size      INTEGER NOT NULL DEFAULT 0,
    new_size      INTEGER NOT NULL DEFAULT 0,
    old_content   TEXT    NOT NULL DEFAULT '',
    new_content   TEXT    NOT NULL DEFAULT '',
    snapshot_version INTEGER NOT NULL DEFAULT 0,
    old_digest    TEXT    NOT NULL DEFAULT '',
    new_digest    TEXT    NOT NULL DEFAULT '',
    old_mode      INTEGER NOT NULL DEFAULT 0,
    new_mode      INTEGER NOT NULL DEFAULT 0,
    old_type      TEXT    NOT NULL DEFAULT '',
    new_type      TEXT    NOT NULL DEFAULT '',
    old_binary    INTEGER NOT NULL DEFAULT 0,
    new_binary    INTEGER NOT NULL DEFAULT 0,
    old_data      BLOB    NOT NULL DEFAULT X'',
    new_data      BLOB    NOT NULL DEFAULT X'',
    created_at    INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS turn_file_changes_turn
    ON turn_file_changes(session_id, turn_id, path);

CREATE TABLE IF NOT EXISTS turn_file_change_states (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    turn_id    TEXT NOT NULL REFERENCES turns(id) ON DELETE CASCADE,
    state      TEXT NOT NULL DEFAULT 'applied',
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (session_id, turn_id)
);

CREATE TABLE IF NOT EXISTS queued_inputs (
    session_id        TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    client_message_id TEXT    NOT NULL,
    text              TEXT    NOT NULL,
    parts             TEXT    NOT NULL DEFAULT '[]',
    status            TEXT    NOT NULL,
    provider          TEXT    NOT NULL DEFAULT '',
    model             TEXT    NOT NULL DEFAULT '',
    mode              TEXT    NOT NULL DEFAULT 'chat',
    model_config      TEXT    NOT NULL DEFAULT '{}',
    turn_id           TEXT    NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    sort_order        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (session_id, client_message_id)
);

CREATE INDEX IF NOT EXISTS queued_inputs_session_active
    ON queued_inputs(session_id, sort_order)
    WHERE status IN ('queued','editing','cancelled');

CREATE TABLE IF NOT EXISTS usage (
    hour_start_at             INTEGER NOT NULL, -- unix ms, UTC hour boundary
    model                     TEXT    NOT NULL DEFAULT '',
    request_count             INTEGER NOT NULL DEFAULT 0,
    input_uncached_tokens     INTEGER NOT NULL DEFAULT 0,
    input_cached_tokens       INTEGER NOT NULL DEFAULT 0,
    cache_creation_tokens     INTEGER NOT NULL DEFAULT 0,
    output_content_tokens     INTEGER NOT NULL DEFAULT 0,
    output_reasoning_tokens   INTEGER NOT NULL DEFAULT 0,
    updated_at                INTEGER NOT NULL,
    PRIMARY KEY (hour_start_at, model)
);

CREATE TABLE IF NOT EXISTS session_usage (
    session_id                         TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    request_count                      INTEGER NOT NULL DEFAULT 0,
    last_provider                      TEXT    NOT NULL DEFAULT '',
    last_model                         TEXT    NOT NULL DEFAULT '',
    last_estimated_input_tokens        INTEGER NOT NULL DEFAULT 0,
    last_input_uncached_tokens         INTEGER NOT NULL DEFAULT 0,
    last_input_cached_tokens           INTEGER NOT NULL DEFAULT 0,
    last_cache_creation_tokens         INTEGER NOT NULL DEFAULT 0,
    last_output_content_tokens         INTEGER NOT NULL DEFAULT 0,
    last_output_reasoning_tokens       INTEGER NOT NULL DEFAULT 0,
    cumulative_input_uncached_tokens   INTEGER NOT NULL DEFAULT 0,
    cumulative_input_cached_tokens     INTEGER NOT NULL DEFAULT 0,
    cumulative_cache_creation_tokens   INTEGER NOT NULL DEFAULT 0,
    cumulative_output_content_tokens   INTEGER NOT NULL DEFAULT 0,
    cumulative_output_reasoning_tokens INTEGER NOT NULL DEFAULT 0,
    updated_at                         INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS canvas_items (
    session_id            TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    id                    TEXT    NOT NULL,
    canvas_id             TEXT    NOT NULL DEFAULT 'default',
    source_session_id     TEXT    NOT NULL DEFAULT '',
    created_by_session_id TEXT    NOT NULL DEFAULT '',
    updated_by_session_id TEXT    NOT NULL DEFAULT '',
    kind                  TEXT    NOT NULL DEFAULT '',
    title                 TEXT    NOT NULL DEFAULT '',
    item_json             TEXT    NOT NULL,
    window_json           TEXT    NOT NULL DEFAULT '',
    source_saved_item_id  TEXT    NOT NULL DEFAULT '',
    base_saved_revision   INTEGER NOT NULL DEFAULT 0,
    saved_dirty           INTEGER NOT NULL DEFAULT 0,
    visible               INTEGER NOT NULL DEFAULT 1,
    created_at            INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL,
    PRIMARY KEY (session_id, id)
);

CREATE INDEX IF NOT EXISTS canvas_items_canvas_visible_updated
    ON canvas_items(session_id, visible, updated_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS canvas_items_session_saved
    ON canvas_items(session_id, source_saved_item_id)
    WHERE source_saved_item_id <> '';

CREATE TABLE IF NOT EXISTS canvas_saved_items (
    id                TEXT    PRIMARY KEY,
    source_session_id TEXT    NOT NULL DEFAULT '',
    source_item_id    TEXT    NOT NULL DEFAULT '',
    kind              TEXT    NOT NULL DEFAULT '',
    title             TEXT    NOT NULL DEFAULT '',
    item_json         TEXT    NOT NULL,
    window_json       TEXT    NOT NULL DEFAULT '',
    revision          INTEGER NOT NULL DEFAULT 1,
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS canvas_saved_items_updated_at
    ON canvas_saved_items(updated_at DESC);

CREATE TABLE IF NOT EXISTS session_browser_tabs (
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    tab_id      TEXT NOT NULL DEFAULT '',
    url         TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL DEFAULT '',
    favicon_url TEXT NOT NULL DEFAULT '',
    mode        TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (session_id, tab_id)
);

CREATE INDEX IF NOT EXISTS session_browser_tabs_updated_at
    ON session_browser_tabs(session_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS browser_history (
    id          TEXT NOT NULL,
    url         TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    favicon_url TEXT NOT NULL DEFAULT '',
    visited_at  INTEGER NOT NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (url)
);

CREATE INDEX IF NOT EXISTS browser_history_visited_at
    ON browser_history(visited_at DESC);

CREATE TABLE IF NOT EXISTS messages (
    id                TEXT PRIMARY KEY,
    session_id        TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    turn_id           TEXT    NOT NULL DEFAULT '',
    role              TEXT    NOT NULL,
    kind              TEXT    NOT NULL DEFAULT '',
    text              TEXT    NOT NULL,
    search_tokens     TEXT    NOT NULL DEFAULT '', -- 由应用层分词生成,不进入 canonical context
    parts             TEXT    NOT NULL DEFAULT '[]',
    turn_index        INTEGER NOT NULL DEFAULT 0,
    metadata          TEXT    NOT NULL DEFAULT '{}',
    client_message_id TEXT    NOT NULL DEFAULT '', -- 仅 user message
    interrupted       INTEGER NOT NULL DEFAULT 0,  -- cancel/failed 保留的半截输出
    created_at        INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS messages_session_created
    ON messages(session_id, created_at);

CREATE INDEX IF NOT EXISTS messages_turn_index
    ON messages(session_id, turn_id, turn_index);

-- events 只存 lifecycle 事件(turn.delta / ping 不落库);
-- seq 为 per-session 单调递增,在写入事务内分配。
-- retention:按条数或天数滚动清理,只需保住 SSE 续传窗口(第 6 节)。
CREATE TABLE IF NOT EXISTS events (
    session_id TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,
    kind       TEXT    NOT NULL,
    turn_id    TEXT    NOT NULL DEFAULT '',
    payload    TEXT    NOT NULL, -- 完整 Event JSON
    created_at INTEGER NOT NULL,
    PRIMARY KEY (session_id, seq)
);

CREATE TABLE IF NOT EXISTS library_favorites (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK(kind IN ('canvas','web')),
    source_session_id TEXT NOT NULL DEFAULT '',
    saved_item_id TEXT REFERENCES canvas_saved_items(id) ON DELETE CASCADE,
    url TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    CHECK ((kind='canvas' AND saved_item_id IS NOT NULL AND url='')
        OR (kind='web' AND saved_item_id IS NULL AND url<>''))
);
CREATE UNIQUE INDEX IF NOT EXISTS library_favorites_canvas ON library_favorites(saved_item_id) WHERE kind='canvas';
CREATE UNIQUE INDEX IF NOT EXISTS library_favorites_web ON library_favorites(url) WHERE kind='web';

CREATE TABLE IF NOT EXISTS library_recent_opens (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK(kind IN ('file','canvas')),
    source_session_id TEXT NOT NULL,
    canvas_item_id TEXT,
    root_path TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL DEFAULT '',
    opened_at INTEGER NOT NULL,
    FOREIGN KEY(source_session_id,canvas_item_id) REFERENCES canvas_items(session_id,id) ON DELETE CASCADE,
    CHECK ((kind='canvas' AND canvas_item_id IS NOT NULL AND root_path='' AND path='')
        OR (kind='file' AND canvas_item_id IS NULL AND root_path<>'' AND path<>''))
);
CREATE UNIQUE INDEX IF NOT EXISTS library_recent_canvas ON library_recent_opens(source_session_id,canvas_item_id) WHERE kind='canvas';
CREATE UNIQUE INDEX IF NOT EXISTS library_recent_file ON library_recent_opens(root_path,path) WHERE kind='file';
CREATE INDEX IF NOT EXISTS library_recent_opened ON library_recent_opens(opened_at DESC,id DESC);

CREATE TABLE IF NOT EXISTS session_dispatches (
    child_session_id TEXT PRIMARY KEY REFERENCES session_children(child_session_id) ON DELETE CASCADE,
    parent_turn_id TEXT NOT NULL REFERENCES turns(id) ON DELETE CASCADE,
    call_id TEXT NOT NULL,
    UNIQUE(parent_turn_id, call_id)
);
CREATE TABLE IF NOT EXISTS collaboration_stops (
    parent_turn_id TEXT PRIMARY KEY REFERENCES turns(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS scheduled_tasks (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    prompt TEXT NOT NULL,
    schedule TEXT NOT NULL,
    enabled INTEGER NOT NULL,
    deleted INTEGER NOT NULL DEFAULT 0,
    revision INTEGER NOT NULL,
    schedule_revision INTEGER NOT NULL,
    next_at INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    request_id TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    UNIQUE(session_id, request_id)
);
CREATE INDEX IF NOT EXISTS scheduled_tasks_due ON scheduled_tasks(enabled, deleted, next_at);
CREATE TABLE IF NOT EXISTS scheduled_task_runs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES scheduled_tasks(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    prompt TEXT NOT NULL,
    definition_revision INTEGER NOT NULL,
    source TEXT NOT NULL,
    scheduled_for INTEGER NOT NULL,
    accepted_at INTEGER NOT NULL,
    client_message_id TEXT NOT NULL UNIQUE,
    handoff TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    skipped_through INTEGER NOT NULL DEFAULT 0,
    trigger_key TEXT NOT NULL,
  schedule TEXT NOT NULL,
    UNIQUE(task_id, trigger_key)
);
CREATE INDEX IF NOT EXISTS scheduled_task_runs_task ON scheduled_task_runs(task_id, accepted_at);
CREATE INDEX IF NOT EXISTS scheduled_task_runs_pending ON scheduled_task_runs(handoff, accepted_at);
