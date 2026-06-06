# Dispatch Task

Dispatch a new task into the kanban board. The task starts as `TODO` and will be
claimed by the next available worker matching its role boundary.

## Usage

```bash
# Basic task (uses project "default")
kanban task dispatch \
  --title "Brief task description" \
  --role worker \
  --priority 50

# Task with project label
kanban task dispatch \
  --title "Fix DTT eval bug" \
  --role worker \
  --project dtt-eval \
  --priority 5
```

Priority is lower=more-urgent. Default: 100. Range typically 1-1000.

## Flags

| Flag | Required | Description |
|---|---|---|
| `--title` | yes | Short task description |
| `--role` | yes | Role boundary (`worker`, `reviewer`, etc.) |
| `--project` | no | Project/scope label (default: "default") |
| `--priority` | no | Urgency (lower=more urgent, default 100) |

## JSON output

```json
{
  "id": "TASK-101",
  "title": "Brief task description",
  "status": "TODO",
  "role_boundary": "worker",
  "project": "dtt-eval",
  "priority": 50,
  "assigned_agent": null,
  "lease_until": null
}
```

## Exit codes

- `0` — task created, JSON on stdout
- `2` — error (title/role missing, db not found), JSON error on stderr

## Batch Operations

To update multiple tasks at once:

```bash
# Re-prioritize 15 old tasks
kanban task batch set-priority \
  --ids TASK-1,TASK-2,TASK-3 \
  --priority 999

# Move tasks to archive project
kanban task batch set-project \
  --ids TASK-1,TASK-2,TASK-3 \
  --project archive
```