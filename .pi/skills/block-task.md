---
name: block-task
description: Mark a task as blocked with an explanation and clear the lease so other agents know the task is stuck.
---

# Block Task

Mark a task as blocked with an explanation. Clears your lease so other
agents know the task is stuck.

Usage:

  kanban task block TASK-101 \
    --agent my-agent \
    --reason "Waiting on API credentials from ops"

Flags:
  --agent  (required) Your agent identifier
  --reason (required) Why the task is blocked

JSON output: task object with status "BLOCKED".

Exit: 0 = success, 2 = not assigned or not found.