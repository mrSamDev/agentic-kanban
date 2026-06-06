---
name: dispatch-task
description: Create a new task in the kanban board. Tasks start as TODO and are picked up by workers via claim-next.
role: manager
type: protocol
---
# Dispatch Task

Dispatch a new task into the kanban board. The task starts as `TODO` and will be
claimed by the next available worker matching its role boundary.

## Usage

```bash
kanban task dispatch \
  --title "Brief task description" \
  --role worker \
  --priority 50
```

Priority is lower=more-urgent. Default: 100. Range typically 1-1000.

## Flags

| Flag | Required | Description |
|---|---|---|
| `--title` | yes | Short task description |
| `--role` | yes | Role boundary (`worker`, `reviewer`, etc.) |
| `--priority` | no | Urgency (lower=more urgent, default 100) |

## JSON output

```json
{
  "id": "TASK-101",
  "title": "Brief task description",
  "status": "TODO",
  "role_boundary": "worker",
  "priority": 50,
  "assigned_agent": null,
  "lease_until": null
}
```

## Exit codes

- `0` — task created, JSON on stdout
- `2` — error (title/role missing, db not found), JSON error on stderr