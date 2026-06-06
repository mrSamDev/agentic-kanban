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

# Just use it — default DB path is .kanban/kanban.db relative to current dir
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
| `task approve <id> --agent` | reviewer | Approve IN_REVIEW → DONE |
| `task reject <id> --agent --reason` | reviewer | Reject IN_REVIEW → TODO |

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

## Architecture

```
Manager                    Workers                    Reviewers
  │                           │                           │
  ├── dispatch tasks ────────>│                           │
  │                           ├── claim-next (TODO)       │
  │                           ├── log-progress (heartbeat)│
  │                           ├── complete --review ─────>│
  │                           │              ├── claim-next (IN_REVIEW)
  │                           │              ├── approve / reject
  │<── search --status BLOCKED│                           │
  └── unblock / reassign ────>│                           │
```

## Project structure

```
├── cmd/kanban/main.go        # CLI entrypoint
├── internal/
│   ├── storage/
│   │   ├── schema.sql        # SQLite schema (embedded)
│   │   └── sqlite.go         # Connection + pragmas + migration
│   └── task/
│       ├── model.go          # Structs
│       ├── service.go        # Business logic
│       └── service_test.go   # 20 tests (incl. concurrent claim race)
├── skills/
│   ├── manager/              # dispatch-task, review-backlog, view-task
│   ├── worker/               # claim-next-task, log-progress, complete-task, block-task
│   └── reviewer/             # claim-review, approve-task, reject-task
└── install.sh                # curl-install script
```

## Skills (agent docs)

Each role directory in `skills/` contains markdown files agents read to learn the protocol. The files describe exact bash commands, JSON output shapes, and exit codes.

- [skills/manager/](skills/manager/) — dispatch, review backlog, view task
- [skills/worker/](skills/worker/) — claim, log progress, block, complete
- [skills/reviewer/](skills/reviewer/) — claim review, approve, reject

## Usage with pi subagents

```bash
cd my-project

# Install kanban into project
curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-kanban/main/install.sh | sh

# Just use it — .kanban/kanban.db is the default
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