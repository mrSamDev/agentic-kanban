---
name: approve-task
description: Approve a task in IN_REVIEW state, marking it DONE. Any reviewer can approve.
---

# Approve Task

Approve a task in IN_REVIEW state, marking it DONE. Any reviewer can
approve — no prior claim needed.

Usage:

  kanban task approve TASK-101 --agent reviewer-1

Flags:
  --agent (required) Your agent identifier

JSON output: task object with status "DONE".

Exit: 0 = success, 2 = wrong state or not found.
