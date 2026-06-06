# Post-Mortem Fixes Plan

Branch: `post-mortem-fixes`
Target: 7 Go changes + 1 skill change = all 8 negatives resolved

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

---

## Step 1: `depends_on` — Dependency tracking (#6)

### Files changed
- `internal/storage/schema.sql` — migration
- `internal/task/model.go` — add `DependsOn` field
- `internal/task/claim.go` — guard in `ClaimNext`
- `internal/task/service.go` — `Dispatch` accepts `dependsOn`
- `cmd/kanban/dispatch.go` — `--depends-on` flag

### Schema
Add column via migration in `storage.Open()` (same pattern as `project` and `ttl_seconds`):

```sql
ALTER TABLE tasks ADD COLUMN depends_on TEXT;
```

`depends_on` stores comma-separated task IDs (e.g., `TASK-8,TASK-9`) or NULL.

### Model
```go
type Task struct {
    // ...existing fields
    DependsOn *string `json:"depends_on"` // comma-separated dependency IDs, nullable
}
```

### Claim guard in `ClaimNext`
Before claiming, check dependencies are all `DONE`:

```sql
-- Inside the serializable transaction, after selecting the candidate task:
SELECT COUNT(*) FROM tasks
 WHERE id IN (split(depends_on, ','))
   AND status != 'DONE'
```

If count > 0, skip this task and try the next one (loop inside the tx until we find one with no unmet deps or run out of candidates).

Implementation sketch:
```go
// Inside ClaimNext, after the initial SELECT candidate:
candidateIDs := // query top N (not just 1) candidates
for _, c := range candidateIDs {
    if c.DependsOn == nil || *c.DependsOn == "" {
        // claim this one
        break
    }
    deps := strings.Split(*c.DependsOn, ",")
    // trim spaces, check each dep is DONE
    unmet, _ := tx.QueryContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id IN (...deps...) AND status != 'DONE'`)
    if unmet == 0 {
        // claim this one
        break
    }
    // else skip, try next
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

### Test
- Dispatch A. Dispatch B with `--depends-on "TASK-A"`.
- Claim for B's role → should skip B until A is DONE.
- Complete A. Claim → should now claim B.

---

## Step 2: `batch-claim` — Parallelism (#1)

### Files changed
- `internal/task/claim.go` — new `ClaimBatch` method
- `cmd/kanban/dispatch.go` — `claim-batch` CLI command

### SQLite batch-claim: select-then-update (not CTE)
SQLite flattens LIMIT in subquery-UPDATE. Use explicit two-step:

```go
func (s *Service) ClaimBatch(ctx context.Context, agent, role, project string, count int) ([]Task, error) {
    tx, _ := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
    defer tx.Rollback()

    // Step 1: select candidate IDs (with dependency guard baked in)
    rows, _ := tx.Query(`
        SELECT id, title, status, role_boundary, project, priority,
               assigned_agent, lease_until, created_at, updated_at, depends_on
          FROM tasks
         WHERE role_boundary = ?
           AND project = ?
           AND (status = 'TODO'
                OR (status = 'IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP))
         ORDER BY priority ASC, created_at ASC
         LIMIT ?
    `, role, project, count*3) // fetch extra to account for blocked-by-deps

    // candidates with unmet-deps filter
    var claimable []Task
    for rows.Next() { ... filter depends_on ... }

    // Step 2: claim up to `count` tasks
    for _, t := range claimable[:min(count, len(claimable))] {
        tx.Exec(`UPDATE tasks SET status='IN_PROGRESS', assigned_agent=?,
                  lease_until=datetime('now','+? minutes'), updated_at=CURRENT_TIMESTAMP
                  WHERE id=? AND status IN ('TODO','IN_PROGRESS')`, agent, defaultLeaseMinutes, t.ID)
        // insert history + events per claimed task
    }
    tx.Commit()
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

### Why this works
- One transaction for N claims — atomic, serializable.
- Depends_on guard is already in the candidate filter.
- Manager agent calls once instead of N claim-next rounds.

---

## Step 3: Cross-agent review gate (#3)

### Files changed
- `internal/task/review.go` — one check in `ReviewApprove` and `ReviewReject`

### Logic
Query the history table for the claimer of the task:

```sql
SELECT agent FROM history
 WHERE task_id = ?
   AND action = 'CLAIM'
 ORDER BY id DESC LIMIT 1
```

If result matches the reviewing agent → return `ErrSelfReview`:

```go
var ErrSelfReview = &ExitError{Code: 2, Message: "cannot review your own task — another agent must approve"}
```

No schema change needed. The history table already captures who claimed.

### Why not a column
The `Complete`/submit clears `assigned_agent = NULL` before review starts. History is the permanent record of who worked on it.

### Implementation
Add the check at the top of `ReviewApprove` and `ReviewReject`:

```go
var claimingAgent string
tx.QueryRow(`SELECT agent FROM history WHERE task_id=? AND action='CLAIM' ORDER BY id DESC LIMIT 1`, id).Scan(&claimingAgent)
if claimingAgent == agent {
    return ErrSelfReview
}
```

---

## Step 4: `extend-lease` command — Lease renewal (#5)

### Files changed
- `internal/task/service.go` — new `ExtendLease` method
- `cmd/kanban/update.go` — `extend-lease` CLI command

### Method
```go
func (s *Service) ExtendLease(ctx context.Context, id, agent string, minutes int) (Task, error) {
    // Validate: task exists AND assigned_agent = agent
    // Update: lease_until = datetime('now', '+' || minutes || ' minutes')
    // Return updated task
    // No event, no hook — lightweight heartbeat extension
}
```

### CLI
```bash
kanban task extend-lease TASK-101 --agent worker-1 --minutes 30
```

Defaults to `defaultLeaseMinutes` (15) if `--minutes` omitted.

### Subagent protocol update
Manager skill template adds: "Run `kanban task extend-lease <task-id> --agent <name>` every 10 minutes for long-running work."

---

## Step 5: `kanban status --burndown` — Progress visibility (#7)

### Files changed
- `internal/task/queries.go` — enhance `Stats` or add `Burndown` method
- `cmd/kanban/view.go` — `status` subcommand with `--burndown` flag

### Data
```go
type BurndownStats struct {
    ByStatus    map[string]int    `json:"by_status"`
    ByRole      map[string]int    `json:"by_role"`
    Total       int               `json:"total"`
    DoneCount   int               `json:"done_count"`
    PercentDone float64           `json:"percent_done"`
}
```

### Output format
Simple table, not JSON (for human reading):

```
Status               Count
───────────────────────────
TODO                 8
IN_PROGRESS          3
BLOCKED              1
IN_REVIEW            2
DONE                 6

Total:    20  │  Done: 6  │  30% complete
```

### CLI
```bash
kanban status           # counts by status + role
kanban status --burndown # includes % charts
```

### Implementation
Reuses existing `Stats()` method. Formats as a table with `fmt.Printf`. No new queries needed.

---

## Step 6: Project env auto-detection (#8)

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

---

## Step 7: Skill updates

### Files changed (skill markdown)
- `internal/bootstrap/embed/skills/manager/approve-plan.md`
- `internal/bootstrap/embed/skills/manager/dispatch-plan.md`
- `internal/bootstrap/embed/skills/worker/claim-next-task.md` or new `claim-batch.md`
- `internal/bootstrap/embed/skills/worker/complete-task.md` (extend-lease instruction)

### 7a: `approve-plan --all` flag
Update `approve-plan.md` to say: "If the user passes `--all`, dispatch every task in the proposal regardless of checkbox state. Otherwise, only dispatch `[x]` checked items."

No Go changes — the skill just skips the checkbox check when told `--all`.

### 7b: `claim-batch` skill
Add `batch-claim` to `SkillNames` in `embed.go` for the worker role. New skill markdown:

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

\`\`\`bash
kanban task claim-batch --agent <name> --role <role> --count <N> [--project <project>]
\`\`\`

Claims up to N available tasks (respecting dependencies) in one transaction.
Returns JSON array of claimed tasks.
```

### 7c: Lease extension instruction
Update `complete-task.md` (or the manager dispatch skill) to include: "For long-running work (>15 min), run `kanban task extend-lease <task-id> --agent <name> --minutes 30` every 10 minutes to prevent lease expiry."

---

## Summary of changes

| # | Change | Go types | Skill markdown |
|---|--------|----------|----------------|
| 6 | depends_on | `model.go`, `schema.sql`, `claim.go`, `service.go`, `dispatch.go` | — |
| 1 | batch-claim | `claim.go`, `dispatch.go` | New `claim-batch.md` |
| 3 | Review gate | `review.go` | — |
| 5 | extend-lease | `service.go`, `update.go` | Update `complete-task.md` |
| 7 | kanban status | `queries.go`, `view.go` | — |
| 8 | Env auto-detection | `config.go` | — |
| 4 | --all flag | — | Update `approve-plan.md` |

All changes are incremental. No structural rewrites, no new dependencies, no breaking schema changes. The DB remains compatible with existing boards.

---

## What we're skipping (for now)

- **Worker discovery / load balancing** — a coordinator process. Defer until `batch-claim` is proven.
- **Cycle-time metrics** — needs timestamps per transition. Schema change. Defer.
- **.env file extension** — MCP host limitation, not ours to fix.