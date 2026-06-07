---
name: approve-plan
description: Read the approved task proposal and dispatch all tasks to the kanban board.
role: manager
type: protocol
---

# Approve Plan

Read `.kanban/tasks-proposal.md`, find checked `[x]` items, and dispatch
each one to the kanban board as a task.

## Workflow

1. Read `.kanban/tasks-proposal.md`
2. Find lines matching `[x]` (checked items)
3. For each checked line, extract:
   - **Title** — text after `[x]`, before priority
   - **Priority** — `(pN)` marker, defaults to 100
   - **Role** — `role: <name>` marker, defaults to worker
4. Dispatch each checked task using the CLI

## Usage

```bash
# For each checked item:
kanban task dispatch \
  --title "Implement user login" \
  --priority 10 \
  --role worker \
  --project default

# Approve all tasks regardless of checkboxes:
/approve-plan --all
```

## Proposal format

A `.kanban/tasks-proposal.md` file looks like:

```
# Task Proposal

1. [x] Implement user login (p10, role: worker)
   - Sign up with email, log in/out
2. [ ] Build task list view (p20, role: worker)
   - Deferred to next sprint
3. [x] Set up CI pipeline (p30, role: worker)
   - GitHub Actions, lint + test
```

The user checked 1 and 3. Dispatch those. Skip unchecked items.

## --all flag

If the user passes `--all`, dispatch every task in the proposal regardless
of checkbox state. Otherwise, only dispatch `[x]` checked items.

## Guidelines

- Dispatch all checked items, skip unchecked
- Use the priority and role from the proposal line
- Default priority: 100. Default role: worker
- Report a summary at the end: "Dispatched 3 tasks, skipped 2"

## Output

Dispatched tasks appear on the board. Run `review-backlog` to confirm.