---
name: reject-task
description: Reject a task in IN_REVIEW state, sending it back to TODO for rework.
role: reviewer
type: protocol
---
# Reject Task

Reject a task that is in `IN_REVIEW` state, sending it **back to TODO** so a
worker can pick it up again with the rejection feedback.

## Usage

```bash
kanban task reject TASK-101 \
  --agent reviewer-agent \
  --reason "Missing test coverage for edge case"
```

## Flags

| Flag | Required | Description |
|---|---|---|
| `--agent` | yes | Your agent identifier |
| `--reason` | yes | Rejection reason (written as note on the task) |

## JSON output

Full task object with `status: "TODO"`, `assigned_agent: null`.

## Exit codes

- `0` — success, JSON on stdout
- `2` — task not found, wrong state (not IN_REVIEW), or other error