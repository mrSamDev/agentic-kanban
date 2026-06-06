# Claim Next Task

Claim the highest-priority unclaimed task for your role. If no work is
available (all tasks claimed or none match your role), returns empty `{}`.

Also reclaims tasks where the previous agent's lease has expired (15 minutes
without heartbeat). Leases are lazy-reclaimed — only checked when someone
calls claim-next.

## Usage

```bash
kanban --db "$KANBAN_DB" task claim-next \
  --agent my-agent-name \
  --role worker
```

## Flags

| Flag | Required | Description |
|---|---|---|
| `--agent` | yes | Your agent identifier |
| `--role` | yes | Your role (`worker`, `reviewer`, etc.) |

## JSON output (task claimed)

```json
{
  "id": "TASK-101",
  "title": "Fix auth bug",
  "status": "IN_PROGRESS",
  "role_boundary": "worker",
  "priority": 10,
  "assigned_agent": "my-agent-name",
  "lease_until": "2026-06-06T08:05:38Z"
}
```

## JSON output (no work)

```json
{}
```

## Exit codes

- `0` — success (task claimed OR no work), JSON on stdout
- `2` — error, JSON error on stderr

## Behavior

- Only tasks matching your `role_boundary` are eligible.
- Among eligible tasks, selects the lowest `priority` value (most urgent),
  then oldest `created_at` (first-in-first-out).
- Stale leases (expired `lease_until`) are reclaimed as if TODO.
- Claim is atomic: two agents calling concurrently never get the same task.
- Lease duration: 15 minutes. Use `log-progress` to renew as a heartbeat.