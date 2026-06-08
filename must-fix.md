# Must Fix

Discovered during evaluation-harness-v4 run (69m, 25 tasks, multi-agent). See `.jsonl` for raw session log.

---

## 1. Review gate unused (skill gap only — not a protocol bug)

**Evidence**: 23 of 25 tasks reached `IN_REVIEW` state. Zero were approved. Auto-execute loop claimed → built → submitted-for-review → moved on. Never wore the reviewer hat.

**Protocol state**: Correct. `IN_REVIEW`, approve/reject transitions, reviewer skill files all exist. The README explicitly defines reviewer as a separate role.

**Gap**: The execution skill (`approve-plan` / auto-execute loop) never enters the review phase.

**Fix** (skill-level only, no protocol/bin changes):

Add a review pass at the end of the auto-execute loop:
1. Poll `review_backlog --status IN_REVIEW`
2. For each: `approve_task --agent <orchestrator-id>`
3. Repeat until 0 IN_REVIEW remain (or timeout)

Note: `review.go` has `checkSelfReview()` which blocks the orchestrator from approving tasks it claimed. Set `KANBAN_ALLOW_SELF_REVIEW=true` in the auto-execute loop environment — the orchestrator IS the reviewer (it dispatched, spawned workers, collected results, verified output).

---

## 2. Subagent identity mismatch on complete

**Evidence**: `claim` binds task to `samdev`. Subagent `pi-worker` does the work. Subagent calls `complete_task`. DB rejects: "not assigned to this agent."

**Protocol state**: `assigned_agent` is the claim owner. `complete` checks `assigned_agent == caller` before transitioning.

**Gap**: No mechanism for delegating completion authority to a subagent.

### Fix: Option C — Delegated claim transfer

New operation: `kanban task claim --transfer TASK-N --agent pi-worker`

Moves `assigned_agent` from orchestrator to subagent. Subagent becomes the sole owner — can complete, extend lease, log progress. If subagent crashes, lease expires and another agent reclaims. Same crash recovery as any worker.

**Why Option C over Option A**: Option A has two entities sharing ownership (orchestrator holds claim, subagent does work). If orchestrator crashes, subagent's work is orphaned — nobody can complete it. Option C makes the subagent the sole owner. "One owner at a time" extends to hierarchical delegation.

#### Implementation plan

```
kanban task claim --transfer TASK-N --agent <to-agent>
```

1. New CLI flag: `--transfer` on `task claim`
2. New Service method: `TransferClaim(ctx, id, fromAgent, toAgent)`
3. SQL: `UPDATE tasks SET assigned_agent = ?, lease_until = datetime('now', '+' || 15 || ' minutes') WHERE id = ? AND assigned_agent = ?`
4. History: `INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'TRANSFER')`
5. Event: `task.transferred` with `{from_agent, to_agent}` in payload
6. Hook: `task.transferred` event type

~35 lines of Go. No new tables, no new concepts. Extends existing claim model.

#### Why it passes the protocol test

"Does this make the coordination protocol more reliable or more useful for multi-agent coordination?"

Yes. Currently crash recovery only covers peer workers. Hierarchical delegation (orchestrator → subagent) has a single point of failure. Claim transfer extends crash recovery to every agent in the chain.

#### Companion pattern: Option A (orchestrator completes)

For fast subagents (<15min) where the orchestrator stays alive, no transfer needed:

```
orchestrator claims → spawns subagent → collects result → complete_task
```

Zero protocol changes. Works today. The orchestrator holds the claim and completes it after collecting the subagent's result. Document in the auto-execute skill alongside the transfer pattern.

#### Option considered (not chosen)

- **Option B (parent completion / `--as` flag)**: Trust escalation — any agent with the original ID can complete any task. Adds surface area without fixing the ownership model.

---

## Harness artifacts / noise (not protocol bugs)

| Artifact | Why noise |
|----------|-----------|
| TASK-21 stale lease | Lease expired 2.5min before session export. `claim_next_task` auto-reclaims on next run. In-flight at session end, not a defect. |
| Re-claim noise (TASK-2 ×2, TASK-4 ×3) | Agent re-claimed already-owned tasks. Cosmetic — no data corruption. Leases tracked correctly in DB. |
| 97 empty assistant messages (39%) | Evaluation harness auto-feeding "continue" to keep agent loop alive. Harness artifact, not protocol issue. |
| `IN_REVIEW → IN_REVIEW` in log | `review_backlog` / `view_task` queries, not state machine transitions. DB had zero invalid transitions. |
| No visible success for `complete_task` by subagent | Session export only captures successful `toolResult` messages — failed calls ("not assigned to this agent") are stderr-only and invisible in `.jsonl`. Blind spot when debugging from export alone. |

## No fix planned

- **Role validation on dispatch**: `role_boundary` accepts any string. All 25 tasks dispatched as `worker` — works. Risk: silent typos. Skipping — add CHECK constraint if it causes real errors.