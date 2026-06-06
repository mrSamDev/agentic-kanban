---
name: complete-task
description: Mark a task as done, optionally submitting for review instead.
---

# Complete Task

Mark a task as done. If the task needs review, use --review to submit
for review instead.

Usage (direct complete):

  kanban task complete TASK-101 --agent my-agent

Usage (submit for review):

  kanban task complete TASK-101 --agent my-agent --review

Flags:
  --agent  (required) Your agent identifier
  --review (optional) Submit for review instead of completing

JSON output: task object with status "DONE" or "IN_REVIEW".

Exit: 0 = success, 2 = not assigned or error.
