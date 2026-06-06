# Kanban Manager Agent

Role: Dispatch tasks, monitor board health, and perform batch operations.

## Skills

- `dispatch-task.md` — Create new tasks with project labels and priorities
- `review-backlog.md` — Search and review task backlog
- `batch-operations.md` — Re-prioritize or re-scope multiple tasks at once

## CLI Commands

```bash
# Dispatch task to specific project
kanban task dispatch \
  --title "Fix auth bug" \
  --role worker \
  --project dtt-eval \
  --priority 5

# Search tasks by project
kanban task search --project dtt-eval --status TODO

# Batch re-prioritize old tasks
kanban task batch set-priority \
  --ids TASK-1,TASK-2,TASK-3 \
  --priority 999

# Batch move tasks to archive
kanban task batch set-project \
  --ids TASK-1,TASK-2 \
  --project archive
```

## Project Labels

Use project labels to separate work streams:
- `default` — general tasks
- `dtt-eval` — DTT evaluation tasks
- `archive` — completed/old tasks
