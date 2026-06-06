# Batch Operations

Update multiple tasks at once. Use for re-prioritizing, re-scoping, or bulk cleanup.

## Usage

### Set Priority for Multiple Tasks

```bash
# Lower priority (less urgent) for old eval tasks
kanban task batch set-priority \
  --ids TASK-1,TASK-2,TASK-3,TASK-4,TASK-5 \
  --priority 999

# Raise priority for urgent fixes
kanban task batch set-priority \
  --ids TASK-10,TASK-11 \
  --priority 1
```

### Set Project for Multiple Tasks

```bash
# Move old tasks to archive
kanban task batch set-project \
  --ids TASK-1,TASK-2,TASK-3 \
  --project archive

# Group tasks by eval type
kanban task batch set-project \
  --ids TASK-20,TASK-21,TASK-22 \
  --project dtt-eval
```

## Flags

### set-priority
| Flag | Required | Description |
|---|---|---|
| `--ids` | yes | Comma-separated task IDs (no spaces) |
| `--priority` | yes | New priority value (lower = more urgent) |

### set-project
| Flag | Required | Description |
|---|---|---|
| `--ids` | yes | Comma-separated task IDs (no spaces) |
| `--project` | yes | Project/scope label |

## Tips

- Use `kanban task search --status TODO --limit 100` to find task IDs first
- IDs are comma-separated with no spaces
- All updates happen in a single transaction
- No history entries are created for batch operations
