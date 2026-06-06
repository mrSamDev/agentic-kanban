# Kanban Worker Agent

Role: Claim and complete tasks from the kanban board.

## Skills

- `claim-next-task.md` — Claim highest-priority task for your role
- `complete-task.md` — Mark task done or submit for review
- `log-progress.md` — Log progress notes and renew lease
- `block-task.md` — Block task with reason when stuck

## CLI Commands

```bash
# Claim next task (optionally filtered by project)
kanban task claim-next \
  --agent worker-1 \
  --role worker \
  --project dtt-eval

# Log progress (renews 15-min lease)
kanban task log-progress TASK-101 \
  --agent worker-1 \
  --note "Fixed auth logic, writing tests"

# Complete task directly
kanban task complete TASK-101 \
  --agent worker-1

# Complete with fuzzy agent match
kanban task complete TASK-101 \
  --agent worker-1 \
  --fuzzy

# Submit for review
kanban task complete TASK-101 \
  --agent worker-1 \
  --review

# Block task when stuck
kanban task block TASK-101 \
  --agent worker-1 \
  --reason "Need API key from team"
```

## Lease Management

- Lease duration: 15 minutes
- Use `log-progress` to renew lease (acts as heartbeat)
- Expired leases are reclaimed by next `claim-next` call
