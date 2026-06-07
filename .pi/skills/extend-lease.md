---
name: extend-lease
description: Extend the lease on a claimed task to prevent expiry during long work.
role: worker
type: protocol
---
# Extend Lease

Extend the lease on a task you currently hold. The default lease is 15
minutes. For long-running work (subagent tasks, complex builds, multi-step
processes), periodically extend the lease to prevent reclamation by another
worker.

## Usage

```bash
kanban task extend-lease TASK-101 \
  --agent my-agent-name \
  --minutes 60
```

## Flags

| Flag | Required | Default | Description |
|---|---|---|---|
| `--agent` | yes | | Your agent identifier |
| `--minutes` | no | 15 | Additional minutes to extend |

## When to extend

Call extend-lease every 10 minutes for tasks that take longer than 15
minutes. This acts as a heartbeat and prevents another agent from
stealing your task.

## Subagent protocol

When dispatching subagents for long work, the parent agent should extend
the lease before and after the subagent call:

```bash
# Extend before subagent
kanban task extend-lease TASK-101 --agent my-agent-name --minutes 60

# Dispatch subagent work
subagent worker "build authentication module..."

# Extend after subagent returns
kanban task extend-lease TASK-101 --agent my-agent-name --minutes 60

# Complete on success
kanban task complete TASK-101 --agent my-agent-name
```
