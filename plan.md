# Post-Mortem Fixes Plan

Branch: `post-mortem-fixes`
Target: 7 Go changes + skill changes = all 8 negatives resolved

---

## Execution Order

Schema changes first → CLI changes → skill changes. No rebases needed.

| Step | What | Why this order |
|------|------|----------------|
| 1 | `depends_on` schema + claim guard | Schema change before anything |
| 2 | `batch-claim` N tasks in one tx | Builds on ClaimNext logic |
| 3 | Cross-agent review gate | Pure check, no schema change |
| 4 | `extend-lease` command | New command, no dependencies |
| 5 | `kanban status --burndown` | New command, read-only |
| 6 | Project env auto-detection | Config change |
| 7 | Skill updates (--all, batch-claim, extend-lease) | References new CLI |
| 8 | Force subagent usage (agent/skill .md) | Behavioral layer on top of Go changes |

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

## Step 2: `batch-claim` — Parallelism (PM#1)

### Files changed
- `internal/task/claim.go` — new `ClaimBatch` method
- `cmd/kanban/dispatch.go` — `claim-batch` CLI command

### SQLite batch-claim: select-then-update (not CTE)
SQLite flattens LIMIT in subquery-UPDATE. Use explicit two-step.

**Design choice**: Instead of `count*3` magic — fetch ALL eligible candidates (up to 100), filter in Go for unmet deps, then claim up to N. If fewer than N remain after filtering, return what's available (no partial failure). This handles worst-case (80% tasks blocked by deps) without wasting claims.

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
        t, _ := scanTaskWithDeps(rows)
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
             lease_until=datetime('now','+? minutes'), updated_at=CURRENT_TIMESTAMP
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

### CLI shape
```bash
kanban task claim-batch \
  --agent "worker-1" \
  --role worker \
  --count 3 \
  --project default
```

Returns JSON array of claimed tasks.

### Verification
```bash
# 1. Dispatch 5 independent tasks
# 2. Claim-batch 3 at once
kanban task claim-batch --agent worker-bot --role worker --count 3
# → Returns 3 tasks in JSON array
# → 3 become IN_PROGRESS, 2 remain TODO
kanban task search --status TODO | jq length  # → 2
```

---

## Step 3: Cross-agent review gate (PM#3)

### Files changed
- `internal/task/review.go` — one check in `ReviewApprove` and `ReviewReject`

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
Add as first check inside the retry callback in both `ReviewApprove` and `ReviewReject`.

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
```

---

## Step 4: `extend-lease` command — Lease renewal (PM#5)

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

## Step 5: `kanban status --burndown` — Progress visibility (PM#7)

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
# In project root:
cd ./deeply/nested/subdir
kanban task search  # finds the DB in parent .kanban/ automatically
kanban task search --db .kanban/kanban.db  # explicit path still works
KANBAN_DB=/custom/path kanban task search  # env var still overrides
```

---

## Step 7: Skill updates

### Files changed (skill markdown)
- `internal/bootstrap/embed/skills/manager/approve-plan.md`
- `internal/bootstrap/embed/skills/manager/dispatch-plan.md`
- `internal/bootstrap/embed/skills/worker/claim-next-task.md`
- `internal/bootstrap/embed/skills/worker/claim-batch.md` — new file
- `internal/bootstrap/embed/skills/worker/complete-task.md`
- `internal/bootstrap/embed.go` — add `claim-batch` to `SkillNames`

### 7a: `approve-plan --all` flag
Update `approve-plan.md` to say: "If the user passes `--all`, dispatch every task in the proposal regardless of checkbox state. Otherwise, only dispatch `[x]` checked items."

No Go changes — the skill just skips the checkbox check when told `--all`.

### 7b: New `claim-batch` skill
Add `batch-claim` to `SkillNames["worker"]` in `embed.go`. New skill markdown:

```markdown
---
name: claim-batch
description: Claim multiple tasks at once
role: worker
type: protocol
---

# Claim Batch

Claim up to N tasks in one atomic operation.

## Usage

```bash
kanban task claim-batch --agent <name> --role <role> --count <N> [--project <project>]
```

Claims up to N available tasks (respecting dependencies) in one transaction.
Returns JSON array of claimed tasks.
```

### 7c: Lease extension instruction
Update `complete-task.md` to include: "For long-running work (>15 min), run `kanban task extend-lease <task-id> --agent <name> --minutes 30` every 10 minutes to prevent lease expiry."

### Post-merge: regenerate skills
After merging this branch, users should run:
```bash
kanban init --harness pi   # regenerates skill files from embedded templates
```
This ensures the new/updated skill .md files land in `.pi/skills/`.

---

## Step 8: Force subagent usage (behavioral enforcement)

### Files changed (agent/skill markdown — no Go code)
- `internal/bootstrap/embed/agents/pi/manager.md` — rewrite to mandate subagent delegation
- `internal/bootstrap/embed/agents/pi/worker.md` — update for claim-batch + extend-lease
- `internal/bootstrap/embed/agents/pi/reviewer.md` — update for cross-agent gate
- `internal/bootstrap/embed/skills/manager/approve-plan.md` — add subagent spawn step
- `internal/bootstrap/embed/skills/worker/claim-batch.md` — new (added in 7b)
- `internal/bootstrap/embed/skills/worker/complete-task.md` — extend-lease instruction
- `internal/bootstrap/embed/skills/worker/claim-next-task.md` — mention claim-batch as preferred path

### 8a: Manager agent rewrite — mandate subagent delegation

**Current (weak):**
```
You are a kanban manager agent. Manage the task board using the registered kanban tools.
```

**Proposed (enforced):**
```
You are a kanban manager agent. Your ONLY job is to plan work, dispatch tasks,
and monitor progress. You MUST NEVER execute tasks yourself.

Rule: Every task execution MUST be delegated via the subagent-creator skill
to a worker subagent. This is not optional — you do not have permission
to claim, log-progress, or complete tasks.

Workflow:
1. Review backlog → identify what needs doing
2. Dispatch plan → create tasks on the board
3. Claim tasks in batch → `kanban task claim-batch --count N`
4. Spawn parallel subagents → use subagent-creator with parallel mode,
   one worker subagent per claimed task
5. Monitor → poll board status, handle blockers
6. Review → spawn reviewer subagents for tasks in IN_REVIEW

The subagent-creator skill is your primary execution tool. The kanban
CLI is your monitoring dashboard — you read from it, you never write to
it for task execution.
```

### 8b: Worker agent update — claim-batch + extend-lease awareness

```
You execute individual tasks claimed from the kanban board.

Available workflow:
1. Manager spawns you for a specific claimed task
2. Your task ID is provided in your instructions
3. For long-running work (>15 min), periodically run:
   `kanban task extend-lease <task-id> --agent <name> --minutes 30`
4. Log progress with `kanban task log-progress <task-id> --agent <name> --note "..."`
5. Complete with `kanban task complete <task-id> --agent <name> --review`
```

### 8c: Reviewer agent update — cross-agent gate awareness

```
You review tasks submitted for review. You MUST NOT review tasks you claimed.
The system enforces this: if you try, it will reject with "cannot review your own task."
```

### 8d: approve-plan skill — spawn subagents after dispatch

After the "Dispatch checked items" section, add:

```
After dispatching, use the subagent-creator skill to spawn worker subagents:

1. `kanban task claim-batch --agent <manager-name> --role worker --count <N>`
2. For each claimed task, spawn a subagent:
   subagent: {
     agent: "worker-<task-id>",
     task: "Execute task <task-id>: <title>. Use kanban task log-progress,
            extend-lease, and complete commands."
   }
```

### Why this works
Pure behavioral enforcement — no Go code needed. The 7 Go changes above are the infrastructure that makes subagent usage safe:

| Go enabler | Why subagents need it |
|-----------|----------------------|
| `batch-claim` | Manager claims N tasks in one call → spawns N subagents in parallel |
| `extend-lease` | Subagent running 20 min doesn't expire mid-work |
| `depends_on` | Subagents can't claim before deps are done — manager doesn't need to orchestrate |
| Cross-agent review gate | Worker subagent ≠ reviewer subagent. Enforced programmatically |
| Env auto-detection | Subagent spawned in subdir auto-finds the `.kanban/` DB |

---

## Summary of changes

| PM# | Change | Go types | Skill markdown | Verification |
|-----|--------|----------|----------------|--------------|
| 6 | depends_on | `model.go`, `schema.sql`, `claim.go`, `service.go`, `dispatch.go` | — | claim skips until dep done, then claims |
| 1 | batch-claim | `claim.go`, `dispatch.go` | New `claim-batch.md` | claim-batch --count 3 returns 3 tasks |
| 3 | Review gate | `review.go` | — | same-agent approve rejected, different-agent OK |
| 5 | extend-lease | `service.go`, `update.go` | Update `complete-task.md` | view shows extended lease_until |
| 7 | kanban status | `queries.go`, `view.go` | — | table output, --json flag |
| 8 | Env auto-detection | `config.go` | — | subdir kanban finds parent .kanban/ |
| 4 | --all flag | — | Update `approve-plan.md` | dispatches all tasks, ignores checkboxes |
| 9 | Force subagents | — | 7 agent/skill .md files | manager MUST delegate, never execute |

All changes are incremental. No structural rewrites, no new dependencies, no breaking schema changes. The DB remains compatible with existing boards. Migration runs at `storage.Open()` — no manual step needed.

---

## What we're skipping (for now)

- **Worker discovery / load balancing** — a coordinator process. Defer until `batch-claim` is proven.
- **Cycle-time metrics** — needs timestamps per transition. Schema change. Defer.
- **.env file extension** — MCP host limitation, not ours to fix.