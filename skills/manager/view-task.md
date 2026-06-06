# View Task

Get full task details including all notes and history events. Use this when you
receive only a task ID and need full context: description, current status,
lease info, notes from prior agents, and the complete event log.

## Usage

```bash
kanban task view TASK-101
```

## JSON output

```json
{
  "task": {
    "id": "TASK-101",
    "title": "Fix auth bug",
    "status": "IN_PROGRESS",
    "role_boundary": "worker",
    "priority": 10,
    "assigned_agent": "alice",
    "lease_until": "2026-06-06T08:05:38Z"
  },
  "notes": [
    {
      "id": 1,
      "task_id": "TASK-101",
      "author": "alice",
      "note_type": "PROGRESS",
      "content": "Found the root cause in auth.go:42",
      "created_at": "2026-06-06T07:51:43Z"
    }
  ],
  "history": [
    {"id":1, "task_id":"TASK-101", "agent":"system", "action":"DISPATCH"},
    {"id":2, "task_id":"TASK-101", "agent":"alice",  "action":"CLAIM"},
    {"id":3, "task_id":"TASK-101", "agent":"alice",  "action":"PROGRESS"}
  ]
}
```

## Exit codes

- `0` — success, JSON on stdout
- `2` — task not found