# agentic-kanban

> **Alpha.** Things may break. You've been warned.

Coordination protocol for AI agents. SQLite-backed. Agent-agnostic.

No server, no daemon, no queues. Just a shared database file agents use to claim, track, review, and finish work. Single Go binary plus SQLite.

```bash
curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-kanban/main/install.sh | sh
```

> The curl pipe install works fine for trusted environments. For production, grab the binary from releases and verify the checksum.

## Why

Started with a markdown file. Sprint 1, task 1.1, task 1.2. Agents kept overwriting each other's updates, forgetting to mark things done, or picking up work already claimed. The file became noise fast.

The answer was a database. Every state change is a transaction, so two agents can't claim the same task. If one crashes, its lease expires and another picks it up, same as Rust's ownership model: one owner at a time, released when the owner disappears.

The `.db` file is the only coordination point.

## When it fits

**Use this when:** multiple AI agents on the same machine or a shared filesystem need durable coordination without Redis or Postgres or message queues. You also want crash recovery and task ownership. Tested with 3 to 10 concurrent agents, and up to 50.

**Skip it when:** agents run across untrusted networks, you need real-time push notifications, or you need thousands of concurrent workers. Those cases want something like Temporal, Celery, or Kafka.

## Quick start

```bash
curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-kanban/main/install.sh | sh
kanban init --harness pi

# Product owner workflow: an LLM reads your spec or roadmap, writes a task
# proposal, you approve, then it dispatches. See the dispatch-plan and
# approve-plan skills under embed/skills/manager/.

# For simple plans, the built-in parser works too.
kanban init --harness pi --plan plan.md

kanban --debug task dispatch --title "Set up auth" --role worker --priority 10
kanban task claim-next --agent my-agent --role worker
kanban task log-progress TASK-1 --agent my-agent --note "Working"
kanban task complete TASK-1 --agent my-agent
```

## Commands

A short table since these are reference docs:

| Command | Who | What |
|---|---|---|
| `task dispatch --title --role` | anyone | Create a task as TODO |
| `task claim-next --agent --role` | worker, reviewer | Atomically grab top task |
| `task log-progress <id> --agent --note` | worker | Log progress, renew lease |
| `task block <id> --agent --reason` | worker | Mark blocked, drop lease |
| `task complete <id> --agent --review` | worker | Mark done or submit for review |
| `task view <id>` | anyone | Full details plus notes and history |
| `task search --status --role --agent` | manager | Filter the board |
| `task approve <id> --agent` | reviewer | IN_REVIEW to DONE |
| `task reject <id> --agent --reason` | reviewer | IN_REVIEW to TODO |
| `batch set-priority --ids --priority` | manager | Bulk priority update |
| `batch set-project --ids --project` | manager | Bulk project label |
| `prune --before --dry-run` | ops | Clean old events and notes |
| `init --harness --plan --dir` | setup | Scaffold DB and agent files |

## Workflow

```text
TODO ── claim-next ──> IN_PROGRESS ── complete --review ──> IN_REVIEW ── approve ──> DONE
                            │                                       │
                            │ block                                 │ reject
                            ▼                                       ▼
                         BLOCKED                                  TODO
```

Reviewers don't need to claim IN_REVIEW tasks. They just approve or reject.

## Skills — the agent runtime

Agents don't ship with kanban knowledge. Skills teach them. Each role gets its own set of skill files — the agent reads its role's skills and learns the protocol.

### Coordination skills (shipped)

These are the **protocol skills** — they teach coordination, not software engineering. They're the moat. Different agents (Claude Code, PI, Codex, Gemini) all read the same skill files. The agents change. The protocol stays.

| Role | Skills | What the agent learns |
|---|---|---|
| `manager/` | dispatch-task, dispatch-plan, approve-plan, review-backlog, view-task | Plan work, dispatch tasks, review progress |
| `worker/` | claim-next-task, log-progress, complete-task, block-task | Claim tasks, report progress, finish work |
| `reviewer/` | claim-review, approve-task, reject-task | Review submissions, approve or reject |

```text
Kanban binary = the protocol engine
Kanban skills = the agent runtime (one per role)
Agent runtime  = Claude Code, PI, Codex, Gemini, ...
```

### Task skills (bring your own)

You can add custom task skills alongside protocol skills. These teach an agent *how to do the work*, not *how to coordinate*. For example `skills/worker/deploy-to-prod.md` or `skills/manager/sprint-review.md`. Protocol skills handle coordination; task skills handle domain logic.

Source: [internal/bootstrap/embed/skills/](internal/bootstrap/embed/skills/)

## Architecture

```
Manager                    Workers                    Reviewers
  │                           │                           │
  ├── dispatch tasks ────────>│                           │
  │                           ├── claim-next              │
  │                           ├── log-progress            │
  │                           ├── complete --review ─────>│
  │                           │              ├── approve  │
  │                           │              ├── reject   │
  │<── search --status BLOCKED│                           │
  └── unblock or reassign ───>│                           │
```

## Project structure

```
cmd/kanban/main.go              CLI entrypoint
internal/
  bootstrap/                    Init, plan parsing, agent and skill templates
  bootstrap/kanban_extension.go Pi extension (TypeScript template)
  storage/                      SQLite connection, schema, migration
  task/                         Models, queries, service logic
embed/skills/               Embedded skill templates (canonical source)
  manager/                 dispatch-task, dispatch-plan, approve-plan, review-backlog, view-task
  worker/                  claim-next-task, log-progress, complete-task, block-task
  reviewer/                claim-review, approve-task, reject-task
examples/                       Integration guides for pi and Claude Code
```

## Hooks

Drop an executable in `.kanban/hooks/` named after an event. The hook gets JSON on stdin with the event type and task details. Each hook has a 30-second timeout. Errors are logged to stderr but the operation continues. Missing hooks are silently ignored.

| Event | When |
|---|---|
| `task.created` | Task dispatched |
| `task.claimed` | Agent claims |
| `task.progress` | Progress logged |
| `task.completed` | Task finished |
| `task.submitted_for_review` | Submitted |
| `task.transferred` | Claim transferred to another agent |
| `task.blocked` | Blocked |
| `review.approved` | Approved |
| `review.rejected` | Rejected |
| `task.priority_updated` | Batch priority |
| `task.project_updated` | Batch project |

Chain multiple hooks by adding a `.d/` directory. The single file runs synchronously. The `.d/` hooks run concurrently, so a slow Slack notifier won't block the caller.

```text
.kanban/hooks/
├── task-created
├── task-completed
└── task-completed.d/
    ├── slack
    ├── metrics
    └── dashboard
```

## Init command

```bash
kanban init                           # interactive prompt
kanban init --harness pi              # pi extensions
kanban init --harness claude          # Claude Code agents
kanban init --harness generic         # plain .agents/ directory
kanban init --harness pi --plan plan.md
```

The plan parser handles markdown headings with optional `[p1]` priority hints and JSON arrays.

```markdown
## Set up auth [p1]
- Implement login endpoint
- Add JWT middleware
```

```json
[
  {"title": "Fix auth bug", "role": "worker", "priority": 1}
]
```

### Product owner workflow

For real plans (specs, PRDs, anything with sections and tables and timelines), the Go parser won't cut it. Use the LLM-driven skills instead. An agent reads the plan, understands what it describes, and writes a proposal to `.kanban/tasks-proposal.md`. You review it, check the items you want, then the agent dispatches each one. No brittle regex, no parser changes. Just the LLM's ability to read a document and figure out what work it describes.

```
Agent reads plan.md, writes .kanban/tasks-proposal.md
You review and check [x] on approved items
Agent reads the proposal and dispatches each checked task
```

The skills live at `embed/skills/manager/dispatch-plan.md` and `embed/skills/manager/approve-plan.md`.

## Observability

```bash
kanban --debug task claim-next --agent alice --role worker
```

Prints database ops alongside normal output. Useful for debugging lock issues, WAL behavior, and multi-agent timing.

## Production notes

3 to 10 concurrent agents works fine. Past 50 you'll see contention on claim-next. The retry loop (100ms, 200ms, 400ms backoff) handles it, but measure latency. Past 1000, SQLite itself becomes the bottleneck. Switch to Postgres or a distributed queue.

Back up the `.db` file like any database. WAL mode lets you safely copy the main file while the system runs. Schema changes happen automatically on open. Events expire after 3 days by default; set `ttl_seconds` to NULL for permanent events. Run `kanban prune --before 30d` to clean up, then `VACUUM` to reclaim space.

## How it works

One `.db` file per project, shared by all agents. No server. Two agents calling claim-next at the same time get different tasks because SQLite serializes writes. A claimed task has a 15-minute lease. The log-progress command renews it. If an agent crashes, the lease expires and the next claim-next reclaims the task. The WAL file checkpoints itself every 1000 pages so it doesn't eat your disk.

```text
Worker-A claims TASK-1. Worker-A crashes.
15 minutes later the lease expires.
Worker-B calls claim-next and gets TASK-1.
```

Every command prints stable JSON to stdout. Empty work returns `{}`. Errors go to stderr as `{"error":"..."}` with exit code 2. Skill files (embedded in the binary, written by `kanban init`) teach agents the protocol. No tool-calling framework needed.

## Integration examples

- [examples/pi-subagents.md](examples/pi-subagents.md) -- three-coder setup
- [examples/claude-code-subagents.md](examples/claude-code-subagents.md) -- Claude Code coordination

## Building

```bash
go build -o kanban ./cmd/kanban/
```

Requires Go 1.24 or later. Pure Go, no CGo.

## License

MIT