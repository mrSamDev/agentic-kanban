# Claim Next Task

Claim the highest-priority unclaimed task for your role. Returns empty {}
if no work is available.

Also reclaims tasks where the previous agent's lease expired (15 min
without heartbeat). Leases are lazy-reclaimed on claim-next.

Usage:

  kanban task claim-next --agent my-agent --role worker

Flags:
  --agent (required) Your agent identifier
  --role  (required) worker, reviewer, etc.

JSON output (task claimed): { id, title, status: "IN_PROGRESS", ... }
JSON output (no work): {}

Exit: 0 = success or no work, 2 = error.
