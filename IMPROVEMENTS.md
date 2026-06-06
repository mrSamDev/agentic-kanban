# Feedback Improvements - June 2026

Incorporates all feedback from DTT eval run.

## ✅ Implemented

### 1. Project/Scope Labels

**Problem:** Old eval tasks (prio 3-5) and new DTT tasks (prio 1-10) were flat in one pool. `claim-next` grabbed old tasks first.

**Solution:** Added `project` field to tasks with filtering.

```bash
# Dispatch with project label
kanban task dispatch \
  --title "Fix DTT bug" \
  --role worker \
  --project dtt-eval \
  --priority 5

# Claim only from specific project
kanban task claim-next \
  --agent worker-1 \
  --role worker \
  --project dtt-eval

# Search by project
kanban task search --project dtt-eval --status TODO
```

**Schema:**
- `project TEXT NOT NULL DEFAULT 'default'`
- Index: `idx_tasks_project (project, status, priority)`

---

### 2. Batch Operations

**Problem:** Couldn't re-prioritize or re-scope 15 old tasks at once.

**Solution:** New `batch` subcommand.

```bash
# Re-prioritize old tasks
kanban task batch set-priority \
  --ids TASK-1,TASK-2,TASK-3 \
  --priority 999

# Move to archive project
kanban task batch set-project \
  --ids TASK-1,TASK-2,TASK-3 \
  --project archive
```

---

### 3. Fuzzy Agent Match on Complete

**Problem:** `complete --agent` strict match required exact handle or got "not assigned to this agent" errors.

**Solution:** Added `--fuzzy` flag for substring matching.

```bash
# Exact match (default)
kanban task complete TASK-1 --agent worker-1

# Fuzzy match (substring)
kanban task complete TASK-1 --agent worker-1 --fuzzy
```

---

### 4. Approve/Reject Workflow Documented

**Problem:** Reviewer completed tasks directly instead of `complete --review → approve`. The review gate loop wasn't exercised.

**Solution:** Updated skill docs with full workflow.

```bash
# Worker submits for review
kanban task complete TASK-1 --agent worker-1 --review

# Reviewer claims
kanban task claim-next --agent reviewer-1 --role reviewer

# Reviewer approves (→ DONE) or rejects (→ TODO)
kanban task approve TASK-1 --agent reviewer-1
kanban task reject TASK-1 --agent reviewer-1 --reason "..."
```

See: `skills/reviewer/approve-task.md` (updated with workflow diagram)

---

### 5. Agent Definitions Co-located

**Problem:** Agent defs separate from skills. Skills dirs existed in `.pi/agents/`, but `subagent()` needed `.pi/agents/*.md` definition files. Surprise config step.

**Solution:** Created `.pi/agents/{manager,worker,reviewer}/` with ready-to-use agent definitions.

```
.pi/agents/
├── manager/kanban-manager.md
├── worker/kanban-worker.md
└── reviewer/kanban-reviewer.md
```

Each file includes:
- Role description
- CLI commands with examples
- Project label guidance
- Workflow notes

---

## Files Changed

| File | Change |
|------|--------|
| `internal/storage/schema.sql` | Added `project` column + index |
| `internal/task/model.go` | Added `Project` field to `Task` struct |
| `internal/task/helpers.go` | Updated `scanTask()` to scan project |
| `internal/task/service.go` | Added `project` param to `Dispatch`, `ClaimNext`; added `BatchUpdatePriority`, `BatchUpdateProject`, `CompleteWithFuzzyMatch` |
| `internal/task/queries.go` | Updated `View`, `Search` for project field |
| `cmd/kanban/main.go` | Added `--project` flags, `--fuzzy` flag, `batch` subcommand |
| `skills/manager/dispatch-task.md` | Added project flag + batch ops docs |
| `skills/manager/batch-operations.md` | **NEW** — batch ops guide |
| `skills/worker/claim-next-task.md` | Added project filter docs |
| `skills/worker/complete-task.md` | Added fuzzy match docs |
| `skills/reviewer/approve-task.md` | Added full workflow diagram |
| `.pi/agents/manager/kanban-manager.md` | **NEW** — manager agent def |
| `.pi/agents/worker/kanban-worker.md` | **NEW** — worker agent def |
| `.pi/agents/reviewer/kanban-reviewer.md` | **NEW** — reviewer agent def |

---

## Testing

```bash
# Project filtering works
./kanban task dispatch --title "DTT task" --role worker --project dtt-eval --priority 5
./kanban task dispatch --title "Old task" --role worker --priority 50
./kanban task claim-next --agent w1 --role worker --project dtt-eval  # Gets DTT task first

# Batch ops work
./kanban task batch set-priority --ids TASK-1,TASK-2 --priority 999
./kanban task batch set-project --ids TASK-1,TASK-2 --project archive

# Fuzzy match works
./kanban task complete TASK-1 --agent w1 --fuzzy  # Matches "worker-1"

# Review workflow works
./kanban task complete TASK-1 --agent w1 --review
./kanban task claim-next --agent r1 --role reviewer
./kanban task approve TASK-1 --agent r1
```

---

## Verdict

Core remains solid (SQLite, role-gated, lease-protected, history-tracked). Now with:
- Multi-project support via labels
- Batch cleanup operations
- Flexible agent matching
- Clear review workflow
- Ready-to-use agent definitions
