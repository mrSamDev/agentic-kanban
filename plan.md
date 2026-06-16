# Technical Debt Sprint — 5 Production Blockers

Priority order: hook lifecycle → migration infra → dependency join table → ApproveAll resilience → documentation.

Items 2 and 3 are schema-breaking — must land before v1.

---

## Item 1: `.d/` Hook Goroutine Race

**File:** `internal/task/hooks.go`

**Problem:** `runHook()` spawns `go execHook(...)` for each `.d/` entry with no `sync.WaitGroup`. When the main process exits (after `os.Exit` or `return`), goroutines are killed mid-flight. Production hooks (Slack, webhooks) silently drop.

**Fix:** Introduce a `HookRunner` struct with a `sync.WaitGroup`.

```go
type HookRunner struct {
    wg sync.WaitGroup
}

func NewHookRunner() *HookRunner {
    return &HookRunner{}
}

// runHook now takes a *HookRunner. Single-file hooks run synchronously (existing behavior).
// .d/ hooks register with wg.Add(1) and run in goroutines.
func runHook(runner *HookRunner, hooksDir, eventType string, payload any) {
    // ... existing logic ...
    // For .d/ entries:
    runner.wg.Add(1)
    go func() {
        defer runner.wg.Done()
        execHook(...)
    }()
}

// Wait blocks until all .d/ hooks finish, with a timeout.
// Called by CLI commands before os.Exit.
// Timeout must exceed execHook's 30s context timeout (35s = 30s + 5s margin).
func (r *HookRunner) Wait(timeout time.Duration) {
    done := make(chan struct{})
    go func() {
        r.wg.Wait()
        close(done)
    }()
    select {
    case <-done:
    case <-time.After(timeout):
        fmt.Fprintf(os.Stderr, "hook runner timeout after %v\n", timeout)
    }
}

// Usage: svc.HookRunner.Wait(35 * time.Second)
```

**Changes:**
- `internal/task/hooks.go` — add `HookRunner` struct, refactor `runHook` signature
- `internal/task/service.go` — embed `*HookRunner` in `Service`, pass to `runHook` calls
- `cmd/kanban/main.go` — add `defer svc.HookRunner.Wait(35*time.Second)` after command dispatch (single canonical location)
- Constructor `NewService` — accept `*HookRunner`

---

## Item 2: Ad-Hoc `pragma_table_info` → Versioned Migration Table

**File:** `internal/storage/sqlite.go` (lines ~77-145)

**Problem:** 6 inline `SELECT COUNT(*) FROM pragma_table_info(...)` checks + `ALTER TABLE` / `DROP INDEX` calls hard-coded in `Open()`. Adding migration #7 means another ad-hoc block. Unmaintainable, race-prone on first startup, and the ordering is implicit (line order).

**Fix:** Introduce `schema_migrations` table and a numbered migration runner.

**Schema addition** (`schema.sql`):
```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Migration runner** (new file `internal/storage/migrations.go`):
```go
// migration is a numbered step. Can be SQL string or Go func for complex migrations.
type migration struct {
    version int
    upSQL   string           // for pure SQL migrations
    upFunc  func(*sql.Tx) error // for Go-based migrations (e.g., data transforms)
}

var migrations = []migration{
    {1, `DROP TABLE IF EXISTS task_seq;`, nil},
    // ^ old task_seq had no PK, allows duplicates. v1 task_seq in schema.sql replaces it.
    {2, `ALTER TABLE tasks ADD COLUMN project TEXT NOT NULL DEFAULT 'default';`, nil},
    {3, `ALTER TABLE events ADD COLUMN ttl_seconds INTEGER DEFAULT 259200;
         CREATE INDEX IF NOT EXISTS idx_events_ttl ON events(created_at, ttl_seconds);`, nil},
    {4, `ALTER TABLE tasks ADD COLUMN depends_on TEXT;`, nil},
    {5, `ALTER TABLE tasks ADD COLUMN claimed_by TEXT;`, nil},
    {6, `DROP INDEX IF EXISTS idx_tasks_claim;
         CREATE INDEX IF NOT EXISTS idx_tasks_claim
           ON tasks(role_boundary, status, priority, created_at, lease_until);
         CREATE INDEX IF NOT EXISTS idx_tasks_claim_project
           ON tasks(role_boundary, project, status, priority, created_at, lease_until);`, nil},
    {7, "", migrateDependsOnToJoinTable}, // Go func: split TEXT, insert, drop column
}

func migrateDependsOnToJoinTable(tx *sql.Tx) error {
    // 1. Read all tasks with depends_on TEXT
    rows, err := tx.Query(`SELECT id, depends_on FROM tasks WHERE depends_on IS NOT NULL`)
    // 2. For each task, split on comma, insert into task_dependencies
    // 3. ALTER TABLE tasks DROP COLUMN depends_on (SQLite 3.35+)
    // 4. Return error if any step fails (tx rolls back)
}
```
```

**In `Open()`**, after schema apply:
```go
current := getCurrentVersion(db) // SELECT MAX(version) FROM schema_migrations

// Fresh-DB seed: if schema_migrations is empty but tasks table exists,
// the DB was created from current schema.sql — seed with all migration versions.
if current == 0 && tableExists(db, "tasks") {
    seedMigrations(db, len(migrations)) // INSERT 1..7
}

for _, m := range migrations {
    if m.version <= current {
        continue
    }
    // apply m.up (SQL string or Go func)
    // INSERT INTO schema_migrations(version) VALUES(m.version)
}

// Preserve task_seq seeding: runs unconditionally on every Open() after migrations.
// Re-seeds next_id from MAX(task number) in existing tasks table.
_, _ = db.Exec(`UPDATE task_seq SET next_id = (
    SELECT COALESCE(MAX(CAST(substr(id,6) AS INTEGER)), 0) FROM tasks
) WHERE id = 1`)
```

This replaces all 6 `pragma_table_info` blocks. Each migration runs exactly once, in order, in a single transaction.

**Fresh-DB handling**: When `schema_migrations` is empty but `tasks` table exists, the DB was created from the current embedded schema — seed with versions 1-7 instead of replaying migrations.

**task_seq preservation**: The seeding step runs on every `Open()` after migrations — ensures new tasks get unique IDs even after migration 1 drops/recreates `task_seq`.

**Changes:**
- `internal/storage/schema.sql` — add `schema_migrations` table
- `internal/storage/sqlite.go` — remove ad-hoc migration blocks, call `runMigrations(db)`
- New `internal/storage/migrations.go` — migration definitions + runner

---

## Item 3: Comma-Separated `depends_on` TEXT → Join Table

**Files:** `internal/storage/schema.sql`, `internal/task/model.go`, `internal/task/service.go`, `internal/task/claim.go`

**Problem:** `Task.DependsOn *string` stores comma-separated IDs. `hasUnmetDeps()` does `strings.Split` + iterative DB lookups. Cycle detection in `Dispatch()` does N separate queries per edge. At 500+ tasks with deep chains this is O(n²) string scans and O(depth) round-trips. No FK constraints. Reverse query ("what depends on TASK-8?") requires a LIKE scan.

**Fix:**
1. Create `task_dependencies` join table:
```sql
CREATE TABLE IF NOT EXISTS task_dependencies (
    task_id           TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    depends_on_task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, depends_on_task_id)
);
CREATE INDEX IF NOT EXISTS idx_task_deps_reverse
    ON task_dependencies(depends_on_task_id);
```

2. **Migration** (item 7 in migration table): read existing `depends_on` TEXT, split on comma, insert into `task_dependencies`, then `ALTER TABLE tasks DROP COLUMN depends_on` (SQLite 3.35+).

3. **Replace `Task.DependsOn *string`** with a method or helper. Remove the field from `Task` struct (or keep it as deprecated). Add `LoadDeps(tx, taskID) ([]string, error)` to fetch from join table.

4. **Rewrite `hasUnmetDeps()`**:
```go
func hasUnmetDeps(tx *sql.Tx, taskID string) (bool, error) {
    var count int
    err := tx.QueryRow(
        `SELECT COUNT(*) FROM task_dependencies d
          JOIN tasks t ON t.id = d.depends_on_task_id
         WHERE d.task_id = ? AND t.status != 'DONE'`,
        taskID,
    ).Scan(&count)
    if err != nil {
        return false, fmt.Errorf("check deps for %s: %w", taskID, err)
    }
    return count > 0, nil
}
```

5. **Rewrite cycle detection in `Dispatch()`** as a recursive CTE:
```sql
WITH RECURSIVE dep_chain(id) AS (
    SELECT depends_on_task_id FROM task_dependencies WHERE task_id = ?
    UNION ALL
    SELECT d.depends_on_task_id
      FROM task_dependencies d
      JOIN dep_chain c ON c.id = d.task_id
)
SELECT COUNT(*) FROM dep_chain WHERE id = ?
```
Checks if the new task's dependency chain circles back to itself.

**Changes:**
- `internal/storage/schema.sql` — add `task_dependencies` table
- `internal/task/model.go` — remove `DependsOn *string` from `Task` struct
- `internal/task/service.go` — rewrite `Dispatch()` cycle detection with CTE, change signature to `dependsOn []string`
- `internal/task/claim.go` — rewrite `hasUnmetDeps(tx, taskID)` to use join table; update `ClaimByID` to call `LoadDeps(tx, id)`
- `internal/task/helpers.go` — update `scanTask()` and `reRead()` to skip `depends_on` column
- `cmd/kanban/dispatch.go` — `--depends-on` flag: parse comma-separated input to `[]string`, pass to `Dispatch()`
- `cmd/kanban/task.go` — `view` and `search` output: call `LoadDeps()` or omit `depends_on` display
- `internal/storage/migrations.go` — add migration 7 (Go func: TEXT → join table data migration)

---

## Item 4: `ApproveAll` First-Error Bailout → Per-Task Diagnostics

**File:** `internal/task/service.go` — `ApproveAll()` method (~lines 500+)

**Problem:** Iterates IN_REVIEW tasks in a Serializable tx. First error (`checkSelfReview`, `insertEvent`) triggers `return fmt.Errorf(...)` → full tx rollback. All prior approvals in the loop are lost. Correct ACID but zero diagnostic value — operator can't see which tasks approved and which failed.

**Fix:** Same pattern as `BatchComplete`: return `([]Task, []error)`.

```go
func (s *Service) ApproveAll(ctx context.Context, agent, project string) ([]Task, []error) {
    var approved []Task
    var errs []error
    // ... tx begin ...
    // Collect per-task errors, continue loop on non-fatal errors
    // Only tx.Begin/tx.Commit failures are fatal
    return approved, errs
}
```

**Behavior:**
- Self-review violation on TASK-3 → add error to `errs`, continue to TASK-4
- Event insert failure on TASK-5 → add error to `errs`, continue
- tx.Commit() failure → all went in or none (ACID), but operator sees `errs`

**Changes:**
- `internal/task/service.go` — change return signature, add per-task error collection
- `cmd/kanban/review.go` — update handler to print success count + error list

---

## Item 5: Events/CDC Polling Undocumented in LLM Ref

**File:** `internal/bootstrap/embed/skills/kanban.md`

**Problem:** The `events` table and hook CDC mechanism exist but the skill file does not mention them. Agents reading `kanban.md` cannot discover event-driven integration patterns without reading Go source code.

**Fix:** Add an "Event-Driven Hooks & CDC" section to `kanban.md`:

```markdown
## Event-Driven Hooks & CDC

### Events Table

Every state transition writes to the `events` table — an append-only CDC log:

```sql
CREATE TABLE events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    event_type  TEXT NOT NULL,
    payload     TEXT NOT NULL,
    ttl_seconds INTEGER DEFAULT 259200  -- 3 days; NULL = never expires
);
```

### Polling for Integration

Agents or external systems can poll for new events:

```sql
SELECT id, event_type, payload FROM events
 WHERE id > :last_seen_id
 ORDER BY id ASC
 LIMIT 100;
```

Events auto-expire after `ttl_seconds` (default 3 days). TTL cleanup runs during board operations.

### Event Types

| Event | Trigger | Payload Fields |
|-------|---------|----------------|
| `task.created` | dispatch | task_id, title, project, priority, role_boundary |
| `task.claimed` | claim | task_id, agent, title, project, priority, role_boundary |
| `task.progress` | log-progress | task_id, agent, note_type |
| `task.completed` | complete | task_id, agent, title |
| `task.submitted_for_review` | complete --review | task_id, agent, title |
| `task.blocked` | block | task_id, agent, reason |
| `task.transferred` | transfer-claim | task_id, agent, from_agent |
| `task.priority_updated` | batch set-priority | task_id, title, priority |
| `task.project_updated` | batch set-project | task_id, title, project |
| `review.approved` | approve | task_id, agent, title |
| `review.rejected` | reject | task_id, agent, reason |

### Hook Directory Layout

Events also fire executable hooks on disk:

```
.kanban/hooks/
├── task-created          ← synchronous, ordered
├── task-completed        ← synchronous, ordered
└── task-completed.d/     ← concurrent goroutines (async)
    ├── slack
    ├── metrics
    └── dashboard
```

- Single-file hooks run synchronously and complete before the command returns.
- `.d/` directory hooks run concurrently in goroutines with a 30s timeout.
- Hook receives JSON event on stdin: `{"event": "task.completed", "payload": {...}}`
- Must be executable (`chmod +x`).
- Non-zero exit is logged to stderr but does not fail the operation.
- Missing hook or missing `.d/` directory is silently ignored.
- The runner waits up to 35s for `.d/` hooks before process exit (to prevent mid-flight goroutine killing — exceeds 30s execHook timeout).
```

---

## Summary

| # | Item | Primary Files | Schema Change | Breaking |
|---|------|--------------|---------------|----------|
| 1 | Hook goroutine race | `hooks.go`, `service.go`, `cmd/*.go` | No | No |
| 2 | Migration table | `sqlite.go`, new `migrations.go`, `schema.sql` | Yes (`schema_migrations`) | No |
| 3 | Dependency join table | `schema.sql`, `model.go`, `claim.go`, `service.go` | Yes (`task_dependencies`, drop `depends_on`) | Yes — v1 break |
| 4 | ApproveAll diagnostics | `service.go`, `cmd/kanban/review.go` | No | No |
| 5 | CDC documentation | `kanban.md` | No | No |

**Execution order:** 1 → 2 → 3 → 4 → 5.

Items 1 and 4 have no schema dependencies — can be done first or in parallel.
Item 2 must land before item 3 (migration infra needed for 7th migration).
Item 3 is the highest-value structural change but depends on item 2.
Item 5 is pure documentation — can land any time, listed last for ordering discipline.