---
name: approve-plan
description: Read the approved task proposal at .kanban/tasks-proposal.md and dispatch all tasks to the kanban board.
---

# Approve Plan

Read .kanban/tasks-proposal.md, parse the approved tasks, and dispatch
each to the kanban board as a TODO item. Run this after the user has
reviewed and approved the proposal written by dispatch-plan.

## Workflow

1. Read .kanban/tasks-proposal.md
2. For each checked [x] item, dispatch a task:

       kanban task dispatch --title "Implement user login" --priority 10 --role worker

3. Skip unchecked items: [ ]
4. Report results: how many dispatched, how many skipped

## Proposal format expected

Each approved task is a markdown item with [x]:

   1. [x] Implement user login (p10, role: worker)
       - Sign up with email, log in/out
   2. [ ] Nice-to-have: dark mode toggle (p60, role: worker)

Priority is extracted from (pN) pattern, role from (role: X) pattern.
Defaults: priority=100, role=worker.

## Important

Do NOT parse — use dispatch_task tool for each task.
One dispatch_task call per task. Report summary at end.