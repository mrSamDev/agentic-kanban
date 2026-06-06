# Agent Coordination Engine — Final Spec (v1)

A persistent, no-UI, single-binary CLI in Go over SQLite, for coordinating shared
state between cooperating agents. Compiles to one `./kanban` binary reading one
local `.db` file. No daemon, no network, no queue.

This spec merges the original blueprint with review feedback and corrects two
points where the feedback was right-for-the-wrong-reason or premature.

---

## 1. Decisions carried over (these were good)

- **Intent-based commands**, not generic CRUD. Commands map 1:1 to agent intents.
- **Markdown skill layer**: capabilities exposed as plain `.md` files fed into an
  agent's context. No tool-calling protocol.
- **Role directories** (`manager/`, `worker/`, `reviewer/`) to scope which skills
  an agent sees. Note: this is *soft*, prompt-level scoping, not a sandbox — a
  worker could still run any subcommand if told to. Fine for cooperative agents.
- **Lease-based crash recovery**: a crashed agent's lease expires and the task
  becomes reclaimable.
- **SQLite + WAL**. Correct tool for this scale.

## 2. Corrections to the review

- **Keep one agent field for v1, not `assigned_agent` + `claimed_by`.** This design
  has no manager→agent assignment; workers self-claim by role. `assigned_agent`
  simply means "current lease holder." Add a second field only when real direct
  assignment exists.
- **Drop the `version` column — but for the right reason.** Optimistic locking is
  unnecessary, but the lease does *not* prevent the claim race. SQLite serializing
  writes does, *only if* `claim-next` runs as one atomic write transaction. See §5.

## 3. Things both the plan and the review missed

1. **`IN_REVIEW` state.** A reviewer role exists but the status enum had no review
   state. Added below.
2. **Lease renewal.** A fixed 15-min lease double-claims any task longer than 15
   min. Fix: `log-progress` extends the lease as a side effect (acts as heartbeat).
3. **SQLite foreign keys are OFF by default.** `ON DELETE CASCADE` does nothing
   unless every connection runs `PRAGMA foreign_keys = ON`.
4. **Stable JSON + exit-code contract** so bash-driven agents can parse reliably,
   including a defined "no work available" signal.
5. **ID scheme.** Use human-readable sequential IDs (`TASK-101`) over UUIDs.

---

## 4. Schema (`internal/storage/schema.sql`, embedded)

```sql
CREATE TABLE IF NOT EXISTS tasks (
    id            TEXT PRIMARY KEY,                  -- e.g. 'TASK-101'
    title         TEXT NOT NULL,
    status        TEXT NOT NULL CHECK(status IN
                  ('TODO','IN_PROGRESS','BLOCKED','IN_REVIEW','DONE')),
    role_boundary TEXT NOT NULL,                     -- 'worker' | 'reviewer' | ...
    priority      INTEGER NOT NULL DEFAULT 100,      -- lower = more urgent
    assigned_agent TEXT,                             -- current lease holder, nullable
    lease_until   DATETIME,                          -- nullable when unclaimed
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tasks_claim
    ON tasks(role_boundary, status, priority, created_at);

CREATE TABLE IF NOT EXISTS notes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    TEXT NOT NULL,
    author     TEXT NOT NULL,
    note_type  TEXT,                                 -- PROGRESS|ERROR|DECISION|REVIEW|SYSTEM (v3)
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

-- v2 (deferred): task dependencies
-- CREATE TABLE IF NOT EXISTS task_dependencies (
--     task_id    TEXT NOT NULL,
--     depends_on TEXT NOT NULL,
--     PRIMARY KEY (task_id, depends_on)
-- );
```

### Required pragmas — run on EVERY connection open

```sql
PRAGMA journal_mode = WAL;     -- persists in the db file, but harmless to re-run
PRAGMA busy_timeout = 5000;    -- per-connection; handles concurrent writers
PRAGMA foreign_keys = ON;      -- per-connection; REQUIRED for cascades to work
```

---

## 5. The claim race — the one thing that must be exactly right

Two idle workers calling `claim-next` simultaneously must never both get the same
task. Do the whole claim as a single atomic write transaction and check the result.

```sql
BEGIN IMMEDIATE;                       -- take the write lock up front

UPDATE tasks
   SET status         = 'IN_PROGRESS',
       assigned_agent = :agent,
       lease_until    = datetime('now', '+15 minutes'),
       updated_at     = CURRENT_TIMESTAMP
 WHERE id = (
        SELECT id FROM tasks
         WHERE role_boundary = :role
           AND ( status = 'TODO'
                 OR (status = 'IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP) )
         ORDER BY priority ASC, created_at ASC
         LIMIT 1 )
RETURNING *;                           -- empty result = no work available

-- same transaction: INSERT into history (action='CLAIM') for the returned id
COMMIT;
```

Why this is safe without a `version` column: SQLite allows only one write
transaction at a time. The second worker's `UPDATE` re-evaluates the subquery
against already-committed state and selects a different row (or none). The
`status` guard inside the WHERE is the lightweight optimistic check.

Lease reclaim is **lazy**: a stale task is only recovered when someone next calls
`claim-next`. There is no background reaper — consistent with "zero infrastructure."

---

## 6. Commands

| Command | Role | Logic |
|---|---|---|
| `task dispatch --title --role [--priority]` | manager | Insert new task as `TODO`. history: DISPATCH. |
| `task claim-next --agent --role` | worker, reviewer | Atomic claim per §5. Returns task JSON or empty. history: CLAIM. |
| `task log-progress <id> --agent --note [--type]` | worker | Append note. **Also renews lease** (`lease_until = now + 15m`). history: PROGRESS. |
| `task block <id> --agent --reason` | worker | Clear lease, status → `BLOCKED`, append reason note. history: BLOCK. |
| `task complete <id> --agent` | worker | Clear lease, status → `DONE` (or `IN_REVIEW` if review required). history: COMPLETE. |
| `task view <id>` | all | Full task + notes + history as JSON. First-class — agents get a bare id and need context. |
| `task search [--status] [--role] [--agent]` | manager, reviewer | Filtered list. Managers use this constantly. |

Reviewer flow uses `claim-next --role reviewer` to pick up `IN_REVIEW` tasks, then
an approve path that sets `DONE` (or sends back to `TODO` on rejection).

### Output contract

- Default output is **JSON** on stdout. Stable field names matching the schema.
- `claim-next` with no work: exit `0`, empty object `{}` (or `null`). Agents treat
  this as "idle, nothing to do."
- Not-found / invalid state transition: exit `2` + `{"error": "..."}` on stderr.
- Every state-changing command writes its `history` row **inside the same
  transaction** as the state change, so a crash never leaves an unaudited change.

---

## 7. Skill layer (unchanged in shape)

```
skills/
├── manager/   dispatch-task.md   review-backlog.md
├── worker/    claim-next-task.md log-progress.md  block-task.md
└── reviewer/  claim-review.md    approve-task.md
```

Each `.md` describes when to call the skill, the exact bash command, and how to
read the JSON output. Group by role to control what each agent's context contains.

---

## 8. Build order

**Milestone 1 — get the loop correct**
tasks + history tables, pragmas (WAL/busy_timeout/foreign_keys), timestamps,
`dispatch` / `claim-next` (atomic) / `complete` / `view`, JSON + exit-code contract.

**Milestone 2 — resilience**
leases + lease renewal via `log-progress`, `block`, `search`, `IN_REVIEW` state and
reviewer commands.

**Milestone 3 — scheduling depth**
priority ordering tuned, task dependencies (claim-next gains a `NOT EXISTS`
unmet-dependency guard), note types, role-directory boundaries.

Role boundaries come last because they're nearly free (which files go in which
directory) and don't change scheduling behavior — unlike priority and dependencies.

---

## 9. Project layout

```
cmd/kanban/main.go            # CLI entrypoint, subcommand routing
internal/
├── task/
│   ├── service.go            # intent logic: Claim, Block, Complete, etc.
│   └── model.go              # structs <-> rows
└── storage/
    ├── sqlite.go             # connection setup + pragmas + transactions
    └── schema.sql            # embedded via go:embed, migrated on first run
skills/                       # markdown skill files by role
```