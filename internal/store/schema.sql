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
    workspace_dirs TEXT NOT NULL DEFAULT '[]',
    pinned     INTEGER NOT NULL DEFAULT 0,
    pinned_order INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL, -- unix ms
    updated_at INTEGER NOT NULL,
    last_activity_at INTEGER NOT NULL
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
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    -- submit 幂等键:同 session 内 clientMessageID 唯一
    UNIQUE (session_id, client_message_id)
);

-- 第一阶段不允许并发 turn:每个 session 至多一个 running(开放问题第 14 节)
CREATE UNIQUE INDEX IF NOT EXISTS turns_one_running
    ON turns(session_id) WHERE status = 'running';

CREATE TABLE IF NOT EXISTS queued_inputs (
    session_id        TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    client_message_id TEXT    NOT NULL,
    text              TEXT    NOT NULL,
    attachments       TEXT    NOT NULL DEFAULT '[]',
    local_folders     TEXT    NOT NULL DEFAULT '[]',
    parts             TEXT    NOT NULL DEFAULT '[]',
    status            TEXT    NOT NULL,
    provider          TEXT    NOT NULL DEFAULT '',
    model             TEXT    NOT NULL DEFAULT '',
    mode              TEXT    NOT NULL DEFAULT 'chat',
    model_config      TEXT    NOT NULL DEFAULT '{}',
    turn_id           TEXT    NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    PRIMARY KEY (session_id, client_message_id)
);

CREATE INDEX IF NOT EXISTS queued_inputs_session_active
    ON queued_inputs(session_id, created_at)
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
    id                    TEXT    PRIMARY KEY,
    canvas_id             TEXT    NOT NULL DEFAULT 'default',
    source_session_id     TEXT    NOT NULL DEFAULT '',
    created_by_session_id TEXT    NOT NULL DEFAULT '',
    updated_by_session_id TEXT    NOT NULL DEFAULT '',
    kind                  TEXT    NOT NULL DEFAULT '',
    title                 TEXT    NOT NULL DEFAULT '',
    item_json             TEXT    NOT NULL,
    window_json           TEXT    NOT NULL DEFAULT '',
    visible               INTEGER NOT NULL DEFAULT 1,
    created_at            INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS canvas_items_canvas_visible_updated
    ON canvas_items(canvas_id, visible, updated_at DESC);

-- 最近关闭的小组件。后端只保留有限历史,避免前端存储积累大列表。
CREATE TABLE IF NOT EXISTS canvas_closed_items (
    id               TEXT    PRIMARY KEY,
    source_item_id   TEXT    NOT NULL UNIQUE,
    actor_session_id TEXT    NOT NULL DEFAULT '',
    kind             TEXT    NOT NULL DEFAULT '',
    title            TEXT    NOT NULL DEFAULT '',
    item_json        TEXT    NOT NULL,
    window_json      TEXT    NOT NULL DEFAULT '',
    closed_at        INTEGER NOT NULL,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS canvas_closed_items_closed_at
    ON canvas_closed_items(closed_at DESC);

CREATE TABLE IF NOT EXISTS session_browser_state (
    session_id  TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    tab_id      TEXT NOT NULL DEFAULT '',
    url         TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL DEFAULT '',
    favicon_url TEXT NOT NULL DEFAULT '',
    mode        TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
    id                TEXT PRIMARY KEY,
    session_id        TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    turn_id           TEXT    NOT NULL DEFAULT '',
    role              TEXT    NOT NULL,
    kind              TEXT    NOT NULL DEFAULT '',
    text              TEXT    NOT NULL,
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
