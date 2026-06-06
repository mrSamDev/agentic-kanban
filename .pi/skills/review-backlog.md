---
name: review-backlog
description: Search tasks by filters to see what's available, blocked, or done.
---

# Review Backlog

Search tasks by filters to see what's available, blocked, or done.

Usage:

  kanban task search --status TODO --role worker
  kanban task search --status BLOCKED
  kanban task search --agent my-agent

Flags:
  --status (optional) Filter by status
  --role   (optional) Filter by role boundary
  --agent  (optional) Filter by assigned agent
  --limit  (optional) Max results

JSON output: array of task objects sorted by priority.

Exit: 0 = success, 2 = error.