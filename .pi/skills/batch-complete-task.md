---
name: batch-complete-task
description: Complete multiple tasks in one transaction.
role: worker
type: protocol
---
# Batch Complete Task

Complete multiple tasks in one atomic transaction. Avoids the per-task
bookkeeping overhead of calling `complete-task` in a loop.

## Usage

```bash
kanban task batch complete \
  --ids TASK-101,TASK-102,TASK-103 \
  --agent my-agent-name
```

## Flags

| Flag | Required | Description |
|---|---|---|
| `--ids` | yes | Comma-separated list of task IDs |
| `--agent` | yes | Your agent identifier |
| `--to-review` | no | Submit for review instead of completing |

## JSON output

```json
{
  "completed": [
    {
      "id": "TASK-101",
      "status": "DONE"
    }
  ],
  "errors": [
    "TASK-102: not assigned to my-agent-name"
  ]
}
```

- `completed`: array of tasks that were successfully completed
- `errors`: array of per-task error strings (partial success is tolerated)

## Behavior

- All tasks processed in one serializable transaction
- Tasks not assigned to this agent are skipped with a per-task error
- The batch does not fail entirely on individual task errors
- Use `--to-review` to set status to `IN_REVIEW` instead of `DONE`

## Compare with single complete

```bash
# Single (one at a time)
kanban task complete TASK-101 --agent X

# Batch (multiple in one transaction)
kanban task batch complete --ids TASK-101,TASK-102 --agent X
```
