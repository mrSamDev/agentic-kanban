# Agent-Kanban v1.0.0-alpha — Release Notes

## Feature Lock: v0.4 Safety + v0.5 Speed + v0.6 Maturity

This release locks the feature set for v1.0.0-alpha — all planned protocol features are implemented, tested, and ready for multi-agent coordination.

## v0.4 — Safety Release (Cross-Agent Safety)

**Make multi-agent execution safe.** Parallel workers can't corrupt state.

- `depends_on` schema + claim guard — tasks with unmet dependencies are invisible to claim-next
- `extend-lease` command — extend TTL on a claimed task without logging progress
- Cross-agent review gate — `KANBAN_ALLOW_SELF_REVIEW=false` prevents self-approval

## v0.5 — Speed Release (Parallel Worker Speedup)

**Make multi-agent execution fast.** Batch operations and delegation primitives.

- `claim-next --count N` — atomic batch claim (N tasks in one transaction)
- `batch-complete` — complete multiple tasks in one call
- `claim --transfer --to` — subagent delegation (transfer ownership between agents)
- Project env auto-detection — subagents auto-find `.kanban.db` via directory walk
- Concurrent claim tests — no double-claims under parallel load

## v0.6 — Maturity Release (Operational Visibility)

**Operational maturity.** Progress tracking, plan validation, batch review.

- `kanban status --burndown` — progress percentage by status bucket (JSON + table)
- `kanban plan lint` — dependency cycle detection (DFS) + unknown dep warnings
- `approve --all` — batch-approve all IN_REVIEW tasks (optional `--project` filter)
- E2E tests — full workflow (standard, reject-loop, batch-claim-review)

## Performance

| Operation | Benchmark |
|-----------|-----------|
| Claim 1 task | 441µs |
| Claim 5 tasks (batch) | 645µs |
| Claim 10 tasks (batch) | 850µs |
| Concurrent 2 agents × 5 tasks | 5.5ms |
| Concurrent 5 agents × 5 tasks | 9.7ms |
| Concurrent 10 agents × 5 tasks | 16.7ms |

## Schema

Production-ready schema with two index migrations applied:
- `idx_tasks_claim` — covers claim-next query (status, project, role, priority)
- `idx_tasks_claim_project` — covers project-scoped claim queries

## Distribution

```
v1.0.0-alpha ← <current HEAD>
```

GitHub release includes binary, checksum, and install.sh.