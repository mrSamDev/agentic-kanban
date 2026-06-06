---
name: dispatch-task
description: Create a new task in the kanban board. Tasks start as TODO and are picked up by workers via claim-next.
---

# Dispatch Task

Create a new task in the kanban board. Tasks start as TODO and are picked
up by workers via claim-next.

Usage:

  kanban task dispatch --title "Task title" --role worker --priority 10

Flags:
  --title    (required) Task title
  --role     (required) Role boundary (worker, reviewer, etc.)
  --priority (optional) Lower = more urgent (default 100)

JSON output: task object with status "TODO".

Exit: 0 = success, 2 = error.