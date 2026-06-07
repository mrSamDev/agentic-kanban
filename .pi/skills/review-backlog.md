---
name: review-backlog
description: Search tasks by filters to see what is available, blocked, or done.
role: manager
type: protocol
---
# Review Backlog

List all tasks visible to a manager. Apply optional filters to find specific
tasks: stalled, unassigned, or in a certain state.

## Usage

```bash
# All tasks
kanban task search

# Filter by status
kanban task search --status BLOCKED

# Filter by role
kanban task search --role worker --status TODO

# Filter by agent
kanban task search --agent alice
```

## Flags

| Flag | Required | Description |
|---|---|---|
| `--status` | no | Filter by status: TODO, IN_PROGRESS, BLOCKED, IN_REVIEW, DONE |
| `--role` | no | Filter by role boundary |
| `--agent` | no | Filter by assigned agent |
| `--limit` | no | Max results (default: no limit) |

## JSON output

```json
[
  {
    "id": "TASK-101",
    "title": "...",
    "status": "TODO",
    "role_boundary": "worker",
    "priority": 50,
    "assigned_agent": null,
    "lease_until": null
  }
]
```

Empty array `[]` when no tasks match.

## Exit codes

- `0` — success
- `2` — error