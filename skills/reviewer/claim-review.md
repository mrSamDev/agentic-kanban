# Claim Review

Claim the next task in `IN_REVIEW` state for review. Works identically to
`claim-next` but targets tasks submitted for review (`IN_REVIEW` status).

Note: `claim-next --role reviewer` picks up both `IN_REVIEW` tasks submitted
by workers AND any unclaimed TODO tasks tagged with `role_boundary: reviewer`.
This is correct behavior — reviewers can claim either type.

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