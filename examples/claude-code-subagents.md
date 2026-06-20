# Integration: Claude Code subagents

Coordinating Claude Code subagents via taskbind. Uses Claude's native
subagent/agent delegation.

## Setup

```bash
cd my-project

# Install taskbind
curl -sfL https://raw.githubusercontent.com/mrSamDev/taskbind/main/install.sh | sh

# Init with claude harness
taskbind init --harness claude --plan plan.md
```

Creates:

```
.taskbind/
└── taskbind.db
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
You are a worker agent. You coordinate work through a taskbind board.

Rules:
1. Claim one task at a time via: taskbind task claim-next --agent worker-1 --role worker
2. If no work (output is {}), report idle.
3. Work on the task, log progress: taskbind task log-progress TASK-N --agent worker-1 --note "..."
4. When done: taskbind task complete TASK-N --agent worker-1
5. If blocked: taskbind task block TASK-N --agent worker-1 --reason "..."

Read the skill files in .claude/agents/worker/skills/ for exact command syntax.
```

### Reviewer agent (`reviewer.md`)

```markdown
You are a reviewer agent. Review completed work via the taskbind board.

Rules:
1. Find IN_REVIEW tasks: taskbind task search --status IN_REVIEW
2. View task details: taskbind task view TASK-N
3. If acceptable: taskbind task approve TASK-N --agent reviewer-1
4. If needs rework: taskbind task reject TASK-N --agent reviewer-1 --reason "..."

No need to claim IN_REVIEW tasks — approve/reject directly.
```

### Manager agent (`manager.md`)

```markdown
You are a manager agent. You plan and dispatch work.

Rules:
1. Create tasks: taskbind task dispatch --title "..." --role worker --priority N
2. Check backlog: taskbind task search --status TODO
3. Check blockers: taskbind task search --status BLOCKED
4. Inspect tasks: taskbind task view TASK-N
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