# agentic-kanban

Portable, zero-infrastructure kanban for cooperating AI agents. Single Go binary + SQLite. No daemon, no network, no queue.

```bash
curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-kanban/main/install.sh | sh
```

## Why

AI agents (subagents in pi, Claude, etc.) need shared state without servers. Kanban gives them a SQLite-backed task board they all read/write. Agents claim tasks, report progress, and complete work — the `.db` file is the coordination point.

## Quick start

```bash
# Install
curl -sfL https://raw.githubusercontent.com/mrSamDev/agentic-kanban/main/install.sh | sh

# Init a board
export KANBAN_DB="$(pwd)/.kanban/kanban.db"
kanban --db "$KANBAN_DB" task dispatch --title "Set up auth" --role worker --priority 10

# Agent claims and completes
kanban --db "$KANBAN_DB" task claim-next --agent my-agent --role worker
kanban --db "$KANBAN_DB" task log-progress TASK-1 --agent my-agent --note "Working" --type PROGRESS
kanban --db "$KANBAN_DB" task complete TASK-1 --agent my-agent
```

## Commands

| Command | Role | Description |
|---|---|---|
| `task dispatch --title --role [--priority]` | manager | Create a new task (status=TODO) |
| `task claim-next --agent --role` | worker, reviewer | Atomically claim highest-priority task |
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
- **JSON output.** Every command prints stable JSON on stdout. `claim-next` with no work returns `{}`. Errors go to stderr as `{"error":"..."}` with exit code 2.
- **Markdown skills.** `skills/worker/` etc. contain docs agents read to learn the protocol. No tool-calling protocol needed.

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

# Init the board
export KANBAN_DB="$(pwd)/.kanban/kanban.db"
kanban --db "$KANBAN_DB" task dispatch --title "Refactor auth" --role worker --priority 1
kanban --db "$KANBAN_DB" task dispatch --title "Add tests" --role worker --priority 5

# Let agents coordinate
subagent --agent worker --task "
  Read skills/worker/claim-next-task.md.
  Export KANBAN_DB=$KANBAN_DB
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