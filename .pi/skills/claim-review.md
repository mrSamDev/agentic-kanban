---
name: claim-review
description: Claim the next unclaimed reviewer task (TODO tasks tagged with role_boundary: reviewer).
---

# Claim Review

Claim the next unclaimed reviewer task (TODO tasks tagged with
role_boundary: reviewer).

For reviewing worker-submitted work (IN_REVIEW status), use approve
or reject directly — no claim step needed.

Usage:

  kanban task claim-next --agent reviewer-1 --role reviewer

JSON output: same as Claim Next Task. Empty {} if no work.

Exit: 0 = success or no work, 2 = error.