# Claim Review

Claim the next unclaimed reviewer task (TODO tasks tagged `role_boundary: reviewer`).

For reviewing worker-submitted work (`IN_REVIEW` status), use `approve` or `reject`
directly — no claim step needed.

## Usage

```bash
kanban task claim-next \
  --agent reviewer-agent \
  --role reviewer
```

## JSON output

Same as `claim-next-task.md`. Empty `{}` if no reviews pending.

## Exit codes

- `0` — success (task claimed OR no work)
- `2` — error