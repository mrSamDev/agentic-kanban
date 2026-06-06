# Approve Task

Approve a task that is in `IN_REVIEW` state, marking it as `DONE`. Only
applicable to tasks that were submitted for review via `complete --review`.

## Review Workflow

1. Worker completes work: `kanban task complete TASK-101 --agent worker-1 --review`
2. Task enters `IN_REVIEW` state
3. Reviewer claims: `kanban task claim-next --agent reviewer-1 --role reviewer`
4. Reviewer approves: `kanban task approve TASK-101 --agent reviewer-1`
   - Or rejects: `kanban task reject TASK-101 --agent reviewer-1 --reason "..."`

## Usage

```bash
kanban task approve TASK-101 \
  --agent reviewer-agent
```

## Flags

| Flag | Required | Description |
|---|---|---|
| `--agent` | yes | Your agent identifier |

## JSON output

Full task object with `status: "DONE"`, `assigned_agent: null`.

## Exit codes

- `0` — success, JSON on stdout
- `2` — task not found, wrong state (not IN_REVIEW), or other error