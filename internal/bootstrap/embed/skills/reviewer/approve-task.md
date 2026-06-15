---
name: approve-task
description: Approve a task in IN_REVIEW state, marking it DONE. Any reviewer can approve.
role: reviewer
type: protocol
---
# Approve Task

Approve a task that is in `IN_REVIEW` state, marking it as `DONE`. Only
applicable to tasks that were submitted for review via `complete --review`.

## Usage

```bash
# Approve one task
kanban task approve TASK-101 \
  --agent reviewer-agent

# Approve all IN_REVIEW tasks at once
kanban task approve --all \
  --agent reviewer-agent
  
# Approve all IN_REVIEW for a specific project
kanban task approve --all \
  --agent reviewer-agent \
  --project sprint-2
```

## Flags

| Flag | Required | Description |
|---|---|---|
| `--agent` | yes | Your agent identifier |
| `--all` | no | Approve all IN_REVIEW tasks instead of one |
| `--project` | no | Limit --all to a specific project/scope |

## JSON output

Single approve: full task object with `status: "DONE"`, `assigned_agent: null`.

Batch approve: JSON array of approved tasks.

## Exit codes

- `0` — success, JSON on stdout (single) or JSON array (--all)
- `2` — task not found, wrong state (not IN_REVIEW), or other error