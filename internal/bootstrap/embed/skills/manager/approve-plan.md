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

## Manager Mode

Two execution modes, controlled by the manager agent config:

### Serial mode (default)

The manager executes tasks itself, one at a time. Suitable for small projects
or sequential workflows where parallel execution adds overhead.

```
Plan → dispatch → claim → execute → complete → repeat
```

### Parallel mode

The manager delegates to worker subagents via `claim-next --count N`.
The manager NEVER executes tasks — only plans and delegates.

```
Plan → dispatch → claim-next --count N → spawn N subagents → poll → review
```

In parallel mode:
1. Dispatch all proposal tasks to the board
2. Use `claim-next --count N` to claim up to N tasks atomically
3. Spawn a subagent per claimed task
4. Poll until all subagents return or lease expires
5. Submit completed tasks for review

## Output

Dispatched tasks appear on the board. Run `review-backlog` to confirm.

## After Dispatch — Auto-Execute Loop

After dispatching all tasks, do NOT stop. Do NOT explain. Do NOT ask confirm.

Immediately claim the highest-priority task and spawn a subagent to work it.

Loop:

```
dispatch → claim → subagent → work → complete → review-backlog → repeat
```

Until the board has zero TODO tasks.

### Steps

1. Run `claim-next-task` to grab the highest-priority unclaimed task
2. Spawn a `subagent` to execute the work (pass the task ID and details)
3. When subagent returns, call `complete-task --review` to submit
4. Run `review-backlog` to check if more TODO tasks remain
5. If yes, go to step 1. If no, proceed to review phase.

### Review Phase

After all tasks are built and submitted for review, approve them:

1. Poll `review-backlog --status IN_REVIEW`
2. For each task: `kanban task approve TASK-N --agent <your-agent>`
3. Repeat until 0 IN_REVIEW remain (or 5 minutes timeout)

Set `KANBAN_ALLOW_SELF_REVIEW=true` in the environment before the
review phase. The orchestrator dispatched the work, verified subagent
output, and is acting as the reviewer — self-review is intentional here.

### Subagent delegation

For long-running subagent work (>15 min), transfer the claim so the
subagent owns it and can complete independently:

```
kanban task claim TASK-5 --agent <orchestrator> --transfer --to <subagent>
```

The subagent can then extend its own lease, log progress, and
call `complete-task` directly. If the subagent crashes, lease expiry
reclaims the task — same crash recovery as any worker.

### Rules

- Do NOT narrate the loop. Just run it.
- Do NOT ask for confirmation between iterations.
- If `claim-next-task` returns empty `{}`, the board has no TODO. Start review phase.
- If a subagent returns an error, log it and try the next task. Do not block the whole queue.