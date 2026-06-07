---
name: review-gate-skill
description: Enforce cross-agent review — the reviewer must differ from the worker.
role: manager
type: protocol
---
# Review Gate

Cross-agent review is enforced by default. A task submitted for review
(via `--review` flag) must be approved by a different agent than the one
who worked on it.

This prevents self-approval and ensures at least two agents inspect every
review-submitted task.

## Approval

```bash
kanban task approve TASK-101 \
  --agent reviewer-agent-name
```

The approving agent must differ from the task's last worker. If the task
was assigned to "alice" and "alice" tries to approve, the gate rejects:

```json
{"error": "cannot self-review — use a different agent to approve"}
```

## Single-agent mode

If you are running solo (no reviewer agent), set this environment variable
to bypass the gate:

```bash
export KANBAN_ALLOW_SELF_REVIEW=true
```

## Gate logic

- `checkSelfReview` compares `agent` against the task's `assigned_agent` in
  the history entry for the most recent `REVIEW` action
- Mismatch or single-agent mode → allowed
- Same agent without env var → `ErrSelfReview`

## Skills flow

1. Worker completes task with `--review` → status becomes `IN_REVIEW`
2. Reviewer agent calls `kanban task approve` → cross-agent check runs
3. If approved: status → `DONE`
4. If rejected: status → `TODO`, worker can fix and resubmit
