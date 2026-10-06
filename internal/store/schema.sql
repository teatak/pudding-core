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
    loaded_plugin_ids TEXT NOT NULL DEFAULT '[]',
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

CREATE TABLE studio_mounts (
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 id TEXT NOT NULL,
 item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
 visible INTEGER NOT NULL DEFAULT 1,
 created_at INTEGER NOT NULL,
 PRIMARY KEY(session_id,id),
 UNIQUE(session_id,item_id)
);

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
    kind TEXT NOT NULL CHECK(kind IN ('studio','web')),
    source_session_id TEXT NOT NULL DEFAULT '',
    saved_item_id TEXT REFERENCES studio_items(id) ON DELETE CASCADE,
    url TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    CHECK ((kind='studio' AND saved_item_id IS NOT NULL AND url='')
        OR (kind='web' AND saved_item_id IS NULL AND url<>''))
);
CREATE UNIQUE INDEX IF NOT EXISTS library_favorites_studio ON library_favorites(saved_item_id) WHERE kind='studio';
CREATE UNIQUE INDEX IF NOT EXISTS library_favorites_web ON library_favorites(url) WHERE kind='web';

CREATE TABLE IF NOT EXISTS library_recent_opens (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK(kind IN ('file','studio')),
    source_session_id TEXT NOT NULL,
    studio_mount_id TEXT,
    root_path TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL DEFAULT '',
    opened_at INTEGER NOT NULL,
    FOREIGN KEY(source_session_id,studio_mount_id) REFERENCES studio_mounts(session_id,id) ON DELETE CASCADE,
    CHECK ((kind='studio' AND studio_mount_id IS NOT NULL AND root_path='' AND path='')
        OR (kind='file' AND studio_mount_id IS NULL AND root_path<>'' AND path<>''))
);
CREATE UNIQUE INDEX IF NOT EXISTS library_recent_studio ON library_recent_opens(source_session_id,studio_mount_id) WHERE kind='studio';
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

CREATE TABLE studio_items (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK(kind IN ('doc','table','widget')),
    name TEXT NOT NULL,
    icon TEXT NOT NULL DEFAULT '',
    icon_color TEXT NOT NULL DEFAULT '',
    source_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    revision INTEGER NOT NULL,
    head_revision TEXT NOT NULL,
    active_revision TEXT NOT NULL,
    bindings TEXT NOT NULL,
    binding_version INTEGER NOT NULL,
    deleted INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    archived_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX studio_items_archived_at ON studio_items(archived_at);
CREATE TABLE studio_item_revisions (
    item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
    hash TEXT NOT NULL,
    parent_revision TEXT NOT NULL,
    client_request_id TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    build_receipt TEXT NOT NULL,
    content TEXT,
    content_hash TEXT NOT NULL DEFAULT '',
    author_kind TEXT NOT NULL DEFAULT '',
    author_session_id TEXT NOT NULL DEFAULT '',
    author_turn_id TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(item_id, hash),
    UNIQUE(item_id, client_request_id)
);
CREATE TABLE studio_item_content (
    item_id TEXT PRIMARY KEY REFERENCES studio_items(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    content_hash TEXT NOT NULL
);
CREATE TABLE studio_item_saves (
    item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
    client_request_id TEXT NOT NULL,
    hash TEXT NOT NULL,
    request_hash TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(item_id, client_request_id)
);

CREATE TABLE widget_actions (
 id TEXT PRIMARY KEY,
 item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
 client_request_id TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('prepared','executing','succeeded','failed','unknown')),
 spec TEXT NOT NULL,
 result TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL,
 UNIQUE(item_id,client_request_id)
);

CREATE TABLE widget_links (
 id TEXT PRIMARY KEY,
 item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
 left_entity TEXT NOT NULL,
 right_entity TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 UNIQUE(item_id,left_entity,right_entity)
);

CREATE TABLE studio_table_ids (
    item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
    entity_kind TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    PRIMARY KEY (item_id, entity_kind, entity_id)
);

CREATE TABLE remote_identity (
    singleton INTEGER PRIMARY KEY CHECK (singleton=1),
    desktop_id TEXT NOT NULL UNIQUE
);
CREATE TABLE remote_pairings (
    id TEXT PRIMARY KEY,
    mode TEXT NOT NULL CHECK (mode IN ('lan','relay')),
    origin TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','requested','approved')),
    code_hash TEXT UNIQUE,
    poll_hash TEXT UNIQUE,
    device_name TEXT NOT NULL DEFAULT '',
    expires_at INTEGER NOT NULL
);
CREATE TABLE remote_devices (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('lan','relay')),
    origin TEXT NOT NULL,
    credential_hash TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
