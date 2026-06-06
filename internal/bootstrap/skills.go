package bootstrap

const SkillDispatchTask = `---
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
`

const SkillViewTask = `---
name: view-task
description: View full task details including notes and history. Useful when an agent receives a task ID and needs context.
---

# View Task

View full task details including notes and history. Useful when an agent
receives a task ID and needs context.

Usage:

  kanban task view TASK-101

JSON output: { task, notes[], history[] }

Exit: 0 = success, 2 = not found or error.
`

const SkillReviewBacklog = `---
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
`

const SkillClaimNextTask = `---
name: claim-next-task
description: Claim the highest-priority unclaimed task for a role, with lease reclamation on stale tasks.
---

# Claim Next Task

Claim the highest-priority unclaimed task for your role. Returns empty {}
if no work is available.

Also reclaims tasks where the previous agent's lease expired (15 min
without heartbeat). Leases are lazy-reclaimed on claim-next.

Usage:

  kanban task claim-next --agent my-agent --role worker

Flags:
  --agent (required) Your agent identifier
  --role  (required) worker, reviewer, etc.

JSON output (task claimed): { id, title, status: "IN_PROGRESS", ... }
JSON output (no work): {}

Exit: 0 = success or no work, 2 = error.
`

const SkillLogProgress = `---
name: log-progress
description: Log a progress note and renew your lease (heartbeat) to prevent lease expiry.
---

# Log Progress

Log a progress note and renew your lease (heartbeat). Call this periodically
while working to prevent lease expiry.

Usage:

  kanban task log-progress TASK-101 \
    --agent my-agent \
    --note "Implemented the auth handler" \
    --type PROGRESS

Flags:
  --agent  (required) Your agent identifier
  --note   (required) Progress description
  --type   (optional) PROGRESS, ERROR, or DECISION

Lease renewal: this command extends lease to +15 min from now.

Exit: 0 = success, 2 = not assigned or not found.
`

const SkillBlockTask = `---
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
`

const SkillCompleteTask = `---
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
`

const SkillClaimReview = `---
name: claim-review
description: "Claim the next unclaimed reviewer task (TODO tasks tagged with role_boundary: reviewer)."
---

# Claim Review

Claim the next unclaimed reviewer task (TODO tasks tagged with
role_boundary: reviewer).

For reviewing worker-submitted work (IN_REVIEW status), use approve
or reject directly — no claim step needed.

Usage:

  kanban task claim-next --agent reviewer-1 --role reviewer

JSON output: same as Claim Next Task. Empty {} if no work.

Exit: 0 = success or no work, 2 = error.
`

const SkillApproveTask = `---
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
`

const SkillRejectTask = `---
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
`

const SkillDispatchPlan = `---
name: dispatch-plan
description: Read a plan file (spec, roadmap, product brief), extract tasks using your own intelligence, write a review file for user approval.
---

# Dispatch Plan (LLM-Driven)

You are the product owner. Read the plan file, extract meaningful tasks
using your own understanding of the document, and write a review file
so the user can approve before tasks are dispatched.

## Workflow

1. Read the plan file (plan.md, spec.md, PRD, etc.)
2. Analyze it — identify every actionable work item the plan describes:
   - Features, components, pages, API endpoints, DB changes
   - Bug fixes, tests, deployment steps
   - Research spikes, documentation tasks
3. For each task determine:
   - Title (clear, actionable)
   - Priority (1-100, lower = more urgent)
   - Role (worker, reviewer, etc.) — defaults to worker
4. Write a proposal file at .kanban/tasks-proposal.md in this format:

       # Task Proposal

       1. [ ] Implement user login (p10, role: worker)
           - Sign up with email, log in/out
       2. [ ] Build task list view (p20, role: worker)
           - Filter by all/completed/pending

5. Present the proposal to the user: I've created a task proposal
   at .kanban/tasks-proposal.md. Review and run approve-plan to dispatch.

## Guidelines

- Break large features into discrete tasks (each should be 1-2 days work)
- Use your judgment — don't parse mechanically, understand the plan
- Prioritize core features first (p1-p20), enhancements later (p50+)
- Add context under each task with a bullet-point description
- If the plan has a timeline/sprint section, group related tasks

## Output

File: .kanban/tasks-proposal.md
The user reviews this file then runs approve-plan to dispatch.
`

const SkillApprovePlan = `---
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
   2. [ ] Nice-to-have: dark mode (p60, role: worker)

Priority is extracted from (pN) pattern, role from (role: X) pattern.
Defaults: priority=100, role=worker.

## Important

Do NOT parse — use dispatch_task tool for each task.
One dispatch_task call per task. Report summary at end.
`