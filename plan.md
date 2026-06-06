# Post-Mortem Fixes Plan

Branch: `post-mortem-fixes`

---

## Release Strategy

Ordered by leverage, not implementation convenience.

### Release 1 — Make multi-agent execution safe (v0.4)

Safety before speed. These three changes turn the system from "works in demos" into "safe for real projects."

| Step | What | Why |
|------|------|-----|
| 1 | `depends_on` schema + claim guard | Without this, parallel workers produce garbage |
| 2 | `extend-lease` command | Without this, long tasks get double-claimed |
| 3 | Cross-agent review gate | Without this, a single agent can approve its own work |

### Release 2 — Make multi-agent execution fast (v0.5)

| Step | What | Why |
|------|------|-----|
| 4 | `claim-next --count N` | Atomic batch claim for parallel worker spawning |
| 5 | Optional subagent delegation | Make parallelism easy, not mandatory |
| 6 | Project env auto-detection | Subagents in subdirs auto-find the DB |

### Release 3 — Operational maturity (v0.6)

| Step | What | Why |
|------|------|-----|
| 7 | `kanban status --burndown` | Progress visibility |
| 8 | `kanban plan lint` | Catch bad plans before execution |
| 9 | `approve-plan --all` flag | Minor UX improvement |

---

## Step 1: `depends_on` — Dependency tracking (PM#6)

### Files changed
- `internal/storage/schema.sql` — migration
- `internal/task/model.go` — add `DependsOn` field
- `internal/task/claim.go` — guard in `ClaimNext`, reuse in `ClaimBatch`
- `internal/task/service.go` — `Dispatch` accepts `dependsOn`
- `cmd/kanban/dispatch.go` — `--depends-on` flag

### Schema
Add column via migration in `storage.Open()` (same pattern as `project` and `ttl_seconds`):

```sql
ALTER TABLE tasks ADD COLUMN depends_on TEXT;
```

`depends_on` stores comma-separated task IDs (e.g., `TASK-8,TASK-9`) or NULL.

**Backwards compatibility**: Migration runs at `Open()`, safe for existing DBs. Old rows have `depends_on = NULL` which the guard treats as "no deps" — zero behavior change.

**Rollback**: SQLite doesn't support `DROP COLUMN` before 3.35. If needed: copy table to temp, recreate without column, copy back. Unlikely to be needed — the column is read-only for existing tasks.

### Model
```go
type Task struct {
    // ...existing fields
    DependsOn *string `json:"depends_on"` // comma-separated dependency IDs, nullable
}
```

### Claim guard in `ClaimNext`
**Key change**: `ClaimNext` currently fetches 1 candidate. Must now fetch N candidates and loop until it finds one with no unmet deps (or runs out).

**scanTask update**: `scanTask()` in `helpers.go` currently scans 12 fields. `depends_on` makes 13. Add `depends_on` to `scanTask()` — the migration runs at `Open()`, so by the time any service method runs, the column exists.

```go
func (s *Service) ClaimNext(ctx context.Context, agent, role, project string) (Task, error) {
    // ... existing setup ...

    // Fetch up to 20 candidates (not just 1) to account for dep filtering
    rows, _ := tx.Query(`
        SELECT id, title, status, role_boundary, project, priority,
               assigned_agent, lease_until, created_at, updated_at, depends_on
          FROM tasks
         WHERE role_boundary = ?
           AND project = ?
           AND (status = 'TODO'
                OR (status = 'IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP))
         ORDER BY priority ASC, created_at ASC
         LIMIT 20`, role, project,
    )

    var candidate Task
    for rows.Next() {
        t, _ := scanTask(rows)
        if hasUnmetDeps(tx, t) {
            continue
        }
        candidate = t
        break
    }
    if candidate.ID == "" {
        return Task{}, nil // nothing claimable
    }

    // Claim the found candidate (existing logic)
    // ...
}

func hasUnmetDeps(tx *sql.Tx, t Task) bool {
    if t.DependsOn == nil || *t.DependsOn == "" {
        return false
    }
    ids := strings.Split(*t.DependsOn, ",")
    for i := range ids {
        ids[i] = strings.TrimSpace(ids[i])
    }
    // Build dynamic IN clause
    placeholders := make([]string, len(ids))
    args := make([]any, len(ids))
    for i, id := range ids {
        placeholders[i] = "?"
        args[i] = id
    }
    var count int
    tx.QueryRow(
        `SELECT COUNT(*) FROM tasks WHERE id IN (`+strings.Join(placeholders, ",")+`) AND status != 'DONE'`,
        args...,
    ).Scan(&count)
    return count > 0
}
```

### Dispatch update
`Dispatch` accepts optional `--depends-on` flag (comma-separated task IDs). Stored as-is in the column.

```bash
kanban task dispatch \
  --title "Build auth API" \
  --priority 20 \
  --role worker \
  --depends-on "TASK-8"
```

### Verification
```bash
# 1. Dispatch two tasks
kanban task dispatch --title "Scaffold" --role worker
kanban task dispatch --title "Deploy" --role worker --depends-on "TASK-<scaffold-id>"
# 2. Claim for worker role → claims Scaffold, skips Deploy
kanban task claim-next --agent test --role worker
# 3. Complete Scaffold
kanban task complete <scaffold-id> --agent test
# 4. Claim again → now claims Deploy
kanban task claim-next --agent test --role worker
```

---

## Step 2: `extend-lease` — Lease renewal (PM#5)

### Files changed
- `internal/task/service.go` — new `ExtendLease` method
- `cmd/kanban/update.go` — `extend-lease` CLI command

### Method
```go
func (s *Service) ExtendLease(ctx context.Context, id, agent string, minutes int) (Task, error) {
    if minutes <= 0 { minutes = defaultLeaseMinutes }

    var task Task
    err := s.retryOnBusy(func() error {
        res, err := s.db.Exec(
            `UPDATE tasks
                SET lease_until = datetime('now', '+' || ? || ' minutes'),
                    updated_at = CURRENT_TIMESTAMP
              WHERE id = ? AND assigned_agent = ?`,
            minutes, id, agent,
        )
        // ... error handling same as LogProgress ...
        task, err = s.View(ctx, id)
        return err
    })
    return task, err
}
```

**No events, no hooks** — heartbeat extension is not a state transition. The updated `lease_until` is visible via `kanban task view <id>`.

### CLI
```bash
kanban task extend-lease TASK-101 --agent worker-1 --minutes 30
```

Defaults to `defaultLeaseMinutes` (15) if `--minutes` omitted.

### Verification
```bash
# 1. Claim a task → lease_until is now + 15 min
kanban task claim-next --agent test --role worker
# 2. Extend to 60 min
kanban task extend-lease <id> --agent test --minutes 60
# 3. Verify extension
kanban task view <id> | jq '.task.lease_until'
```

---

## Step 3: Cross-agent review gate (PM#3)

### Files changed
- `internal/task/review.go` — one check in `ReviewApprove` and `ReviewReject`
- `cmd/kanban/config.go` — `review.require_separate_agent` config key (default: `true`)

### Configurability

The gate is on by default but can be disabled for small single-agent projects:

```bash
kanban config set review.require_separate_agent=false
```

Stored in `.kanban/config.toml`. When `false`, self-review is allowed and `checkSelfReview` returns `nil` immediately.

### Logic
Query the history table for the last agent who claimed the task:

```sql
SELECT agent FROM history
 WHERE task_id = ?
   AND action = 'CLAIM'
 ORDER BY id DESC LIMIT 1
```

If result matches the reviewing agent → return `ErrSelfReview`.

### Edge case: no CLAIM history
Tasks created directly in `IN_REVIEW` (manual or script) have no CLAIM entry. Handle gracefully — allow the review:

```go
var ErrSelfReview = &ExitError{Code: 2, Message: "cannot review your own task — another agent must approve"}

func checkSelfReview(tx *sql.Tx, id, agent string) error {
    var claimingAgent sql.NullString
    err := tx.QueryRow(
        `SELECT agent FROM history WHERE task_id=? AND action='CLAIM' ORDER BY id DESC LIMIT 1`,
        id,
    ).Scan(&claimingAgent)

    if err == sql.ErrNoRows {
        return nil // no CLAIM history — task was created directly in review, allow
    }
    if err != nil {
        return err
    }
    if claimingAgent.Valid && claimingAgent.String == agent {
        return ErrSelfReview
    }
    return nil
}
```

### Implementation
Add as first check inside the retry callback in both `ReviewApprove` and `ReviewReject`. Read `review.require_separate_agent` from config before calling.

### Verification
```bash
# 1. Worker claims and completes a task
kanban task claim-next --agent worker-1 --role worker
kanban task complete <id> --agent worker-1 --review
# 2. Same agent tries to approve → rejected
kanban task approve <id> --agent worker-1
# → Error: "cannot review your own task — another agent must approve"
# 3. Different agent approves → OK
kanban task approve <id> --agent reviewer-2
# → Task marked DONE
# 4. Disable gate for single-agent project
kanban config set review.require_separate_agent=false
kanban task approve <id> --agent worker-1  # → OK
```

---

## Step 4: `claim-next --count N` — Batch parallelism (PM#1)

### Files changed
- `internal/task/claim.go` — new `ClaimBatch` method (internal)
- `cmd/kanban/dispatch.go` — extend `claim-next` with `--count` flag

### API design

Instead of a separate `claim-batch` command, extend the existing `claim-next`:

```bash
kanban task claim-next --agent "worker-1" --role worker --count 3
```

`--count 1` (default) → existing single-task behavior, returns single task JSON.
`--count N` → returns JSON array of N tasks.

This keeps the API surface clean — one command, one concept.

### SQLite batch-claim: select-then-update (not CTE)
SQLite flattens LIMIT in subquery-UPDATE. Use explicit two-step.

**Design choice**: Fetch ALL eligible candidates (up to 100), filter in Go for unmet deps, then claim up to N. If fewer than N remain after filtering, return what's available (no partial failure).

```go
func (s *Service) ClaimBatch(ctx context.Context, agent, role, project string, count int) ([]Task, error) {
    tx, _ := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
    defer tx.Rollback()

    // Step 1: select ALL eligible candidates (cap at 100 to avoid OOM)
    maxFetch := count * 5
    if maxFetch > 100 { maxFetch = 100 }

    rows, _ := tx.Query(`
        SELECT id, title, status, role_boundary, project, priority,
               assigned_agent, lease_until, created_at, updated_at, depends_on
          FROM tasks
         WHERE role_boundary = ?
           AND project = ?
           AND (status = 'TODO'
                OR (status = 'IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP))
         ORDER BY priority ASC, created_at ASC
         LIMIT ?`, role, project, maxFetch,
    )

    // Filter out tasks with unmet deps
    var claimable []Task
    for rows.Next() {
        t, _ := scanTask(rows)
        if hasUnmetDeps(tx, t) {
            continue
        }
        claimable = append(claimable, t)
        if len(claimable) >= count {
            break
        }
    }

    // Step 2: claim up to `count` tasks in same transaction
    claimed := make([]Task, 0, len(claimable))
    for _, t := range claimable[:min(count, len(claimable))] {
        res, _ := tx.Exec(
            `UPDATE tasks SET status='IN_PROGRESS', assigned_agent=?,
             lease_until=datetime('now','+' || ? || ' minutes'), updated_at=CURRENT_TIMESTAMP
             WHERE id=? AND status IN ('TODO','IN_PROGRESS')`,
            agent, defaultLeaseMinutes, t.ID,
        )
        n, _ := res.RowsAffected()
        if n == 0 { continue } // race lost, skip

        tx.Exec(`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'CLAIM')`, t.ID, agent)
        insertEvent(tx, "task.claimed", EventPayload{TaskID: t.ID, Agent: agent, Title: t.Title})
        claimed = append(claimed, t)
    }

    if err := tx.Commit(); err != nil {
        return nil, fmt.Errorf("batch claim commit: %w", err)
    }

    // Fire hooks outside tx
    for _, t := range claimed {
        runHook(s.hooksDir, "task.claimed", EventPayload{TaskID: t.ID, Agent: agent, Title: t.Title})
    }
    return claimed, nil
}
```

### Verification
```bash
# 1. Dispatch 5 independent tasks
# 2. Claim 3 at once
kanban task claim-next --agent worker-bot --role worker --count 3
# → Returns JSON array of 3 tasks
# → 3 become IN_PROGRESS, 2 remain TODO
kanban task search --status TODO | jq length  # → 2
```

---


## Step 5: Optional subagent delegation

### Files changed (agent/skill markdown — no Go code)
- `internal/bootstrap/embed/agents/pi/manager.md` — add `manager_mode` config, parallel vs serial
- `internal/bootstrap/embed/agents/pi/worker.md` — update for claim-next --count + extend-lease
- `internal/bootstrap/embed/agents/pi/reviewer.md` — update for cross-agent gate awareness
- `internal/bootstrap/embed/skills/manager/approve-plan.md` — document both modes
- `internal/bootstrap/embed/skills/worker/complete-task.md` — extend-lease instruction
- `internal/bootstrap/embed/skills/worker/claim-next-task.md` — mention --count as parallel path

### Design

**Don't force parallelism.** Many projects want a single manager + single worker. Make it a config:

```yaml
# .kanban/config.toml
[manager]
mode = "serial"   # or "parallel"
```

- `serial` (default): manager executes tasks itself, one at a time. Works fine for small projects.
- `parallel`: manager uses `claim-next --count N` + subagent-creator to spawn parallel workers.

### Manager agent update — mode-aware

```
You are a kanban manager agent.

manager_mode = serial (default):
  Plan → dispatch tasks → claim them yourself → execute one at a time

manager_mode = parallel:
  Plan → dispatch tasks → claim-next --count N → spawn N worker subagents in parallel
  You NEVER execute tasks in parallel mode — only delegate.
```

### Worker agent update — claim-next --count + extend-lease awareness

```
You execute individual tasks claimed from the kanban board.

For long-running work (>15 min), periodically run:
  kanban task extend-lease <task-id> --agent <name> --minutes 30
```

### Reviewer agent update — cross-agent gate awareness

```
You review tasks submitted for review. You MUST NOT review tasks you claimed.
The system enforces this: if you try, it will reject with "cannot review your own task."
```

### Post-merge: regenerate skills
```bash
kanban init --harness pi
```

---

## Step 6: Project env auto-detection (PM#8)

### Files changed
- `cmd/kanban/config.go` — `resolveConfig` logic

### Rule
1. If `--db` flag explicitly passed (not the default) → use it.
2. Else if `KANBAN_DB` env var set → use it.
3. Else → walk up from `os.Getwd()` looking for `.kanban/` directory. First hit wins.

```go
func findProjectRoot() string {
    dir, _ := os.Getwd()
    for dir != "/" && dir != "." {
        if _, err := os.Stat(filepath.Join(dir, ".kanban")); err == nil {
            return filepath.Join(dir, ".kanban", "kanban.db")
        }
        dir = filepath.Dir(dir)
    }
    return ".kanban/kanban.db"
}
```

Called only as fallback when no explicit path + no env var.

### Subagent benefit
A subagent spawned in any subdirectory of a kanban project auto-finds the DB. No more `KANBAN_DB` manual override needed.

### Verification
```bash
cd ./deeply/nested/subdir
kanban task search  # finds the DB in parent .kanban/ automatically
kanban task search --db .kanban/kanban.db  # explicit path still works
KANBAN_DB=/custom/path kanban task search  # env var still overrides
```

---

## Step 7: `kanban status --burndown` — Progress visibility (PM#7)

### Files changed
- `internal/task/queries.go` — add `Burndown` method
- `cmd/kanban/view.go` — `status` subcommand with `--burndown` and `--json` flags

### Data
```go
type BurndownStats struct {
    ByStatus    map[string]int `json:"by_status"`
    ByRole      map[string]int `json:"by_role"`
    Total       int            `json:"total"`
    DoneCount   int            `json:"done_count"`
    PercentDone float64        `json:"percent_done"`
}
```

### Output: table (human) or JSON (pipeable)
Default output is a human-readable table. Add `--json` flag for machine parsing.

```bash
kanban status           # human table, counts by status + role
kanban status --json    # same data as JSON
kanban status --burndown # human table with % charts
```

Table output:
```
Status               Count
───────────────────────────
TODO                 8
IN_PROGRESS          3
BLOCKED              1
IN_REVIEW            2
DONE                 6
───────────────────────────
Total:    20  │  Done: 6  │  30% complete

By Role:
worker     14
reviewer    6
```

### Implementation
New `Burndown()` method reuses existing `Stats()` query but adds formatting. The `stats` subcommand becomes a `status` subcommand. Backwards compatible: `kanban task stats` still works (alias).

### Verification
```bash
# After dispatching 10 tasks, completing 3:
kanban status
# → TODO: 7, DONE: 3, 30% complete
kanban status --json | jq '.percent_done'  # → 30
```

---

## Step 8: `kanban plan lint` — Catch bad plans before execution

### Files changed
- `internal/task/lint.go` — new `LintPlan` function
- `cmd/kanban/lint.go` — `plan lint` CLI command

### What it checks

```bash
kanban plan lint          # lint all tasks on the board
kanban plan lint --json   # machine-readable output
```

Checks run against the current board state:

| Check | Example warning |
|-------|----------------|
| Unknown dependency | `WARN: TASK-12 depends on unknown task TASK-99` |
| Dependency cycle | `ERROR: Cycle detected: TASK-3 → TASK-5 → TASK-3` |
| Task with no role | `WARN: TASK-7 has no role_boundary set` |
| Blocked by nonexistent task | `WARN: TASK-10 is blocked but blocker TASK-4 is DONE` |

### Output

```
WARN  TASK-12  depends on unknown task TASK-99
ERROR TASK-3   cycle detected: TASK-3 → TASK-5 → TASK-3
WARN  TASK-7   no role_boundary set

3 issues found (1 error, 2 warnings)
```

Exits 0 if no errors (warnings are OK). Exits 1 if any errors.

### Implementation

```go
type LintIssue struct {
    TaskID   string `json:"task_id"`
    Severity string `json:"severity"` // "error" or "warn"
    Message  string `json:"message"`
}

func LintPlan(ctx context.Context, db *sql.DB, project string) ([]LintIssue, error) {
    // 1. Load all tasks for project
    // 2. Build dependency graph
    // 3. Check each rule: unknown deps, cycles (DFS), missing roles, stale blockers
    // 4. Return sorted issues (errors first)
}
```

Cycle detection uses iterative DFS — no recursion, no stack overflow on deep chains.

### Verification
```bash
# 1. Dispatch tasks with bad dependency
kanban task dispatch --title "Deploy" --role worker --depends-on "TASK-99"
# 2. Lint → catches it
kanban plan lint
# → WARN TASK-X depends on unknown task TASK-99
# → 1 issue found (0 errors, 1 warning)
```

---

## Step 9: `approve-plan --all` flag (PM#4)

### Files changed (skill markdown only)
- `internal/bootstrap/embed/skills/manager/approve-plan.md`

### Change
Update `approve-plan.md` to say: "If the user passes `--all`, dispatch every task in the proposal regardless of checkbox state. Otherwise, only dispatch `[x]` checked items."

No Go changes — pure skill behavior.

### Verification
```bash
# Approve all tasks in a plan, ignoring checkboxes
/approve-plan --all
# → dispatches every task in the proposal
```

---

## Summary of changes

| Release | Step | Change | Files | Verification |
|---------|------|--------|-------|--------------|
| v0.4 | 1 | `depends_on` | `model.go`, `schema.sql`, `claim.go`, `service.go`, `dispatch.go` | claim skips until dep done, then claims |
| v0.4 | 2 | `extend-lease` | `service.go`, `update.go` | view shows extended `lease_until` |
| v0.4 | 3 | Review gate (configurable) | `review.go`, `config.go` | same-agent approve rejected; gate disableable |
| v0.5 | 4 | `claim-next --count N` | `claim.go`, `dispatch.go` | claim-next --count 3 returns 3 tasks |
| v0.5 | 5 | Optional subagent delegation | 6 agent/skill `.md` files | manager serial by default, parallel opt-in |
| v0.5 | 6 | Env auto-detection | `config.go` | subdir kanban finds parent `.kanban/` |
| v0.6 | 7 | `kanban status` | `queries.go`, `view.go` | table output, `--json` flag |
| v0.6 | 8 | `kanban plan lint` | `lint.go`, `cmd/kanban/lint.go` | catches unknown deps, cycles, missing roles |
| v0.6 | 9 | `approve-plan --all` | `approve-plan.md` | dispatches all tasks, ignores checkboxes |

All changes are incremental. No structural rewrites, no new dependencies, no breaking schema changes. The DB remains compatible with existing boards. Migration runs at `storage.Open()` — no manual step needed.

---

## What we're skipping (for now)

- **Worker discovery / load balancing** — a coordinator process. Defer until `claim-next --count N` is proven.
- **Cycle-time metrics** — needs timestamps per transition. Schema change. Defer.
- **.env file extension** — MCP host limitation, not ours to fix.
- **Dependency visualization** — nice-to-have, deferred to v0.6+.