# Integration: pi subagents

Three-coder setup using pi subagents and agentic-kanban.

## Setup

```bash
cd my-project

# Install kanban
curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-kanban/main/install.sh | sh

# Init project with pi harness and seed plan
kanban init --harness pi --plan plan.md
```

This creates:

```
.kanban/
└── kanban.db
.pi/
├── agents/
│   ├── manager.md             # Agent definition with tool references
│   ├── worker.md
│   └── reviewer.md
└── skills/
    ├── dispatch-task.md       # Skill docs for bash fallback
    ├── review-backlog.md
    ├── view-task.md
    ├── claim-next-task.md
    ├── log-progress.md
    ├── complete-task.md
    ├── block-task.md
    ├── claim-review.md
    ├── approve-task.md
    └── reject-task.md
```

Then install the pi integration package:

```bash
pi install agent-kanban-pi
```

This registers 12 custom MCP tools:
`claim_next_task`, `batch_claim_task`, `batch_complete_task`, `claim_task`,
`dispatch_task`, `log_progress`, `block_task`, `complete_task`,
`approve_task`, `reject_task`, `review_backlog`, `view_task`.

The footer shows live task counts by status. Type `/kanban` for a board overview.

## How it works

When you open `pi` in a project initialized with `kanban init --harness pi`:

1. Pi loads the extension → kanban tools appear in the tool list
2. The LLM uses the typed tools instead of bash commands
3. Tools auto-detect the `.kanban/kanban.db` path (walk up from cwd)
4. Agent definitions in `.pi/agents/` provide per-role prompts
5. Skill files in `.pi/skills/` serve as reference docs for bash fallback

## Plan file example

Save as `plan.md`:

```markdown
## Refactor auth module [p1]
- Extract middleware to separate package
- Add token refresh endpoint
- Write integration tests

## Add CI pipeline
- Set up GitHub Actions
- Add linter step
- Configure test runner

## Review all auth changes [p1]
```

Priority hints: `[p1]`-`[p999]`.

## Running agents

Each agent is a flat `.md` file in `.pi/agents/` with YAML frontmatter.
Run with `pi run <name>`.

### Manager (dispatches work)

`.pi/agents/manager.md`:

```markdown
---
name: manager
description: Kanban manager agent that dispatches work and reviews the backlog
tools: read, bash, write, edit
model: claude-sonnet-4-5
---

You are a kanban manager agent. Manage the task board using the registered kanban tools.

Available tools (prefer these over raw bash):
- dispatch_task: create new tasks for workers or reviewers
- review_backlog: search tasks by status, role, agent, or project
- view_task: inspect full task details including notes and history

Workflow:
1. Review the backlog to see what's pending
2. Dispatch tasks to workers or reviewers with appropriate priority
3. Monitor progress by checking task status
```

Run: `pi run manager`

### Worker (claims and executes)

`.pi/agents/worker.md`:

```markdown
---
name: worker
description: Kanban worker agent that claims and completes tasks
tools: read, bash, write, edit
model: claude-sonnet-4-5
---

You are a kanban worker agent. Claim and complete tasks from the kanban board.

Available tools (prefer these over raw bash):
- claim_next_task: claim the highest-priority unclaimed task
- log_progress: report progress and renew lease (heartbeat)
- block_task: mark a task as blocked with explanation
- complete_task: mark done or submit for review

Workflow:
1. Claim the next available task
2. Work on the task, logging progress periodically
3. Submit for review or mark complete
4. If blocked, mark with reason
```

Run: `pi run worker`

### Reviewer (approves or rejects)

`.pi/agents/reviewer.md`:

```markdown
---
name: reviewer
description: Kanban reviewer agent that approves or rejects completed tasks
tools: read, bash, write, edit
model: claude-sonnet-4-5
---

You are a kanban reviewer agent. Review and approve/reject completed tasks.

Available tools (prefer these over raw bash):
- approve_task: approve IN_REVIEW tasks → DONE
- reject_task: reject IN_REVIEW tasks → TODO for rework
- view_task: inspect full task details before review
- review_backlog: search for IN_REVIEW tasks

Workflow:
1. Check for tasks in IN_REVIEW state using review_backlog
2. View task details
3. Approve or reject with clear reason
```

Run: `pi run reviewer`

## Manual workflow (without agents)

If you just want to use the kanban CLI directly:

```bash
# Create tasks
kanban task dispatch --title "Refactor auth" --role worker --priority 1

# Claim and work
kanban task claim-next --agent my-agent --role worker
kanban task log-progress TASK-1 --agent my-agent --note "Working" --type PROGRESS
kanban task complete TASK-1 --agent my-agent --review

# Review
kanban task approve TASK-1 --agent reviewer-1
```

## Crash recovery

If a worker crashes mid-task:

1. Lease expires after 15 minutes (no `log-progress` heartbeat).
2. Next worker calling `claim_next_task` (or `kanban task claim-next`) automatically reclaims the stale task.
3. Work resumes from the last logged progress note.

No daemon, no monitoring, no infrastructure. Just a `.db` file.