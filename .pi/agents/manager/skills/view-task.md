# View Task

View full task details including notes and history. Useful when an agent
receives a task ID and needs context.

Usage:

  kanban task view TASK-101

JSON output: { task, notes[], history[] }

Exit: 0 = success, 2 = not found or error.
