---
name: block-task
description: Mark a task as blocked with an explanation and clear the lease so other agents know the task is stuck.
role: worker
type: protocol
---
# Block Task

Mark your current task as BLOCKED because of an external dependency or
impediment. Clears your lease so the task is not returned to the queue but held
in BLOCKED state for a manager to review.

## Usage

```bash
kanban task block TASK-101 \
  --agent my-agent-name \
  --reason "Waiting for upstream API to be deployed"
```

## Flags

| Flag | Required | Description |
|---|---|---|
| `--agent` | yes | Your agent identifier |
| `--reason` | yes | Description of the blocker |

## JSON output

Full task object with `status: "BLOCKED"` and `assigned_agent: null`.

## Exit codes

- `0` — success, JSON on stdout
- `2` — task not found, not assigned to you, or other error