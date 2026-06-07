---
name: batch-claim-task
description: Claim multiple tasks atomically for parallel execution.
role: worker
type: protocol
---
# Batch Claim Task

Claim multiple tasks in one atomic transaction. Use when you have capacity
to work on several tasks simultaneously (e.g., dispatching subagents).

## Usage

```bash
kanban task batch claim \
  --agent my-agent-name \
  --role worker \
  --count 3
```

## Flags

| Flag | Required | Default | Description |
|---|---|---|---|
| `--agent` | yes | | Your agent identifier |
| `--role` | yes | | Role (`worker`, `reviewer`, etc.) |
| `--count` | no | 1 | Number of tasks to claim |
| `--project` | no | | Filter by project/scope |
| `--respect-deps` | no | true | Skip tasks with unmet dependencies |

## JSON output

```json
[
  {
    "id": "TASK-101",
    "title": "Fix auth bug",
    "status": "IN_PROGRESS",
    "assigned_agent": "my-agent-name",
    "lease_until": "2026-06-06T08:05:38Z",
    "priority": 10
  }
]
```

Returns a JSON array of claimed tasks. Empty array if no eligible work.

## Behavior

- Claims up to `--count` tasks atomically in one transaction
- Tasks selected in priority order (lowest priority value first), then FIFO
- Stale leases (expired) are reclaimed automatically
- Tasks with unmet dependencies are skipped by default
- Use `--respect-deps=false` to claim regardless of dependency status

## Prefer claim-next with --count

The `kanban task claim-next --agent X --role Y --count 3` command does the
same thing. Use whichever form reads better in your task plan.
