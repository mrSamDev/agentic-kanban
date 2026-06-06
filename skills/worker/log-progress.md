# Log Progress

Report progress on your current task. This appends a note and **renews your
lease** (heartbeat) — extend your 15-minute claim window.

Call this frequently (every few minutes) while actively working to prevent
other agents from reclaiming your task.

## Usage

```bash
kanban --db "$KANBAN_DB" task log-progress TASK-101 \
  --agent my-agent-name \
  --note "Found the root cause in auth.go:42" \
  --type PROGRESS
```

## Flags

| Flag | Required | Description |
|---|---|---|
| `--agent` | yes | Your agent identifier |
| `--note` | yes | Progress description |
| `--type` | no | Note type: `PROGRESS`, `ERROR`, `DECISION` |

## JSON output

Full task object with updated `lease_until` timestamp.

## Exit codes

- `0` — success, JSON on stdout
- `2` — task not found, not assigned to you, or other error