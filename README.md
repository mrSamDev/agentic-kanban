# agentic-kanban

SQLite-backed task coordination for AI agents.

No server. No daemon. No queues. Just a shared database file that agents use to claim, track, review, and complete work.

Single Go binary + SQLite.

```bash
curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-kanban/main/install.sh | sh
```

## Why

AI agents (subagents in pi, Claude, etc.) need shared state without servers. Kanban gives them a SQLite-backed task board they all read/write. Agents claim tasks, report progress, and complete work — the `.db` file is the coordination point.

## When to use this

**Use agentic-kanban when:**

- Multiple AI agents need shared state
- Agents run on the same machine or shared filesystem
- You want durable coordination without Redis / Postgres / message queues
- You need crash recovery and task ownership

**Not a fit when:**

- Agents run across untrusted networks
- You need real-time push notifications
- You need thousands of concurrent workers

For those cases, look at Temporal, Celery, or Kafka.

## Quick start

```bash
# Install
curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-kanban/main/install.sh | sh

# Init a project (creates DB, scaffolds skills)
kanban init --harness pi

# Or init + seed from a plan file
kanban init --harness pi --plan plan.md

# Just use it — default DB path is .kanban/kanban.db
# Tasks may be created by humans, manager agents, or orchestration agents.
# Once created, workers and reviewers coordinate entirely through the shared DB.

kanban task dispatch --title "Set up auth" --role worker --priority 10

# Agent claims and completes
kanban task claim-next --agent my-agent --role worker
# Returns task JSON if work exists
# Returns {} if no eligible task exists
kanban task log-progress TASK-1 --agent my-agent --note "Working" --type PROGRESS
kanban task complete TASK-1 --agent my-agent
```

## Commands

| Command | Role | Description |
|---|---|---|
| `task dispatch --title --role [--priority]` | any | Create a task (status=TODO). Humans or agents. |
| `task claim-next --agent --role` | worker, reviewer | Atomically claim highest-priority task. `{}` if none. |
| `task log-progress <id> --agent --note [--type]` | worker | Log progress + renew 15-min lease |
| `task block <id> --agent --reason` | worker | Mark blocked, clear lease |
| `task complete <id> --agent [--review]` | worker | Mark done (or submit for review) |
| `task view <id>` | all | Full detail: task + notes + history |
| `task search [--status] [--role] [--agent]` | manager | Filter task list |
| `task approve <id> --agent` | reviewer | Approve IN_REVIEW → DONE (no claim needed) |
| `task reject <id> --agent --reason` | reviewer | Reject IN_REVIEW → TODO (no claim needed) |
| `init [--harness] [--plan] [--dir]` | setup | Scaffold DB + skills for pi, claude, or generic |

## How it works

- **One `.db` file per project.** Agents share it. No server.
- **Atomic task claims.** Two agents calling `claim-next` simultaneously get different tasks (SQLite write-serialized).
- **Lease-based crash recovery.** A claimed task has a 15-minute lease. `log-progress` renews it. If an agent crashes, the lease expires and the next `claim-next` reclaims the task.

```
Worker-A claims TASK-1.
Worker-A crashes.
15 minutes later the lease expires.
Worker-B calls claim-next and automatically receives TASK-1.
```
- **JSON output.** Every command prints stable JSON on stdout. `claim-next` with no work returns `{}`. Errors go to stderr as `{"error":"..."}` with exit code 2.
- **Markdown skills.** `skills/worker/` etc. contain docs agents read to learn the protocol. No tool-calling protocol needed.

## Init command

`kanban init` bootstraps a project with a kanban database and agent skill files:

```bash
# Interactive harness prompt
kanban init

# Or specify harness directly
kanban init --harness pi
kanban init --harness claude
kanban init --harness generic

# Seed tasks from a plan file (markdown headings or JSON)
kanban init --harness pi --plan plan.md
kanban init --harness pi --plan plan.json --dir ./my-project
```

Plan file formats:

```markdown
## Set up auth [p1]
- Implement login endpoint
- Add JWT middleware

## Add CI pipeline

## Review everything 🔥
```

Priority hints: `[p1]`-`[p999]` in headings, or `🔥` = priority 1.

Or JSON:

```json
[
  {"title": "Fix auth bug", "role": "worker", "priority": 1},
  {"title": "Review PR", "role": "reviewer", "priority": 5}
]
```

## Workflow

```
TODO
  │ dispatch
  ▼
TODO ── claim-next ──> IN_PROGRESS ── complete --review ──> IN_REVIEW ── approve ──> DONE
                          │                                       │
                          │ block                                 │ reject
                          ▼                                       ▼
                       BLOCKED                                  TODO
```

Reviewers approve/reject IN_REVIEW tasks directly — no claim needed.

## Architecture

```
Manager                    Workers                    Reviewers
  │                           │                           │
  ├── dispatch tasks ────────>│                           │
  │                           ├── claim-next (TODO)       │
  │                           ├── log-progress (heartbeat)│
  │                           ├── complete --review ─────>│
  │                           │              ├── approve (no claim)
  │                           │              ├── reject (no claim)
  │<── search --status BLOCKED│                           │
  └── unblock / reassign ────>│                           │
```

## Project structure

```
├── cmd/kanban/main.go        # CLI entrypoint
├── internal/
│   ├── bootstrap/
│   │   ├── bootstrap.go      # Init logic + plan parsing
│   │   ├── bootstrap_test.go # Tests for init + plan parsing
│   │   └── skills.go         # Skill templates
│   ├── storage/
│   │   ├── schema.sql        # SQLite schema (embedded)
│   │   └── sqlite.go         # Connection + pragmas + migration
│   └── task/
│       ├── helpers.go        # Errors, service struct
│       ├── model.go          # Structs
│       ├── queries.go        # View, Search
│       ├── service.go        # Business logic
│       └── service_test.go   # 20+ tests (incl. concurrent claim race)
├── skills/
│   ├── manager/              # dispatch-task, review-backlog, view-task
│   ├── worker/               # claim-next-task, log-progress, complete-task, block-task
│   └── reviewer/             # claim-review, approve-task, reject-task
├── examples/
│   ├── pi-subagents.md       # Integration guide for pi
│   └── claude-code-subagents.md  # Integration guide for Claude Code
└── install.sh                # curl-install script
```

## Skills (agent docs)

Each role directory in `skills/` contains markdown files agents read to learn the protocol. The files describe exact bash commands, JSON output shapes, and exit codes.

- [skills/manager/](skills/manager/) — dispatch, review backlog, view task
- [skills/worker/](skills/worker/) — claim, log progress, block, complete
- [skills/reviewer/](skills/reviewer/) — claim review, approve, reject

## Integration examples

- [examples/pi-subagents.md](examples/pi-subagents.md) — three-coder setup with pi
- [examples/claude-code-subagents.md](examples/claude-code-subagents.md) — Claude Code subagent coordination

## Usage with pi subagents

```bash
cd my-project

# Install and init with pi harness
curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-kanban/main/install.sh | sh
kanban init --harness pi

# Dispatch work
kanban task dispatch --title "Refactor auth" --role worker --priority 1
kanban task dispatch --title "Add tests" --role worker --priority 5

# Let agents coordinate (no --db needed)
subagent --agent worker --task "
  Read skills/worker/claim-next-task.md.
  Claim and execute the next worker task.
"
```

## Building

```bash
go build -o kanban ./cmd/kanban/
```

Requires Go 1.24+. Pure Go, no CGo.

## License

MIT