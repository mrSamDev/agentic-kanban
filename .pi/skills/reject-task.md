---
name: reject-task
description: Reject a task in IN_REVIEW state, sending it back to TODO for rework.
---

# Reject Task

Reject a task in IN_REVIEW state, sending it back to TODO for rework.
Any reviewer can reject — no prior claim needed.

Usage:

  kanban task reject TASK-101 \
    --agent reviewer-1 \
    --reason "Needs more test coverage"

Flags:
  --agent  (required) Your agent identifier
  --reason (required) Why the task was rejected

JSON output: task object with status "TODO".

Exit: 0 = success, 2 = wrong state or not found.