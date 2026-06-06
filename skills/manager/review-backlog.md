# Review Backlog

List all tasks visible to a manager. Apply optional filters to find specific
tasks: stalled, unassigned, or in a certain state.

## Usage

```bash
# All tasks
kanban --db "$KANBAN_DB" task search

# Filter by status
kanban --db "$KANBAN_DB" task search --status BLOCKED

# Filter by role
kanban --db "$KANBAN_DB" task search --role worker --status TODO

# Filter by agent
kanban --db "$KANBAN_DB" task search --agent alice
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