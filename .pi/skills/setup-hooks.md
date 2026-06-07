---
name: setup-hooks
description: Set up kanban event hooks for notifications, metrics, logging, or custom automation
role: manager
type: protocol
---
# Setup Hooks

Set up event hooks that fire on kanban state transitions. Hooks let you
integrate Slack, Discord, metrics, logging, or any custom automation.

## Hook directory

Place executables in `.kanban/hooks/` named after the event. The event name
uses hyphens (`.` replaced by `-`):

```
.kanban/hooks/
├── task-created          ← single hook
├── task-completed        ← single hook
└── task-completed.d/     ← multiple hooks, all receive same payload
    ├── slack
    ├── metrics
    └── dashboard
```

## Rules

- Hook must be executable (`chmod +x`).
- Receives event JSON on stdin: `{"event": "task.created", "payload": {...}}`.
- Non-zero exit is logged to stderr but does not fail the operation.
- Missing hook or missing `.d/` directory is silently ignored.
- `.d/` entries run concurrently after the single-file hook.
- Each hook has a 30-second timeout.

## Events

| Event | When |
|---|---|
| `task.created` | Task dispatched |
| `task.claimed` | Agent claims a task |
| `task.progress` | Progress logged |
| `task.completed` | Task finished |
| `task.submitted_for_review` | Submitted for review |
| `task.blocked` | Blocked |
| `review.approved` | Approved |
| `review.rejected` | Rejected |
| `task.priority_updated` | Batch priority update |
| `task.project_updated` | Batch project update |

## JSON payload

```json
{
  "event": "task.created",
  "payload": {
    "id": "TASK-101",
    "title": "Set up auth",
    "status": "TODO",
    "role_boundary": "worker",
    "priority": 10,
    "assigned_agent": null,
    "lease_until": null,
    "project": "default"
  }
}
```

## Example: Slack notification hook

```bash
#!/bin/bash
# .kanban/hooks/task-completed.d/slack

set -e

eval "$(jq -r '@sh "event=\(.event) id=\(.payload.id) title=\(.payload.title) status=\(.payload.status)"')"

if [ "$event" != "task.completed" ]; then
  exit 0
fi

WEBHOOK_URL="${SLACK_WEBHOOK_URL:-}"
if [ -z "$WEBHOOK_URL" ]; then
  echo "SLACK_WEBHOOK_URL not set" >&2
  exit 0
fi

curl -sf -X POST "$WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg id "$id" --arg title "$title" --arg status "$status" \
    '{text: "Task \($id): \($title) → \($status)"}')"
```

## Example: log all events

```bash
#!/bin/bash
# .kanban/hooks/log-events

mkdir -p /tmp/kanban-hooks
cat >> "/tmp/kanban-hooks/events.log"
```

## Example: format hook files

`kanban init` scaffold creates the `.kanban/` directory. Hooks are manual.
Create them with your editor or generate from a template:

```bash
mkdir -p .kanban/hooks
cat > .kanban/hooks/task-created << 'SCRIPT'
#!/bin/bash
eval "$(jq -r '@sh "title=\(.payload.title) id=\(.payload.id)"')"
echo "[hook] task $id created: $title"
SCRIPT
chmod +x .kanban/hooks/task-created
```

## Shell

Always set executable permission:

```bash
chmod +x .kanban/hooks/task-created
chmod -R +x .kanban/hooks/task-completed.d/
```