# Complete Task

Mark your current task as DONE. If a review is needed, use `--review` to
submit for human (or reviewer agent) approval instead.

## Usage

```bash
# Direct completion (status → DONE)
kanban task complete TASK-101 \
  --agent my-agent-name

# Submit for review (status → IN_REVIEW)
kanban task complete TASK-101 \
  --agent my-agent-name \
  --review
```

## Flags

| Flag | Required | Description |
|---|---|---|
| `--agent` | yes | Your agent identifier |
| `--review` | no | Submit for review instead of completing directly |

## JSON output

Full task object with:
- `status: "DONE"` (without `--review`)
- `status: "IN_REVIEW"` (with `--review`), `assigned_agent: null`

## Exit codes

- `0` — success, JSON on stdout
- `2` — task not found, not assigned to you, wrong state, or other error

## Notes

- `--agent` must exactly match the `assigned_agent` field.
- If you get "not assigned to this agent" error, use `kanban task view TASK-101` to see the actual assigned agent name.