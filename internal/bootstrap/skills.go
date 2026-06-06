package bootstrap

// Skill templates for agent harness scaffolding.

const SkillDispatchTask = `# Dispatch Task

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

const SkillViewTask = `# View Task

View full task details including notes and history. Useful when an agent
receives a task ID and needs context.

Usage:

  kanban task view TASK-101

JSON output: { task, notes[], history[] }

Exit: 0 = success, 2 = not found or error.
`

const SkillReviewBacklog = `# Review Backlog

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

const SkillClaimNextTask = `# Claim Next Task

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

const SkillLogProgress = `# Log Progress

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

const SkillBlockTask = `# Block Task

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

const SkillCompleteTask = `# Complete Task

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

const SkillClaimReview = `# Claim Review

Claim the next unclaimed reviewer task (TODO tasks tagged with
role_boundary: reviewer).

For reviewing worker-submitted work (IN_REVIEW status), use approve
or reject directly — no claim step needed.

Usage:

  kanban task claim-next --agent reviewer-1 --role reviewer

JSON output: same as Claim Next Task. Empty {} if no work.

Exit: 0 = success or no work, 2 = error.
`

const SkillApproveTask = `# Approve Task

Approve a task in IN_REVIEW state, marking it DONE. Any reviewer can
approve — no prior claim needed.

Usage:

  kanban task approve TASK-101 --agent reviewer-1

Flags:
  --agent (required) Your agent identifier

JSON output: task object with status "DONE".

Exit: 0 = success, 2 = wrong state or not found.
`

const SkillRejectTask = `# Reject Task

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