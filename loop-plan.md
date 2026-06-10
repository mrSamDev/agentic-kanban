# Agent-Kanban Loop Plan

**Project**: Agent-Kanban - SQLite-backed coordination protocol for AI agents
**Current Version**: v0.6.0
**Last Updated**: 2026-06-10
**Branch**: `review/final-feedback`

---

## Project Vision

Multiple AI agents working on the same machine/filesystem need durable coordination without Redis, Postgres, or message queues. Agent-Kanban solves this with SQLite as the single coordination point:

- **One database file** = the only state
- **No server, no daemon, no external services**
- **Atomic transactions** ensure no double-claiming
- **Lease-based ownership** mirrors Rust's borrowing model
- **Crash recovery** via automatic lease expiration

**Why it matters**: Agents stop overwriting each other's work, forgetting task completion, or parallel-claiming the same task. The `.db` file is the contract.

---

## Completed Work

### v0.2-v0.3 (Hooks & Events) ✅
- Webhook system for task lifecycle events
- Single-file hooks in `.kanban/hooks/task-created`, etc.
- `.d/` multi-hook directory support (Unix fan-out pattern)
- Hook timeout (30s) with stderr logging
- All tests passing

### v0.4 (Release 1: Safety) ✅
**Make multi-agent execution safe** - parallel workers can't corrupt state.

| Step | Feature | Status |
|------|---------|--------|
| 1 | `depends_on` schema + claim guard | ✅ Shipped |
| 2 | `extend-lease` command | ✅ Shipped |
| 3 | Cross-agent review gate | ✅ Shipped (env-var gated) |

**Impact**: Workers can safely depend on prior tasks. Lease auto-renewal prevents premature task reclaim. Reviewers can't approve their own work without a second agent.

---

## Roadmap: Releases 2-3

### v0.5 (Release 2: Speed) - Parallel Worker Speedup

**Make multi-agent execution fast** - efficient bulk claiming and optional delegation.

| Step | Feature | Scope |
|------|---------|-------|
| 4 | `claim-next --count N` | ✅ Atomic batch claim API; refactor `ClaimBatch` core; add batch tests |
| 5 | Optional subagent delegation | Manager protocol teaches conditional sub-dispatch (YAML/JSON); control via config |
| 6 | Project env auto-detection | Subagents in project subdirs auto-find the `.db` via env walk |

**Why**: Workers spawn parallel subagents. Rather than polling 10 times, `claim-next --count 10` gives them 10 tasks atomically. Delegation is opt-in (no forced parallelism). Subagents auto-discover the DB.

**Effort**: ~3 features, moderate refactor to batch claim, protocol update.

---

### v0.6 (Release 3: Maturity) - Operational Visibility

**Operational maturity** - progress tracking and plan validation.

| Step | Feature | Scope |
|------|---------|-------|
| 7 | `kanban status --burndown` | `BurndownStats` struct; progress %; human table + `--json`; new query |
| 8 | `kanban plan lint` | Dependency cycle detection (DFS); unknown dep warnings; lint rules in --json |
| 9 | `approve --all` flag | ✅ Added `ApproveAll` in service.go + `--all`/`--project` flags in approve command + 5 tests |

**Why**: Managers need to see progress (burndown). Plans with cycles hang forever (lint catches this). Batch-approve is faster than one-at-a-time.

**Effort**: ~3 features, new service methods, lint algo, skill update.

---

## Implementation Order

### Phase 1: Verify v0.4 Integration ✅
- [x] Read current plan.md + recent git history
- [x] Confirm v0.4 is fully merged and tested
- [x] Audit code against AGENTS.md philosophy
- [x] Lock release strategy in place — v0.5.0 and v0.6.0 tags created

**Tags**:
- `v0.5.0` ← a383b9b (batch claim, subagent delegation, env auto-detection)
- `v0.6.0` ← 4d4b181 (burndown stats, plan lint, batch approve, E2E tests)
- `v1.0.0-alpha` ← 3261e86 (feature lock — v0.4 safety + v0.5 speed + v0.6 maturity)

### Phase 2: v0.5 Speedup Features ✅
- [x] Step 4: `ClaimBatch` refactor + atomic multi-claim (`--count N`)
- [x] Step 5: Manager protocol + delegation config (serial/parallel mode in agent markdown)
- [x] Step 6: Subagent env auto-discovery (`findProjectRoot` in `config.go`)
- [x] Integration tests for concurrent claiming (concurrent batch-claim tests)
- [x] Tag v0.5 release (`v0.5.0` on a383b9b)

### Phase 3: v0.6 Maturity Features ✅
- [x] Step 7: Burndown stats + status command
- [x] Step 8: Lint engine + plan validation
- [x] Step 9: Batch approve (`--all` flag + `ApproveAll` service + 5 tests)
- [x] E2E tests for full workflow (3 tests: standard, reject-loop, batch-claim-review)
- [x] Tag v0.6 release (`v0.6.0` on 4d4b181)

### Phase 4: Polish & Polish (Current)
- [x] Benchmark concurrent claim performance
  - ClaimBatch size=1: 441µs, size=5: 645µs, size=10: 850µs
  - Concurrent 2 agents: 5.5ms, 5 agents: 9.7ms, 10 agents: 16.7ms
  - All within target (<10ms single, <100ms batch 10)
- [x] Audit schema for production readiness
  - AUDIT.md written: schema state, index coverage, migration paths, safety
  - Two index migrations applied (idx_tasks_claim + idx_tasks_claim_project)
  - No blockers — schema is production-ready
- [x] Finalize documentation
  - README.md: added missing commands (extend-lease, claim --transfer, plan lint, burndown, batch claim/complete, approve --all)
  - README.md: fixed stale embed/skills/ paths → internal/bootstrap/embed/skills/
  - README.md: added claim-task to worker skills table
  - approve-task.md skill: added --all and --project flags
- [x] Prepare v1.0 feature lock
  - RELEASE_NOTES.md written
  - Version bumped to v1.0.0-alpha
  - Feature scope locked (v0.4 safety + v0.5 speed + v0.6 maturity)

**Next task**: None — feature lock complete

---

## Code Organization

### Layout
```
cmd/kanban/               CLI layer (commands, flags, output)
  main.go               Entrypoint
  dispatch.go           `kanban task dispatch` + flags
  view.go               `kanban task view` + filtering
  plan.go               `kanban plan lint` command
  ...

internal/
  bootstrap/            Init, plan parsing, skill templates
  storage/              SQLite schema, migration, connection
  task/                 Service layer, models, queries
    model.go            Task struct + enums
    claim.go            Claim guard logic
    lint.go             Cycle detection
    queries.go          DB queries (Status, Burndown, etc)
    service.go          High-level operations

embed/skills/           Protocol skills (canonical source)
  manager/              dispatch-task, dispatch-plan, approve-plan
  worker/               claim-next-task, log-progress, complete-task
  reviewer/             claim-review, approve-task, reject-task
```

### AGENTS.md Philosophy (Golden Rules)
- **Context Acquisition**: Read only what you need; prefer targeted reads over file scans
- **Change Scope**: Only change what's required; no adjacent refactors
- **Evidence**: Read code before changing; verify assumptions
- **Simplicity**: Prefer flat control flow, early returns, explicit data flow
- **Anti-Bloat**: No deep abstractions, generic wrappers, or enterprise patterns
- **File Discipline**: Keep files ~200 lines; one responsibility per file
- **Function Discipline**: Keep functions ~50 lines; one job per function
- **Naming**: Names explain intent without comments

---

## Critical Design Decisions

### 1. Dependency Model
**Choice**: Comma-separated TEXT (`depends_on` field)
**Why**: Simple for forward queries ("can I claim?"). Reverse queries need LIKE scan but fan-out is small.
**Trade-off**: No FK constraint. Acceptable for expected dependency sizes.

### 2. Concurrency Strategy
**Choice**: Serializable transactions (explicit) over implicit atomic updates
**Why**: Enables dep filtering while preserving safety. Better for understanding under load.
**Trade-off**: Slightly slower than single atomic update, but not measurable for expected worker counts (3-50).

### 3. Claim Guard
**Rule**: `ClaimNext` returns only tasks with satisfied dependencies.
**Check**: Before claiming, verify all deps in `depends_on` are in DONE status.
**Rollback**: If a dep is not DONE, task stays in TODO and is skipped.

### 4. Lease-Based Ownership
**Model**: Rust-like borrowing - one agent owns a task at a time.
**Expires**: After `ttl_seconds` (default 300s) with no `log-progress` renewal.
**Recovery**: Expired lease is auto-released; another agent can claim.

### 5. Review Gate (Cross-Agent)
**Rule**: Agent A can't approve work submitted by Agent A.
**Mechanism**: `submitted_by` vs `approved_by` fields in task history.
**Gate**: Env var `KANBAN_ALLOW_SELF_REVIEW=false` (default).
**Use Case**: Prevent a single agent from self-approving in demos; require second agent in production.

---

## Testing Strategy

### Unit Tests
- Claim guard: deps satisfied vs unsatisfied
- Lease expiration: time-based ownership
- Burndown: stats calculation correctness
- Lint: cycle detection, unknown deps, error ordering

### Integration Tests
- Multi-agent claiming: no double-claim race conditions
- Dependency chains: task waits for prior completion
- Lease renewal: `log-progress` keeps task owned
- Review gate: self-approval blocked/allowed per env var

### E2E Tests
- Full workflow: dispatch → claim → progress → complete → review → approve → done
- Concurrent workers: 5 agents on same board
- Plan-based dispatch: parse plan, auto-create tasks, run workflow

---

## Performance Targets

| Operation | Target | Mechanism |
|-----------|--------|-----------|
| Claim one task | <10ms | Atomic SELECT+UPDATE |
| Claim N tasks | <10+N/10ms | Batch claim loop |
| Burndown query | <50ms | COUNT(*) per status |
| Lint check | <100ms | DFS on deps graph |
| Full workflow (10 tasks) | <1s | Serial path + overhead |

**Note**: Bottleneck is SQLite's single-writer design. Acceptable for 3-50 workers. Beyond that, use Temporal/Celery.

---

## Skill System (Runtime)

Each role gets protocol skills (coordination) + task skills (domain-specific):

### Manager
- `dispatch-task`: Create TODO from CLI
- `dispatch-plan`: Parse plan.md, create task batch
- `approve-plan`: Batch-approve all IN_REVIEW (v0.6)
- `review-backlog`: Search and filter tasks
- `view-task`: Details + history

### Worker
- `claim-next-task`: Claim one unblocked task
- `log-progress`: Renew lease, record progress
- `complete-task`: Mark done or submit for review
- `block-task`: Mark blocked, drop lease

### Reviewer
- `claim-review`: Claim task(s) in IN_REVIEW (no ownership)
- `approve-task`: Move IN_REVIEW → DONE
- `reject-task`: Move IN_REVIEW → TODO with reason

---

## Hooks & Events

Drop an executable in `.kanban/hooks/` (single file) or `.kanban/hooks/task-created.d/` (directory of executables):

| Event | When | Payload |
|-------|------|---------|
| `task.created` | Task dispatched | Task ID, title, role |
| `task.claimed` | Agent claims | Task ID, agent, role |
| `task.progress` | Progress logged | Task ID, agent, note |
| `task.completed` | Task finished | Task ID, agent, status |
| `task.submitted_for_review` | Submitted | Task ID, agent, note |
| `task.transferred` | Claim transferred | Task ID, from_agent, to_agent |
| `task.blocked` | Blocked | Task ID, agent, reason |
| `review.approved` | Approved | Task ID, reviewer |
| `review.rejected` | Rejected | Task ID, reviewer, reason |

**Timeout**: 30s per hook. Errors logged to stderr; operation continues.

---

## Deployment Checklist

### Pre-Release (v0.5, v0.6)
- [ ] All tests pass (`go test ./...`)
- [ ] Code audit against AGENTS.md
- [ ] README updated with new commands
- [ ] Examples added for new features
- [ ] Skills updated (manager, worker, reviewer)
- [ ] Backward compatibility verified (schema migration)
- [ ] Performance benchmarked
- [ ] Git tag created (`vX.Y.Z`)

### Release
- [ ] GitHub release with binary + checksum
- [ ] Update install.sh to pull new version
- [ ] Announce on relevant channels

### Post-Release
- [ ] Monitor issue reports
- [ ] Patch any critical bugs
- [ ] Gather user feedback for next cycle

---

## Known Limitations

1. **Single Writer**: SQLite is single-writer. For 50+ concurrent workers, use Temporal/Celery.
2. **No Remote**: Agents must be on same machine or shared filesystem. No network coordination.
3. **No Push Notifications**: Agents must poll for status. No real-time alerts.
4. **Dependency Reverse Queries**: Finding "what tasks depend on TASK-8" requires LIKE scan (no FK).
5. **No Sharding**: Single `.db` file per project. Can't split across databases for scaling.

---

## Success Metrics

- **Safety**: Zero double-claims under concurrent load (50 agents)
- **Speed**: Batch claim 10 tasks in <100ms
- **Maturity**: Lint catches all plan cycles; burndown shows real progress
- **Simplicity**: All tests pass; code < 2000 LOC (cmd + internal); maintainable by one engineer
- **Adoption**: Works with Claude Code, PI, Codex, and other agents

---

## Next Steps (Immediate)

1. **Lock Release 1 (v0.4)**: Confirm all changes merged and tested
2. **Plan Release 2 (v0.5)**: Detailed spec for `claim-next --count N`
3. **Kickoff**: Start Step 4 (ClaimBatch refactor)

---

## References

- **README.md**: Feature overview, quick start, workflow diagram
- **AGENTS.md**: Engineering philosophy (context, scope, simplicity rules)
- **plan.md**: Detailed implementation spec per step
- **embed/skills/**: Protocol skill templates (canonical source)
- **internal/task/**: Service layer + models
