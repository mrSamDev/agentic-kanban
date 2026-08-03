---
name: taskbind
version: 0.1.13-alpha
language: Go (1.25.0)
database: SQLite (modernc.org/sqlite, pure Go, no CGo)
cli_framework: cobra (spf13/cobra)
license: MIT
status: alpha
repository: github.com/mrSamDev/taskbind
description: >
  SQLite-backed coordination protocol for AI agents. No server, no daemon,
  no queues. One shared .db file, multiple agents claim, track, review, and
  finish work through a single Go binary.
architecture: >
  Single binary, shared SQLite file. Agents coordinate through the database,
  not through each other. Every state change is a transaction. JSON stdout
  for agent consumption. Embedded skill files teach agents the protocol.
---

# taskbind — Project Reference

## 1. What It Is

taskbind is a **coordination protocol** for multiple AI agents working on the
same project. It is not a task queue, not a job scheduler, not a workflow
engine. It is a shared SQLite database with a CLI that agents call to claim,
track, review, and finish work.

**Core insight:** Agents don't talk to each other. They talk to the database.
The database is the single source of truth. If an agent crashes, its lease
expires and another agent picks up the work. No server, no message broker,
no orchestrator process.

**Target scale:** 3-10 concurrent agents, up to 50. Past 50, SQLite
contention becomes the bottleneck. Past 1000, switch to Postgres or a
distributed queue.

---

## 2. State Machine

Every task moves through exactly five states. Transitions are enforced at
the database level via CHECK constraints and application-level guards.

```
TODO ── claim ──> IN_PROGRESS ── complete --review ──> IN_REVIEW ── approve ──> DONE
                        │                                      │
                        │ block                                │ reject
                        ▼                                      ▼
                     BLOCKED                                  TODO
```

### State Definitions

| State | Meaning | Can Be Claimed? | Has Lease? | Terminal? |
|-------|---------|-----------------|------------|-----------|
| `TODO` | Ready for work. Anyone with matching role can claim it. | Yes | No | No |
| `IN_PROGRESS` | An agent is working on it. Has a lease timer. | No (unless lease expired) | Yes (15 min) | No |
| `IN_REVIEW` | Work is done, needs approval. Reviewer can approve or reject. | No | No | No |
| `DONE` | Finished. Terminal state. | No | No | Yes |
| `BLOCKED` | Can't proceed. Requires manager intervention. | No | No | No |

### Allowed Transitions

| From | To | Trigger | Who |
|------|----|---------|-----|
| (none) | TODO | `dispatch` | manager |
| TODO | IN_PROGRESS | `claim-next` or `claim` | worker |
| IN_PROGRESS | IN_REVIEW | `complete --review` | assigned agent |
| IN_PROGRESS | DONE | `complete` | assigned agent |
| IN_PROGRESS | BLOCKED | `block` | assigned agent |
| IN_REVIEW | DONE | `approve` | reviewer (not the claimer) |
| IN_REVIEW | TODO | `reject` | reviewer (not the claimer) |
| TODO | (any) | `set-priority` | manager |
| IN_PROGRESS | IN_PROGRESS | `transfer` | assigned agent → new agent |
| IN_PROGRESS | IN_PROGRESS | `extend-lease` | assigned agent |
| IN_PROGRESS | IN_PROGRESS | `log-progress` | assigned agent |

---

## 3. Lease Model

The lease is the core ownership mechanism. It prevents two agents from
working on the same task and provides crash recovery.

### How Leases Work

1. When an agent claims a task, the task gets:
   - `assigned_agent` set to the agent's name
   - `lease_until` set to `now + 15 minutes`
2. Only the lease holder can modify the task (complete, block, log progress)
3. The lease is renewed on every `log-progress` call (heartbeat)
4. The lease can be extended explicitly via `extend-lease --minutes N`
5. If the agent crashes, the lease expires after 15 minutes
6. The next `claim-next` call reclaims expired leases lazily

### Lease Reclamation

Leases are reclaimed **lazily** — only when another agent calls `claim-next`
for the same role. There is no background reaper. The `claim-next` query
selects tasks where:

```sql
status = 'TODO'
OR (status = 'IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP)
```

This means an orphaned `IN_PROGRESS` task stays orphaned until another agent
of the same role tries to claim work.

### Lease Constants

| Parameter | Value | Location |
|-----------|-------|----------|
| Default lease duration | 15 minutes | `internal/task/helpers.go` |
| Max candidate fetch | 100 | `internal/task/helpers.go` |
| Context timeout | 30 seconds | `internal/task/helpers.go` |
| Retry attempts | 3 | `internal/task/helpers.go` |
| Retry base delay | 100ms (exponential + jitter) | `internal/task/helpers.go` |

---

## 4. Database Schema

### 4.1 Tables

#### `task_seq` — ID Sequence

```sql
CREATE TABLE task_seq (
    id    INTEGER PRIMARY KEY CHECK (id = 1),
    next_id INTEGER NOT NULL DEFAULT 0
);
```

Single-row table. `next_id` is atomically incremented via
`UPDATE ... RETURNING next_id` inside a transaction. IDs are formatted as
`TASK-{N}`. The sequence is seeded from `MAX(CAST(substr(id,6) AS INTEGER))`
on every database open to prevent ID reuse after task deletion.

#### `tasks` — Core Task Table

```sql
CREATE TABLE tasks (
    id            TEXT PRIMARY KEY,                  -- 'TASK-101'
    title         TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL CHECK(status IN
                  ('TODO','IN_PROGRESS','BLOCKED','IN_REVIEW','DONE')),
    role_boundary TEXT NOT NULL,                     -- 'worker' | 'reviewer' | ...
    project       TEXT NOT NULL DEFAULT 'default',   -- project/scope label
    priority      INTEGER NOT NULL DEFAULT 100,      -- lower = more urgent
    assigned_agent TEXT,                             -- current lease holder, nullable
    lease_until   DATETIME,                          -- nullable when unclaimed
    claimed_by    TEXT,                               -- immutable snapshot of who claimed it (for self-review check)
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Indexes:**

| Index | Columns | Purpose |
|-------|---------|---------|
| `idx_tasks_claim` | `(role_boundary, status, priority, created_at, lease_until)` | Claim-next query (TODO + expired leases) |
| `idx_tasks_claim_project` | `(role_boundary, project, status, priority, created_at, lease_until)` | Project-scoped claim-next |
| `idx_tasks_lease` | `(status, lease_until)` | Lease expiry queries |
| `idx_tasks_project` | `(project, status, priority)` | Project-scoped searches |

**Note:** The claim query uses an `OR` on status (`status = 'TODO' OR (status = 'IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP)`) which partially defeats the composite index. A `UNION ALL` of two separate queries would be more efficient.

#### `notes` — Task Notes

```sql
CREATE TABLE notes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    TEXT NOT NULL,
    author     TEXT NOT NULL,
    note_type  TEXT,                                 -- PROGRESS|ERROR|DECISION|REVIEW|SYSTEM
    content    TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
```

**Index:** `idx_notes_task` on `(task_id, id)`

#### `history` — Action Log

```sql
CREATE TABLE history (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    TEXT NOT NULL,
    agent      TEXT NOT NULL,
    action     TEXT NOT NULL,                        -- DISPATCH|CLAIM|PROGRESS|BLOCK|COMPLETE|REVIEW|TRANSFER
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
```

**Index:** `idx_history_task` on `(task_id, id)`, `idx_history_created_at` on `(created_at)`

#### `events` — CDC Event Log

```sql
CREATE TABLE events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    event_type TEXT NOT NULL,
    payload    TEXT NOT NULL
);
```

**Index:** `idx_events_created_at` on `(created_at)`

Append-only CDC log. No TTL or auto-prune. Clean manually via
`taskbind prune --before 30d`.

#### `task_dependencies` — Dependency Graph

```sql
CREATE TABLE task_dependencies (
    task_id           TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    depends_on_task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, depends_on_task_id)
);
```

**Index:** `idx_task_deps_reverse` on `(depends_on_task_id)`

Join table replaces comma-separated `depends_on` TEXT. FK-enforced. Cycle
detection runs at dispatch time via recursive DFS.

#### `schema_migrations` — Migration Tracking

```sql
CREATE TABLE schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### 4.2 Migrations

| Version | Description | Type |
|---------|-------------|------|
| 1 | task_seq PK fix (single-row constraint) | Pre-schema (sqlite.go) |
| 2 | Add `tasks.project` column | Column addition |
| 5 | Add `tasks.claimed_by` column | Column addition |
| 6 | Rebuild claim indexes with `lease_until` | Index rebuild |
| 8 | Add `tasks.description` column | Column addition |

### 4.3 SQLite Configuration

| Pragma | Value | Purpose |
|--------|-------|---------|
| `journal_mode` | WAL | Concurrent reads + writes |
| `wal_autocheckpoint` | 1000 | Prevent unbounded WAL growth |
| `busy_timeout` | 5000 (writer) / 10000 (reader) | Wait instead of fail on contention |
| `foreign_keys` | ON | Referential integrity |
| `query_only` | ON (reader connection) | Reject writes on read-replica |

### 4.4 Connection Architecture

- **Writer:** 1 connection (`SetMaxOpenConns(1)`) — serializes all writes
- **Reader:** 3 connections (`SetMaxOpenConns(3)`) — concurrent reads via
  `PRAGMA query_only = 1`
- WAL checkpoint on close: `PRAGMA wal_checkpoint(TRUNCATE)`

---

## 5. Roles

Three built-in roles. Each role has a set of protocol skills (embedded in the
binary, written to disk by `taskbind init`).

### 5.1 Manager

**Purpose:** Plan work, dispatch tasks, review progress, unblock workers.

**Commands:**
- `task dispatch` — Create tasks
- `task search` — Filter the board
- `task view` — Inspect task details
- `task approve --all` — Batch approve reviews
- `taskbind plan lint` — Detect cycles, unknown deps, missing roles
- `taskbind status` — Board overview with burndown

**Embedded Skills:**
| Skill | File | Purpose |
|-------|------|---------|
| dispatch-task | `manager/dispatch-task.md` | Create a new task |
| dispatch-plan | `manager/dispatch-plan.md` | LLM-driven plan → task dispatch |
| create-plan | `manager/create-plan.md` | 3-pass extraction from spec/PRD |
| approve-plan | `manager/approve-plan.md` | Read proposal, dispatch checked items |
| review-backlog | `manager/review-backlog.md` | Search and filter tasks |
| view-task | `manager/view-task.md` | Full task detail with notes + history |
| setup-hooks | `manager/setup-hooks.md` | Set up event-driven hook scripts |

### 5.2 Worker

**Purpose:** Claim tasks, do work, report progress, finish work.

**Commands:**
- `task claim-next` — Claim highest-priority TODO
- `task claim <id>` — Claim a specific task by ID
- `task log-progress` — Report progress, renew lease
- `task extend-lease` — Extend lease without state change
- `task complete` — Mark done (or submit for review)
- `task block` — Mark blocked with reason

**Embedded Skills:**
| Skill | File | Purpose |
|-------|------|---------|
| claim-next-task | `worker/claim-next-task.md` | Claim highest-priority task |
| claim-task | `worker/claim-task.md` | Claim by ID or transfer |
| log-progress | `worker/log-progress.md` | Heartbeat + progress note |
| complete-task | `worker/complete-task.md` | Mark done or submit for review |
| block-task | `worker/block-task.md` | Mark blocked with reason |

### 5.3 Reviewer

**Purpose:** Review submitted work, approve or reject.

**Commands:**
- `task approve <id>` — Approve one task
- `task approve --all` — Approve all IN_REVIEW
- `task reject <id>` — Reject with reason

**Embedded Skills:**
| Skill | File | Purpose |
|-------|------|---------|
| approve-task | `reviewer/approve-task.md` | Approve IN_REVIEW → DONE |
| reject-task | `reviewer/reject-task.md` | Reject IN_REVIEW → TODO |
| claim-review | `reviewer/claim-review.md` | Claim reviewer-role tasks |

### 5.4 System Skill

| Skill | File | Purpose |
|-------|------|---------|
| taskbind | `skills/taskbind.md` | Protocol overview (state machine, lease model, roles, events, hooks) |

---

## 6. Commands — Complete Reference

### 6.1 Task Commands

#### `taskbind task dispatch`

Create a new task as TODO.

```
--title       string   Task title (required, max 500 chars)
--role        string   Role boundary (required, e.g. worker, reviewer)
--description string   Task description (max 2000 chars)
--project     string   Project/scope label (default: "default")
--priority    int      Priority 0-999, lower = more urgent (default: 100)
--depends-on  string   Comma-separated dependency task IDs
```

**Validates:** title length, description length, priority range, role non-empty,
dependency cycle detection via DFS.

**Output:** Full task JSON with `status: "TODO"`.

**Events fired:** `task.created`

---

#### `taskbind task claim-next`

Claim the highest-priority unclaimed task for a role. Also reclaims tasks
with expired leases.

```
--agent        string   Agent name (required)
--role         string   Role (required)
--project      string   Filter by project/scope
--count        int      Number of tasks to claim (default: 1, returns JSON array when >1)
--respect-deps bool     Skip tasks with unmet dependencies (default: true)
```

**Selection order:** Priority ASC (most urgent first), then created_at ASC
(oldest first).

**Atomicity:** Uses Serializable isolation. Two concurrent callers never
get the same task.

**Output:** Single task JSON (count=1) or JSON array (count>1). Empty `{}`
when no work available.

**Events fired:** `task.claimed` (per claimed task)

---

#### `taskbind task claim <id>`

Claim a specific task by ID.

```
--agent    string   Agent name (required)
--transfer bool     Transfer claim to another agent
--to       string   Target agent for transfer
```

**Validates:** Task exists, is TODO (or IN_PROGRESS with expired lease),
no unmet dependencies, not already claimed by same agent.

**Transfer mode:** `--agent` must be current `assigned_agent`. Task must be
IN_PROGRESS. Cannot transfer to self. Receiver gets fresh 15-min lease.

**Events fired:** `task.claimed` (direct claim), `task.transferred` (transfer)

---

#### `taskbind task complete <id>`

Mark a task as done or submit for review.

```
--agent   string   Agent name (required)
--review  bool     Submit for review instead of completing directly
```

**Validates:** Task exists, assigned to this agent, status is IN_PROGRESS
or IN_REVIEW.

**Without `--review`:** Status → DONE, assigned_agent → NULL, lease → NULL.
**With `--review`:** Status → IN_REVIEW, assigned_agent → NULL, lease → NULL.

**Events fired:** `task.completed` or `task.submitted_for_review`

---

#### `taskbind task log-progress <id>`

Report progress and renew lease (heartbeat).

```
--agent  string   Agent name (required)
--note   string   Progress note (required, max 10000 chars)
--type   string   Note type: PROGRESS, ERROR, DECISION
```

**Validates:** Task exists, assigned to this agent.

**Lease renewal:** Sets `lease_until = now + 15 minutes`.

**Events fired:** `task.progress`

---

#### `taskbind task extend-lease <id>`

Extend the lease without a state change (pure heartbeat).

```
--agent   string   Agent name (required)
--minutes int      Lease duration in minutes (default: 15)
```

**Validates:** Task exists, assigned to this agent.

---

#### `taskbind task block <id>`

Mark a task as blocked with a reason.

```
--agent   string   Agent name (required)
--reason  string   Block reason (required, max 1000 chars)
```

**Validates:** Task exists, assigned to this agent, status is IN_PROGRESS
or IN_REVIEW.

**Effect:** Status → BLOCKED, assigned_agent → NULL, lease → NULL.
Block reason written as a note with `note_type = 'BLOCKED'`.

**Events fired:** `task.blocked`

---

#### `taskbind task view <id>`

View full task details with notes and history.

```
--notes    int   Max notes to return (0 = all)
--history  int   Max history entries to return (0 = all)
```

**Output:** `{task: {...}, notes: [...], history: [...]}`

---

#### `taskbind task search`

Search tasks by structured filters or keyword.

```
--status   string   Filter by status (TODO, IN_PROGRESS, BLOCKED, IN_REVIEW, DONE)
--role     string   Filter by role boundary
--agent    string   Filter by assigned agent
--project  string   Filter by project/scope
--query    string   Keyword search (LIKE on title + description)
--limit    int      Max results (0 = unlimited)
```

**Note:** Keyword search uses LIKE, not FTS5. modernc.org/sqlite does not
support FTS5.

---

#### `taskbind task approve <id>`

Approve a task in review, marking it DONE.

```
--agent   string   Agent name (required)
--all     bool     Approve all IN_REVIEW tasks
--project string   Limit --all to a specific project/scope
```

**Self-review gate:** Blocked by default. An agent cannot approve its own
work (checked via `claimed_by` column). Bypass with
`TASKBIND_ALLOW_SELF_REVIEW=true`.

**Events fired:** `review.approved`

---

#### `taskbind task reject <id>`

Reject a task in review, sending it back to TODO.

```
--agent   string   Agent name (required)
--reason  string   Rejection reason (required, max 1000 chars)
```

**Self-review gate:** Same as approve.

**Effect:** Status → TODO, assigned_agent → NULL, lease → NULL.
Rejection reason written as a note with `note_type = 'REJECTED'`.

**Events fired:** `review.rejected`

---

#### `taskbind task set-priority`

Update a task's priority.

```
--id       string   Task ID (required)
--priority int      New priority 0-999 (default: 100)
```

---

### 6.2 Batch Commands

#### `taskbind batch claim`

Claim multiple tasks atomically in one Serializable transaction.

```
--agent        string   Agent name (required)
--role         string   Role (required)
--project      string   Filter by project/scope
--count        int      Number of tasks to claim (default: 1)
--respect-deps bool     Skip tasks with unmet dependencies (default: true)
```

**Implementation:** Single Serializable transaction. SELECT candidates
(capped at `count * 5`, max 100), filter deps, UPDATE each, commit all.

---

#### `taskbind batch complete`

Complete multiple tasks in one transaction.

```
--ids      string   Comma-separated task IDs (required)
--agent    string   Agent name (required)
--to-review bool    Submit for review instead of completing
```

**Partial failure:** Returns completed tasks + per-ID errors. Non-atomic by
default — some may succeed, some may fail.

---

### 6.3 Plan Commands

#### `taskbind plan lint`

Check the board for structural problems.

```
--json     bool     Output as JSON
--project  string   Limit to a specific project/scope
```

**Checks:**
- Circular dependencies (iterative DFS cycle detection)
- Unknown dependency targets (FK-enforced at DB level)
- Missing `role_boundary` (warns)

**Output:** Errors first (sorted by task ID), then warnings. Exit code 1
if errors found, 0 if only warnings.

---

### 6.4 Event Commands

#### `taskbind events list`

List all events from the CDC log.

```
--limit   int   Max events to return (0 = all)
```

**Output:** JSON array of events with `id`, `created_at`, `event_type`,
`payload`.

---

#### `taskbind events tail`

Stream events as they arrive (NDJSON).

```
--from-start   bool         Replay all past events before following
--interval     duration     Poll interval (default: 500ms)
```

**Implementation:** Polls `events` table via `SELECT ... WHERE id > :cursor`.
Handles SIGTERM/SIGINT for clean shutdown.

---

### 6.5 Init Commands

#### `taskbind init`

Scaffold a project for agent coordination.

```
--harness   string   Agent harness: pi, claude, generic (prompts if empty)
--plan      string   Path to plan.md or plan.json for initial task dispatch
--dir       string   Project root directory (default: ".")
```

**Creates:**
- `.taskbind/taskbind.db` — SQLite database
- `.taskbind/hooks/` — Hook directory
- Agent definition files (harness-specific)
- Flat skill files in skills directory
- Skill INDEX file

**Plan parsing:**
- Markdown: `##` headings → task titles, `- ` list items → notes,
  `[pN]` → priority
- JSON: `[{title, role, priority}]`
- Max file size: 1MB

---

#### `taskbind re-init`

Re-scaffold agent files without touching the database.

```
--harness   string   Agent harness (prompts if empty)
--plan      string   Path to plan file for task dispatch
--dir       string   Project root directory (default: ".")
```

Useful after upgrading taskbind to pick up new or updated skills.

---

### 6.6 Prune Command

#### `taskbind prune`

Delete old events, history, and notes.

```
--before   string   Delete records older than this (7d, 30d, or YYYY-MM-DD, default: 30d)
--dry-run  bool     Show what would be deleted without deleting
```

**Tables affected:** `events`, `history`, `notes` (allowlist-guarded against
SQL injection).

**After pruning:** Run `VACUUM` to reclaim disk space.

---

### 6.7 Status Commands

#### `taskbind status`

Show board status and progress (JSON).

```
--project   string   Filter by project
```

**Output:** `{by_status, by_role, expired_leases, total, done_count, percent_done}`

---

#### `taskbind task stats`

Show aggregate task statistics.

```
--project   string   Filter by project
```

**Output:** `{by_status, by_role, expired_leases, total_tasks}`

---

### 6.8 Skill Commands

#### `taskbind skill list`

List all available embedded skills.

**Output:** Tab-separated: `role name file`

---

#### `taskbind skill view <name>`

View a skill definition by name.

**Output:** `role: <role>\n---\n<content>`

---

### 6.9 Version Command

#### `taskbind version`

Print version string.

---

## 7. Events & Hooks

### 7.1 Event Types

| Event | Trigger | Payload Fields |
|-------|---------|----------------|
| `task.created` | dispatch | task_id, title, project, priority, role_boundary |
| `task.claimed` | claim | task_id, agent, title, project, priority, role_boundary |
| `task.progress` | log-progress | task_id, agent, note_type |
| `task.completed` | complete | task_id, agent, title |
| `task.submitted_for_review` | complete --review | task_id, agent, title |
| `task.blocked` | block | task_id, agent, reason |
| `task.transferred` | transfer-claim | task_id, agent, from_agent |
| `review.approved` | approve | task_id, agent, title |
| `review.rejected` | reject | task_id, agent, reason |

### 7.2 Events Table

```sql
CREATE TABLE events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    event_type  TEXT NOT NULL,
    payload     TEXT NOT NULL
);
```

Append-only CDC log. Poll via `SELECT ... WHERE id > :cursor ORDER BY id ASC`.
Clean manually via `taskbind prune --before <date>`.

### 7.3 Hook System

Executable scripts that fire on state transitions.

**Directory layout:**
```
.taskbind/hooks/
├── task-created          ← synchronous, runs before command returns
├── task-completed        ← synchronous
└── task-completed.d/     ← concurrent goroutines (async, 30s timeout)
    ├── slack
    ├── metrics
    └── dashboard
```

**Rules:**
- Hook must be executable (`chmod +x`)
- Receives JSON on stdin: `{"event": "task.completed", "payload": {...}}`
- Non-zero exit is logged to stderr but does not fail the operation
- Missing hook or missing `.d/` directory is silently ignored
- `.d/` entries run concurrently after the single-file hook
- Each hook has a 30-second context timeout
- Hook stdout goes to `os.Stderr` (not `os.Stdout`) to prevent JSON corruption
- The runner waits up to 35s for `.d/` hooks before process exit

**HookRunner lifecycle:**
```go
runner := task.NewHookRunner()
// ... use service ...
runner.Wait(35 * time.Second) // drain .d/ hooks before exit
```

---

## 8. Dependency Graph

### 8.1 Storage

Dependencies are stored in a join table:

```sql
CREATE TABLE task_dependencies (
    task_id           TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    depends_on_task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, depends_on_task_id)
);
```

FK-enforced — cannot reference non-existent tasks.

### 8.2 Claim-Time Filtering

`claim-next` and `claim-by-id` check for unmet dependencies before claiming.
A task with unmet dependencies is skipped (claim-next) or rejected
(claim-by-id).

```go
func hasUnmetDeps(tx *sql.Tx, taskID string) (bool, error) {
    var count int
    err := tx.QueryRow(
        `SELECT COUNT(*) FROM task_dependencies d
          JOIN tasks t ON t.id = d.depends_on_task_id
         WHERE d.task_id = ? AND t.status != 'DONE'`,
        taskID,
    ).Scan(&count)
    return count > 0, nil
}
```

### 8.3 Cycle Detection

Runs at dispatch time via iterative DFS. Detects cycles in the full
dependency graph before inserting a new edge.

```go
// detectCycles finds all cycles using iterative DFS with 3-color marking
// (unvisited, inStack, done). Returns cycle paths with start node repeated
// at end.
```

### 8.4 Plan Lint

`taskbind plan lint` checks for:
- Circular dependencies (error)
- Unknown dependency targets (FK-enforced, caught at dispatch)
- Missing `role_boundary` (warning)

---

## 9. Plan Parsing

### 9.1 Markdown Format

```markdown
## Set up auth [p1]
- Implement login endpoint
- Add JWT middleware

## Add CI pipeline

## Review everything [p1]
```

- `##` headings → task titles
- `[pN]` → priority (1-999, default 100)
- `- ` list items → notes (stored but not dispatched as tasks)
- Role defaults to "worker"

### 9.2 JSON Format

```json
[
  {"title": "Fix auth bug", "role": "worker", "priority": 1},
  {"title": "Review PR", "role": "reviewer", "priority": 5}
]
```

### 9.3 LLM-Driven Plan (Product Owner Workflow)

For complex plans (specs, PRDs, documents with sections and tables), the
Go parser is insufficient. The LLM-driven workflow uses three skills:

1. **create-plan** — 3-pass extraction:
   - Pass 1: Surface scan (every explicitly named deliverable)
   - Pass 2: Implied work (loading states, error states, validation, auth)
   - Pass 3: Integration + sequencing (dependency graph, phases, gaps)
   - Writes `.taskbind/tasks-proposal.md` (does NOT dispatch)

2. **approve-plan** — Reads proposal, finds `[x]` checked items, dispatches
   each one. Supports `--all` flag to dispatch everything.

3. **dispatch-plan** — Auto-execute loop: dispatch → claim → subagent →
   work → complete → review → repeat until board empty.

---

## 10. Harness Integration

### 10.1 Pi Harness

Scaffolds `.pi/agents/` with agent definition files and `.pi/skills/` with
flat skill files. Each agent (manager, worker, reviewer) gets a markdown
definition that teaches the agent its role and available commands.

### 10.2 Claude Code Harness

Scaffolds `.claude/agents/` with agent definition files and `.claude/skills/`
with flat skill files.

### 10.3 Generic Harness

Scaffolds `.agents/` directory with agent definitions and `.agents/skills/`
with flat skill files. No framework-specific configuration.

### 10.4 Skill INDEX File

A flat `INDEX` file maps skills to roles:

```
system:taskbind.md
manager:dispatch-task.md
manager:create-plan.md
manager:approve-plan.md
manager:review-backlog.md
manager:setup-hooks.md
manager:view-task.md
worker:claim-next-task.md
worker:claim-task.md
worker:log-progress.md
worker:block-task.md
worker:complete-task.md
reviewer:claim-review.md
reviewer:approve-task.md
reviewer:reject-task.md
```

---

## 11. Claim Transfer (Hierarchical Delegation)

For long-running subagent work (>15 minutes), transfer the claim so the
subagent owns it and can complete independently.

### When to Use

- Subagent work may exceed 15 minutes
- Crash recovery is needed: if subagent crashes, lease expiry reclaims
  the task rather than orphaning it

### How It Works

```
orchestrator claims TASK-5
  → taskbind task claim TASK-5 --agent orchestrator --transfer --to pi-worker
  → pi-worker now owns the claim (fresh 15-min lease)
  → pi-worker can complete, extend-lease, log-progress directly
  → if pi-worker crashes, lease expires, task is reclaimable
```

### Fast Subagent Pattern (No Transfer)

For quick work (<15 min), the orchestrator keeps the claim:

```
orchestrator claims → spawns subagent → collects result → complete-task
```

### Transfer Rules

- `--agent` must be the current `assigned_agent` (the claim holder)
- `--to` is the new owner — gets a fresh 15-minute lease
- Task must be `IN_PROGRESS` (cannot transfer unclaimed or done tasks)
- Cannot transfer to self
- History records the transfer; event `task.transferred` fires
- No agent existence check — by design (ghost agents are valid)

---

## 12. Review Gate

### 12.1 Self-Review Prevention

An agent cannot approve or reject its own work. The check uses the
`claimed_by` column, which is an immutable snapshot of the first agent to
claim the task (preserved across lease reclamations).

```go
func checkSelfReview(tx *sql.Tx, id, agent string) error {
    var claimedBy sql.NullString
    err := tx.QueryRow(
        `SELECT claimed_by FROM tasks WHERE id = ?`, id,
    ).Scan(&claimedBy)
    if claimedBy.Valid && claimedBy.String == agent {
        return ErrSelfReview
    }
    return nil
}
```

**Bypass:** Set `TASKBIND_ALLOW_SELF_REVIEW=true` environment variable.
Use when the orchestrator acts as both worker and reviewer.

### 12.2 Review Workflow

1. Worker completes with `--review` → status becomes `IN_REVIEW`
2. Reviewer calls `approve` or `reject` directly (no claim needed)
3. Approve → `DONE`. Reject → `TODO` (back to backlog with reason)
4. `approve --all` batch-approves all `IN_REVIEW` tasks (optionally
   filtered by project)

---

## 13. Batch Operations

### 13.1 Batch Claim

```go
func (s *Service) ClaimBatch(ctx context.Context, agent, role, project string,
    count int, respectDeps ...bool) ([]Task, error)
```

- Serializable transaction
- SELECT candidates (capped at `count * 5`, max 100)
- Filter by dependency status
- UPDATE each claimed task
- Commit all or rollback all

### 13.2 Batch Complete

```go
func (s *Service) BatchComplete(ctx context.Context, ids []string, agent string,
    toReview bool) ([]Task, []error)
```

- Single transaction (not Serializable by default)
- Processes all IDs
- Returns completed tasks + per-ID errors for partial failures
- Already-done tasks are silently included (no error)
- Hooks fire per completed task after commit

---

## 14. Pruning & Retention

### 14.1 Manual Prune

```go
func (s *Service) Prune(ctx context.Context, before time.Time, dryRun bool) (PruneResult, error)
```

Deletes from `events`, `history`, and `notes` tables where `created_at < before`.
Allowlist-guarded against SQL injection.

### 14.2 No Auto-Prune

There is no background reaper or TTL-based auto-prune. Events, history, and
notes grow unbounded until manually pruned. The `taskbind.md` skill and
README previously claimed a 3-day TTL — this does not exist.

---

## 15. Observability

### 15.1 Debug Mode

`taskbind --debug` enables `slog.Info` logging for:
- Database open/close
- WAL mode enable
- Schema application
- Migration application
- task_seq seeding

### 15.2 Normal Operation

Zero logging without `--debug`. Key transitions (claim, complete, block,
hook fire) are not logged by default.

### 15.3 JSON Output

Every command prints stable JSON to stdout. Empty work returns `{}`.
Errors go to stderr as `{"error":"..."}` with exit code 2.

---

## 16. Error Handling

### 16.1 Error Types

```go
type ExitError struct {
    Code    int
    Message string
}

var ErrNotFound      = &ExitError{Code: 2, Message: "task not found"}
var ErrInvalidState  = &ExitError{Code: 2, Message: "invalid state transition"}
var ErrNotAssigned   = &ExitError{Code: 2, Message: "task not assigned to this agent"}
var ErrSelfReview    = &ExitError{Code: 2, Message: "cannot review your own task"}
```

### 16.2 Retry on Busy

```go
func (s *Service) retryOnBusy(fn func() error) error
```

- 3 attempts
- Exponential backoff: 100ms, 200ms, 400ms base
- 30% jitter per attempt
- Retries on `SQLITE_BUSY` (5) and `SQLITE_LOCKED` (6) at any extended level
- All write paths use `retryOnBusy` except `Dispatch` (inconsistency)

### 16.3 Input Validation

| Field | Max Length | Location |
|-------|-----------|----------|
| title | 500 chars | `internal/task/helpers.go` |
| description | 2000 chars | `internal/task/helpers.go` |
| note | 10000 chars | `internal/task/helpers.go` |
| reason | 1000 chars | `internal/task/helpers.go` |
| priority | 0-999 | `internal/task/helpers.go` |

---

## 17. Configuration

### 17.1 Environment Variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `TASKBIND_DB` | Override database path | `.taskbind/taskbind.db` |
| `TASKBIND_ALLOW_SELF_REVIEW` | Bypass self-review gate | (not set) |

### 17.2 Config Resolution

1. `--db` flag
2. `TASKBIND_DB` environment variable
3. Walk up from CWD looking for `.taskbind/` directory
4. Default: `.taskbind/taskbind.db`

### 17.3 Project Root Discovery

```go
func findProjectRoot() string {
    // Walks up from CWD looking for .taskbind/ directory
    // Returns path to taskbind.db inside first .taskbind/ found
}
```

---

## 18. Testing

### 18.1 Test Files

| File | Tests | Lines |
|------|-------|-------|
| `internal/task/service_test.go` | Dispatch, Complete, LogProgress, Block, ExtendLease, SetPriority, Search, View | ~370 |
| `internal/task/claim_test.go` | ClaimNext, ClaimByID, ClaimBatch, atomicity, role filtering, lease expiry, deps | ~400 |
| `internal/task/batch_test.go` | BatchComplete partial failure, BatchClaim priority order, concurrent no-double-claim, deps in concurrent, batch to-review | ~200 |
| `internal/task/review_test.go` | Approve, Reject, SelfReview, SelfReview with env, direct IN_REVIEW, ReviewerCannotClaim, ApproveAll, ApproveAll filter, ApproveAll empty, ApproveAll self-review | ~300 |
| `internal/task/lint_test.go` | Clean board, unknown dep, cycle detection, missing role, errors before warns, burndown | ~150 |
| `internal/task/hook_test.go` | Fires after dispatch, no fire on rollback, empty claim, missing silent, non-zero exit, .d/ all fire, lex order, non-executable, single+.d/, failing doesn't block siblings | ~250 |
| `internal/task/transfer_test.go` | Basic transfer, to-self error, wrong status, wrong owner, chain A→B→C, ghost agent, resets lease, cycle detection | ~200 |
| `internal/task/stats_test.go` | ListEvents, review path events, block+reject events, MaxEventID, PollEvents | ~200 |
| `internal/task/e2e_test.go` | Full workflow: dispatch → claim → progress → complete → review → approve, dependency chain | ~250 |
| `internal/storage/sqlite_test.go` | Open creates DB, creates parent dir, pragmas, schema, idempotent, single connection, close checkpoints, invalid path, debug no panic, project migration, task_seq migration old format, task_seq empty DB | ~300 |
| `internal/bootstrap/bootstrap_test.go` | ParsePlan markdown, ParsePlan JSON, empty file, Init creates DB+skills, Init with plan, ReInit scaffolds | ~200 |

### 18.2 Test Patterns

- **Concurrent tests:** `sync.WaitGroup` + goroutines for race condition
  verification (5 agents batch-claiming, no double-claims)
- **Hook tests:** Sentinel files + polling for async hook completion
- **Migration tests:** Create old-format DB, close, re-open, verify migration
- **E2E test:** Full lifecycle with dependency chain

### 18.3 Test Infrastructure

```go
func newTestDB(t *testing.T) *storage.DB     // Temp DB, auto-cleanup
func newTestService(t *testing.T) *Service    // Service with temp DB
func newTestServiceWithHooks(t, hooksDir)     // Service with hook directory
func makeHook(t, hooksDir, eventType, script) // Create executable hook
func makeHookD(t, hooksDir, eventType, name, script) // Create .d/ hook
func waitForSentinel(t, path)                 // Poll for file up to 2s
```

---

## 19. Project Structure

```
cmd/taskbind/
├── main.go              CLI entrypoint, root command, version
├── config.go            Config resolution (flag, env, project root walk)
├── service.go           Service factory (open DB, create service, hook runner)
├── output.go            JSON stdout / stderr helpers
├── task.go              Task command tree
├── dispatch.go          task dispatch
├── claim.go             task claim (by ID, transfer)
├── view.go              task view, search, stats, status
├── update.go            task complete, log-progress, extend-lease, block
├── review.go            task approve, reject
├── set_priority.go      task set-priority
├── batch.go             batch claim, batch complete
├── events.go            events list, events tail
├── prune.go             prune
├── plan.go              plan lint
├── init.go              init, re-init
├── skill_cmd.go         skill list, skill view

internal/
├── task/
│   ├── model.go         Task, Note, HistoryEntry, TaskDetail, Event types
│   ├── helpers.go       Service struct, ExitError, retryOnBusy, nextID, scanTask, constants
│   ├── service.go        Dispatch, Complete, LogProgress, ExtendLease, Block, ApproveAll, SetPriority, SearchFullText
│   ├── claim.go          ClaimNext, ClaimBatch, ClaimByID, TransferClaim, hasUnmetDeps
│   ├── queries.go        View, ViewDetail, listNotes, listHistory, Search, Stats, Burndown, MaxEventID, PollEvents, ListEvents
│   ├── review.go         ReviewApprove, ReviewReject, checkSelfReview
│   ├── lint.go           LintPlan, detectCycles
│   ├── events.go         EventPayload, eventPayload, insertEvent
│   ├── hooks.go          HookRunner, runHook, execHook
│   ├── prune.go          Prune, pruneTable
│   ├── *_test.go         12 test files
├── storage/
│   ├── schema.sql        All CREATE TABLE statements
│   ├── sqlite.go         Open, Close, DB wrapper, pragmas, schema apply, task_seq seed
│   ├── migrations.go     runMigrations, addColumnIfMissing
│   ├── sqlite_test.go    Storage tests
├── bootstrap/
│   ├── bootstrap.go      Init, ReInit, scaffoldHarness, dispatchPlan
│   ├── embed.go          Embedded agent/skill file loading, ListSkills, ReadSkill
│   ├── plan.go           ParsePlan (markdown + JSON)
│   ├── bootstrap_test.go Bootstrap tests
│   ├── embed/
│       ├── agents/       Agent definitions (pi, claude, generic)
│       ├── skills/       Protocol skills (manager, worker, reviewer, taskbind.md)

docs/                     Astro documentation site
examples/                 Integration guides (pi-subagents, claude-code-subagents)
```

---

## 20. Key Design Decisions

### 20.1 Why SQLite

- No server, no daemon, no queues — one file
- SQLite serializes writes — perfect for claim atomicity
- WAL mode allows concurrent reads
- Pure Go (modernc.org/sqlite) — no CGo, no platform issues
- Portable — copy the `.db` file for backup

### 20.2 Why No Server

- Every agent runs the binary directly
- The `.db` file is the only coordination point
- No deployment, no scaling, no ops
- Works on shared filesystems (NFS, SSHFS)

### 20.3 Why JSON Output

- Agents parse JSON, not tables
- Every command prints stable JSON to stdout
- Empty work returns `{}` (not an error)
- Errors go to stderr as `{"error":"..."}`

### 20.4 Why Embedded Skills

- Skills teach agents the protocol
- Different agents (Claude Code, PI, Codex, Gemini) read the same files
- The agents change. The protocol stays.
- `taskbind init` writes them to disk; `taskbind re-init` refreshes them

### 20.5 Why No Background Reaper

- Leases are reclaimed lazily on `claim-next`
- A reaper adds complexity (daemon, scheduling, edge cases)
- For alpha, lazy reclamation is sufficient
- Future: startup sweep in `Open()`

---

## 21. Known Issues & Limitations

### 21.1 Critical

| Issue | Impact | Fix |
|-------|--------|-----|
| No CI pipeline | Tests and lint never run automatically | Add `.github/workflows/ci.yml` |
| Docs-code divergence | 9 false claims in README/AUDIT/skills | Delete false claims, reconcile |
| Hook stdout → os.Stdout | First hook that prints corrupts JSON output | Change to `os.Stderr` |
| task_seq re-seeds downward on delete | ID reuse after task deletion | `MAX(next_id, ...)` instead of bare `MAX(...)` |
| No lease reaper | Orphaned IN_PROGRESS tasks stay orphaned until another agent claims | Startup sweep in `Open()` |

### 21.2 High

| Issue | Impact | Fix |
|-------|--------|-----|
| 6 duplicated SELECT column strings | Schema change requires editing 6 locations | `const taskColumns` in `task.go` |
| Claim query OR defeats index | Full scan on claim-next | `UNION ALL` of two queries |
| Events table unbounded | Disk growth until manual prune | Add auto-prune or document manual-only |
| No lint CI | `.golangci.yml` never executed | Add to CI workflow |

### 21.3 Medium

| Issue | Impact | Fix |
|-------|--------|-----|
| Dispatch skips retryOnBusy | Can fail on busy DB | Wrap in retryOnBusy |
| Self-review gate bypassable 3 ways | Env var, NULL claimed_by, no schema constraint | Remove env bypass, add CHECK |
| No SIGTERM handling outside events tail | Hook goroutines orphaned on SIGTERM | signal.NotifyContext in main.go |
| Zero observability without --debug | Normal operation is silent | slog.Info on key transitions |
| No doctor/health command | Can't diagnose DB state | Add `taskbind doctor` |

### 21.4 Low

| Issue | Impact | Fix |
|-------|--------|-----|
| pnpm-lock.yaml at Go repo root | Belongs to docs/ Astro site | Move docs to separate repo |
| Build artifacts + AI exhaust committed | test-results/, feedbacks/, .pi/extensions/ | git rm + .gitignore |

---

## 22. Dependencies

### 22.1 Direct

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `modernc.org/sqlite` | Pure Go SQLite (no CGo) |

### 22.2 Indirect

| Package | Purpose |
|---------|---------|
| `github.com/dustin/go-humanize` | Byte formatting |
| `github.com/google/uuid` | UUID generation |
| `github.com/inconshreveable/mousetrap` | Windows console detection |
| `github.com/mattn/go-isatty` | TTY detection |
| `github.com/ncruces/go-strftime` | strftime for SQLite |
| `github.com/remyoudompheng/bigfft` | Math for SQLite |
| `github.com/spf13/pflag` | Flag parsing |
| `golang.org/x/sys` | System calls |
| `modernc.org/libc` | C library emulation |
| `modernc.org/mathutil` | Math utilities |
| `modernc.org/memory` | Memory management |

---

## 23. Build & Release

### 23.1 Build

```bash
go build -o taskbind ./cmd/taskbind/
```

Requires Go 1.24+. Pure Go, no CGo.

### 23.2 Release

`.goreleaser.yml` configured for multi-platform builds. Install via:

```bash
curl -sfL https://raw.githubusercontent.com/mrSamDev/taskbind/main/install.sh | sh
```

### 23.3 Linting

`.golangci.yml` enables `errcheck` and `staticcheck`. Excludes:
- `errcheck` in test files
- `rows.Close` errors
- `MarkFlagRequired` errors
