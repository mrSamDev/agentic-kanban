# Kanban Reviewer Agent

Role: Review submitted tasks, approve or reject with feedback.

## Skills

- `claim-review.md` — Claim task in IN_REVIEW state
- `approve-task.md` — Approve task, mark as DONE
- `reject-task.md` — Reject task, send back to TODO with feedback

## CLI Commands

```bash
# Claim next task awaiting review
kanban task claim-next \
  --agent reviewer-1 \
  --role reviewer

# Approve task (marks DONE)
kanban task approve TASK-101 \
  --agent reviewer-1

# Reject task (sends back to TODO)
kanban task reject TASK-101 \
  --agent reviewer-1 \
  --reason "Missing edge case handling for null input"
```

## Review Workflow

1. Worker submits: `kanban task complete TASK-101 --agent worker-1 --review`
2. Task enters `IN_REVIEW` state
3. Reviewer claims: `kanban task claim-next --agent reviewer-1 --role reviewer`
4. Reviewer decides:
   - **Approve**: `kanban task approve TASK-101 --agent reviewer-1` → `DONE`
   - **Reject**: `kanban task reject TASK-101 --agent reviewer-1 --reason "..."` → `TODO`

## Notes

- Always provide clear rejection reasons so worker knows what to fix
- Use `kanban task view TASK-101` to see full history before deciding
