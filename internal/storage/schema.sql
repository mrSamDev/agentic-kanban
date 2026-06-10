-- Single-row sequence table. The rowid acts as primary key so INSERT OR IGNORE
-- on an explicit rowid prevents duplicate rows across init calls.
CREATE TABLE IF NOT EXISTS task_seq (
    id    INTEGER PRIMARY KEY CHECK (id = 1),
    next_id INTEGER NOT NULL DEFAULT 0
);

INSERT OR IGNORE INTO task_seq (id, next_id) VALUES (1, 0);

CREATE TABLE IF NOT EXISTS tasks (
    id            TEXT PRIMARY KEY,                  -- e.g. 'TASK-101'
    title         TEXT NOT NULL,
    status        TEXT NOT NULL CHECK(status IN
                  ('TODO','IN_PROGRESS','BLOCKED','IN_REVIEW','DONE')),
    role_boundary TEXT NOT NULL,                     -- 'worker' | 'reviewer' | ...
    project       TEXT NOT NULL DEFAULT 'default',   -- project/scope label
    priority      INTEGER NOT NULL DEFAULT 100,      -- lower = more urgent
    assigned_agent TEXT,                             -- current lease holder, nullable
    lease_until   DATETIME,                          -- nullable when unclaimed
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tasks_claim
    ON tasks(role_boundary, status, priority, created_at, lease_until);
CREATE INDEX IF NOT EXISTS idx_tasks_claim_project
    ON tasks(role_boundary, project, status, priority, created_at, lease_until);
CREATE INDEX IF NOT EXISTS idx_tasks_lease
    ON tasks(status, lease_until);
CREATE INDEX IF NOT EXISTS idx_tasks_project
    ON tasks(project, status, priority);

CREATE TABLE IF NOT EXISTS notes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    TEXT NOT NULL,
    author     TEXT NOT NULL,
    note_type  TEXT,                                 -- PROGRESS|ERROR|DECISION|REVIEW|SYSTEM
    content    TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS history (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    TEXT NOT NULL,
    agent      TEXT NOT NULL,
    action     TEXT NOT NULL,                        -- DISPATCH|CLAIM|PROGRESS|BLOCK|COMPLETE|REVIEW
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_notes_task
    ON notes(task_id, id);
CREATE INDEX IF NOT EXISTS idx_history_task
    ON history(task_id, id);

CREATE TABLE IF NOT EXISTS events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    event_type TEXT NOT NULL,
    payload    TEXT NOT NULL,
    ttl_seconds INTEGER DEFAULT 259200  -- 3 days; NULL = never expires
);

CREATE INDEX IF NOT EXISTS idx_events_ttl
    ON events(created_at, ttl_seconds);