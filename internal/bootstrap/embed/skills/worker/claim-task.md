---
name: claim-task
description: Claim a specific task by ID instead of taking the next available. Use when a manager tells you exactly which task to work on.
role: worker
type: protocol
---
# Claim Task by ID

Claim a specific task by ID rather than scanning for the next available.
This is how subagents participate in the parent task board — the manager
sends a task ID, and you claim it directly. No shadow tasks, no confusion.

## Usage

```bash
kanban task claim TASK-12 \
  --agent worker-1
```

## Flags

| Flag | Required | Description |
|---|---|---|
| `--agent` | yes | Your agent identifier |

## Behavior

- Task must be `TODO` (or `IN_PROGRESS` with expired lease)
- Unmet dependencies block the claim — resolve those first
- Same agent cannot claim a task twice
- Other agents cannot claim an active lease
- Expired leases are reclaimed automatically

## JSON output

Full task object with `status: "IN_PROGRESS"`, `assigned_agent: "<name>"`,
`lease_until: "<timestamp>"`.

## Exit codes

- `0` — success, JSON on stdout
- `2` — task not found, wrong state, already claimed, or dependency blocked