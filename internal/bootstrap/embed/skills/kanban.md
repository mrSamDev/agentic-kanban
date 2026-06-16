---
name: kanban
description: Overview of the kanban coordination protocol — state machine, lease model, roles, and lifecycle. Agents read this before role-specific skills.
role: system
type: protocol
---

# Kanban Coordination Protocol

A shared SQLite database that multiple AI agents use to coordinate work.
No server, no daemon, no queues. One `.db` file, all agents share it.

## State Machine

Every task moves through five states:

```
TODO ── claim ──> IN_PROGRESS ── complete --review ──> IN_REVIEW ── approve ──> DONE
                        │                                      │
                        │ block                                │ reject
                        ▼                                      ▼
                     BLOCKED                                  TODO
```

- **TODO**: Ready to work. Anyone with matching role can claim it.
- **IN_PROGRESS**: Someone is working on it. Has a lease (15 min default).
- **IN_REVIEW**: Work is done, needs approval. Reviewer can approve or reject.
- **DONE**: Finished. Terminal state.
- **BLOCKED**: Can't proceed. Requires manager intervention to unblock.

## Lease Model

When an agent claims a task, it gets a 15-minute lease. The lease is the
agent's ownership ticket — only the lease holder can complete, block, or
extend the lease.

- **Lease renewal**: Call `log-progress` or `extend-lease` to reset the
  timer. Use this as a heartbeat for long-running work.
- **Lease expiry**: If the agent crashes or the session ends, the lease
  expires after 15 minutes. The task becomes reclaimable.
- **Crash recovery**: If the orchestrator claims a task and spawns a
  subagent via `--transfer`, the subagent owns the claim. If the subagent
  crashes, lease expiry reclaims the task — same as any worker crash.

```
Worker-A claims TASK-1. Worker-A crashes.
15 minutes later lease expires.
Worker-B calls claim-next and gets TASK-1.
```

## Dependency Graph

Tasks can depend on other tasks. A task with unmet dependencies cannot
be claimed until all dependencies are DONE.

- Dependencies stored in `task_dependencies` join table (FK-enforced)
- Claim checks: `claim-next` respects deps by default (`--respect-deps`)
- No claim: if a dependency isn't DONE, the task is skipped for claiming
- Plan lint: detects circular dependencies via DFS
- Cycle detection: recursive CTE prevents circular dependency graphs at
  dispatch time

## Roles

| Role | Duties | Commands |
|------|--------|----------|
| `manager` | Plan work, dispatch tasks, review progress | dispatch, plan lint, review-backlog, view-task |
| `worker` | Claim tasks, do work, report progress | claim-next, claim, complete, block, log-progress, extend-lease |
| `reviewer` | Review submissions, approve or reject | approve, reject |

Roles are enforced at the database level — agents can only claim tasks
matching their `role_boundary`.

## Review Gate

Work submitted for review (`complete --review`) enters `IN_REVIEW` state.
Any agent with a different identity than the claimer can approve or reject.

- **Self-review**: Blocked by default. An agent cannot approve its own
  work. Set `KANBAN_ALLOW_SELF_REVIEW=true` to bypass (use when the
  orchestrator acts as both worker and reviewer).
- **Approve**: `IN_REVIEW → DONE`
- **Reject**: `IN_REVIEW → TODO` (with reason, goes back to backlog)

## Batch Operations

Bulk actions that execute atomically in a single transaction:

- **Batch claim**: `claim-next --count N` — claims up to N eligible tasks
- **Batch complete**: `batch complete --ids TASK-1,TASK-2 --agent X` —
  completes multiple tasks in one transaction
- **Batch set-priority**: `batch set-priority --ids TASK-1 --priority 10`
- **Batch set-project**: `batch set-project --ids TASK-1 --project sprint-2`

## Claim Transfer (Hierarchical Delegation)

For long-running subagent work (>15 min), transfer the claim so the
subagent owns it and can complete independently:

```
kanban task claim TASK-5 --agent orchestrator --transfer --to pi-worker
```

After transfer, the subagent (`pi-worker`) owns the claim — can complete,
extend lease, log progress. If the subagent crashes, lease expiry reclaims
the task.

For fast subagent work (<15 min), no transfer needed — keep the claim on
the orchestrator and use the collect-results pattern.

## Commands Summary

| Command | Agent | What it does |
|---------|-------|-------------|
| `task dispatch --title --role --priority` | manager | Create a task as TODO |
| `task claim <id> --agent` | worker | Claim by ID |
| `task claim-next --agent --role` | worker | Grab highest-priority TODO |
| `task claim <id> --agent --transfer --to` | manager | Transfer claim to another agent |
| `task log-progress <id> --agent --note` | worker | Report progress, renew lease |
| `task extend-lease <id> --agent --minutes` | worker | Extend lease without state change |
| `task complete <id> --agent --review` | worker | Mark done or submit for review |
| `task block <id> --agent --reason` | worker | Mark blocked, drop lease |
| `task approve <id> --agent` | reviewer | IN_REVIEW to DONE |
| `task reject <id> --agent --reason` | reviewer | IN_REVIEW to TODO |
| `task view <id>` | anyone | Full details + notes + history |
| `task search --status --role --agent` | manager | Filter the board |
| `batch set-priority --ids --priority` | manager | Bulk priority |
| `batch set-project --ids --project` | manager | Bulk project label |

## Data Integrity

- Every state change is a database transaction — two agents can't claim
  the same task
- All actions are logged in the `history` table (agent, action, timestamp)
- Events are append-only with TTL-based cleanup (default 3 days)
- Foreign keys enforce referential integrity across tasks, notes, and
  history

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

Events auto-expire after `ttl_seconds` (default 3 days). TTL cleanup runs
during board operations.

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

- Single-file hooks run synchronously and complete before the command
  returns.
- `.d/` directory hooks run concurrently in goroutines with a 30s timeout.
- Hook receives JSON event on stdin:
  `{"event": "task.completed", "payload": {...}}`
- Must be executable (`chmod +x`).
- Non-zero exit is logged to stderr but does not fail the operation.
- Missing hook or missing `.d/` directory is silently ignored.
- The runner waits up to 35s for `.d/` hooks before process exit (exceeds
  30s execHook timeout, preventing mid-flight goroutine killing).