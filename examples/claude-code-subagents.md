# Integration: Claude Code subagents

Coordinating Claude Code subagents via agentic-kanban. Uses Claude's native
subagent/agent delegation.

## Setup

```bash
cd my-project

# Install kanban
curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-kanban/main/install.sh | sh

# Init with claude harness
kanban init --harness claude --plan plan.md
```

Creates:

```
.kanban/
└── kanban.db
.claude/
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

## Add CI pipeline
- Set up GitHub Actions
- Add linter step
```

## Agent prompts

### Worker agent (`worker.md`)

```markdown
You are a worker agent. You coordinate work through a kanban board.

Rules:
1. Claim one task at a time via: kanban task claim-next --agent worker-1 --role worker
2. If no work (output is {}), report idle.
3. Work on the task, log progress: kanban task log-progress TASK-N --agent worker-1 --note "..."
4. When done: kanban task complete TASK-N --agent worker-1
5. If blocked: kanban task block TASK-N --agent worker-1 --reason "..."

Read the skill files in .claude/agents/worker/skills/ for exact command syntax.
```

### Reviewer agent (`reviewer.md`)

```markdown
You are a reviewer agent. Review completed work via the kanban board.

Rules:
1. Find IN_REVIEW tasks: kanban task search --status IN_REVIEW
2. View task details: kanban task view TASK-N
3. If acceptable: kanban task approve TASK-N --agent reviewer-1
4. If needs rework: kanban task reject TASK-N --agent reviewer-1 --reason "..."

No need to claim IN_REVIEW tasks — approve/reject directly.
```

### Manager agent (`manager.md`)

```markdown
You are a manager agent. You plan and dispatch work.

Rules:
1. Create tasks: kanban task dispatch --title "..." --role worker --priority N
2. Check backlog: kanban task search --status TODO
3. Check blockers: kanban task search --status BLOCKED
4. Inspect tasks: kanban task view TASK-N
```

## Running with Claude Code

```bash
# Dispatch work (manager role)
claude -p "$(cat manager.md)" -f

# Run a worker subagent
claude -p "$(cat worker.md)" -f --allowedTools "bash"

# Run reviewer
claude -p "$(cat reviewer.md)" -f --allowedTools "bash"
```

Or use Claude Code subagents inside a session:

```bash
# Inside Claude Code, delegate to subagents
claude dev --agent worker -p "$(cat worker.md)" --allowedTools "bash"
claude dev --agent reviewer -p "$(cat reviewer.md)" --allowedTools "bash"
```

## Recommended workflow

```
1. Manager creates tasks via dispatch
2. N workers claim and execute in parallel
3. Workers complete --review when done
4. Reviewer approves/rejects
5. Rejected tasks go back to TODO, re-claimed by next worker
```

Crash recovery: stale leases (no heartbeat for 15 min) auto-reclaim on next `claim-next`.