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
└── agents/
    ├── manager/
    │   └── skills/   (dispatch-task, review-backlog, view-task)
    ├── worker/
    │   └── skills/   (claim-next-task, log-progress, complete-task, block-task)
    └── reviewer/
        └── skills/   (claim-review, approve-task, reject-task)
```

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

## Review all auth changes 🔥
```

Priority hints: `[p1]`-`[p999]` or `🔥` (priority 1).

## Running agents

Each agent reads its skills and runs the kanban CLI.

### Manager (dispatches work)

In `.pi/agents/manager/prompt.md`:

```markdown
You are a manager agent. Use the kanban tool to dispatch work.

Available skills:
- dispatch-task: create new tasks
- review-backlog: check task status
- view-task: inspect task details

Read each skill file before calling the command.
```

Run: `pi run manager`

### Worker (claims and executes)

In `.pi/agents/worker/prompt.md`:

```markdown
You are a worker agent. Claim and complete tasks from the kanban board.

Available skills:
- claim-next-task: claim highest-priority work
- log-progress: report progress (renews lease)
- complete-task: mark done (or submit for review)
- block-task: mark blocked with reason

Loop: claim → work → log progress → complete.
```

Run: `pi run worker`

### Reviewer (approves or rejects)

In `.pi/agents/reviewer/prompt.md`:

```markdown
You are a reviewer agent. Review and approve/reject completed tasks.

Available skills:
- approve-task: approve IN_REVIEW → DONE
- reject-task: reject IN_REVIEW → TODO
- claim-review: claim reviewer-only TODO tasks
- view-task: inspect task details
```

Run: `pi run reviewer`

## Full workflow

```bash
# 1. Manager dispatches tasks
pi run manager
# Manager reads skills/dispatch-task.md, runs:
#   kanban task dispatch --title "Refactor auth" --role worker --priority 1

# 2. Worker picks up and executes
pi run worker
# Reads skills/claim-next-task.md, runs:
#   kanban task claim-next --agent worker-1 --role worker
# Works, logs progress:
#   kanban task log-progress TASK-1 --agent worker-1 --note "Extracted middleware" --type PROGRESS
# Submits for review:
#   kanban task complete TASK-1 --agent worker-1 --review

# 3. Reviewer reviews
pi run reviewer
# Reads skills/approve-task.md, runs:
#   kanban task approve TASK-1 --agent reviewer-1
```

## Crash recovery

If a worker crashes mid-task:

1. Lease expires after 15 minutes (no `log-progress` heartbeat).
2. Next worker calling `claim-next` automatically reclaims the stale task.
3. Work resumes from the last logged progress note.

No daemon, no monitoring, no infrastructure. Just a `.db` file.