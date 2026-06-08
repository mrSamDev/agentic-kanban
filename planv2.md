
 Realignment Plan — "Coordination Protocol, Not CLI Tool"

 ### North Star (from README)

 │ Shared DB + skills = coordination protocol. Agents change. The protocol stays.

 Every build decision passes this test: "Does this make the protocol more reliable or more useful for multi-agent coordination?" If answer is "CLI ergonomics" or "visual polish" → skip.

 ────────────────────────────────────────────────────────────────────────────────

 ### Phase 0: Fix Regressions (urgent)

 Bugs that broke v1 behavior. No new features. Ship as v0.1.12.

 P0 — Dispatch crash (UNIQUE constraint failed: tasks.id)

 Root cause: nextID() uses `UPDATE task_seq SET next_id = next_id + 1 RETURNING next_id` but `task_seq` always starts at `next_id = 0` (see schema.sql: `INSERT OR IGNORE INTO task_seq (next_id) VALUES (0)`). If pre-existing tasks exist from a prior session (e.g., TASK-1 through TASK-11), the sequence generates TASK-1 which already exists → UNIQUE constraint failed.

 Reproduced in v2logs.txt: first `kanban task dispatch` batch hit UNIQUE constraint after 0 tasks dispatched; retry with MCP `dispatch_task` worked because the CLI call had already incremented the sequence without inserting a row, leaving the sequence ahead of actual tasks.

 Fix: seed `task_seq.next_id` from `COALESCE(MAX(substr(id,6)::integer), 0)` on init, and add retry-with-reconcile in nextID() when conflict occurs.

 P0 — kanban task batch claim-complete --agent pi → unknown flag

 batch claim-complete doesn't exist. Only batch set-priority / batch set-project. The agent tried it after suffering through 33 serial round-trips for 11 tasks (see v2logs.txt). This is NOT hallucination — it's desperation.
 
 Correctly classified: Phase 1 feature gap, not Phase 0 regression. The command never existed. Adding batch claim + batch complete is the only fix.

 P0 — kanban status wrong counts

 kanban status queries the DB at the other project's .kanban/kanban.db (agentic-kanban-evaluation-harness). It read the pre-existing stale task from prior session. Fix: status should filter by project or include a
 --project flag. But the deeper issue: agents need kanban status --json to read state programmatically.

 ────────────────────────────────────────────────────────────────────────────────

 ### Phase 1: Protocol Features (core alignment)

 Features that directly serve multi-agent coordination. Each maps to README's "shared DB + skills" philosophy.

 P1 — batch claim + batch complete

 Without this, parallel workers are impossible. Current agents waste 80% of calls on board bookkeeping. The DB supports it (Serializable txns, ClaimBatch exists for claim). Just missing CLI surface and batch
 complete service method.

 ```
   kanban task batch claim --agent pi --role worker --count 3   # claims 3 tasks atomically
   kanban task batch complete --ids TASK-1,TASK-2,TASK-3 --agent pi  # completes 3 in one txn
 ```

 Implementation: BatchComplete(ctx, ids, agent) — single serializable txn, one hook per task.

 P1 — kanban status --json

 Agents can't read ASCII tables. They need structured data to make decisions.

 ```
   kanban status --json
   → {"by_status": {"TODO": 3, "IN_PROGRESS": 1, "DONE": 7}, "by_role": {"worker": 11}, "expired_leases": 0, "total": 11}
 ```

 Already have Burndown() and Stats() in service layer — wire to CLI.

 P2 — claim-next --respect-deps flag

 depends_on column exists. ClaimBatch already filters unmet deps internally. But CLI doesn't expose it. Add --respect-deps to claim-next so agents can opt into DAG ordering.

 ────────────────────────────────────────────────────────────────────────────────

 ### Phase 2: New Protocol Skills (protocol evolves through skills, not CLI)

 README: "Skill files teach agents the protocol. The agents change. The protocol stays."

 P1 — batch-claim-task.md (worker skill)

 Teaches agents: "Use kanban task batch claim --agent <name> --role <role> --count N to claim multiple tasks atomically. Use kanban task batch complete --ids TASK-1,TASK-2 --agent <name> to complete in one
 transaction."

 P1 — batch-complete-task.md (worker skill)

 As above.

 P2 — extend-lease.md (worker skill)

 Teaches agents: "For long work, call kanban task extend-lease TASK-N --agent <name> --minutes 60 before lease expires. Default lease is 15 min. Call every 10 min as heartbeat."

 Already exists in CLI (extendLeaseCmd in task.go), just missing embedded skill.

 P2 — review-gate-skill.md (manager skill)

 Teaches managers: "Cross-agent review is enforced by default. Set KANBAN_ALLOW_SELF_REVIEW=true for solo mode. Use kanban task approve TASK-N --agent <name> — the review agent must differ from the worker agent."

 Self-review gate already exists in code (checkSelfReview in review.go, ErrSelfReview). But no skill teaches agents this.

 ────────────────────────────────────────────────────────────────────────────────

 ### Phase 3: What NOT to Build

 ┌──────────────────────────────────┬────────────────────────────────────────────────────────┐
 │ Shipped in v0.1.11               │ Verdict                                                │
 ├──────────────────────────────────┼────────────────────────────────────────────────────────┤
 │ events list/tail                 │ Shipped. Borderline — event log is protocol obs, keep  │
 ├──────────────────────────────────┼────────────────────────────────────────────────────────┤
 │ view --notes/--history           │ Shipped. Borderline — task context is protocol, keep    │
 ├──────────────────────────────────┼────────────────────────────────────────────────────────┤
 │ re-init                          │ Shipped. Skip going forward — CLI ergonomics            │
 ├──────────────────────────────────┼────────────────────────────────────────────────────────┤
 │ upgrade --check                  │ Shipped. Skip going forward — CLI ergonomics            │
 ├──────────────────────────────────┼────────────────────────────────────────────────────────┤
 │ plan lint                        │ Shipped. Misaligned — agents need skill-driven validation│
 ├──────────────────────────────────┼────────────────────────────────────────────────────────┤
 │ Graphify, impeccable, subagent-* │ ROADMAP noise — not kanban's job                       │
 └──────────────────────────────────┴────────────────────────────────────────────────────────┘

 Future roadmap should be:
 - v0.2 = batch operations + dep enforcement (multi-agent protocol)
 - v0.3 = skill lifecycle (list, validate, upgrade, custom skills)
 - v0.4 = observability (events, status --json, hooks)
 - v0.5 = worker pools (if the protocol proves out)

 ────────────────────────────────────────────────────────────────────────────────

 ### Summary Table

 ┌───────────────────────────────────────┬─────────────────────────────────────────────────┬───────┬──────────────────────────────────────────┐
 │ Current state                         │ Fix                                             │ Phase │ README alignment                         │
 ├───────────────────────────────────────┼─────────────────────────────────────────────────┼───────┼──────────────────────────────────────────┤
 │ Dispatch crash                        │ Fix nextID() seq collision                      │ P0    │ Protocol must be reliable                │
 ├───────────────────────────────────────┼─────────────────────────────────────────────────┼───────┼──────────────────────────────────────────┤
 │ Unknown flag on batch (33 RTT)        │ Add batch claim + batch complete                │ P1    │ Parallel workers = coordination protocol │
 ├───────────────────────────────────────┼─────────────────────────────────────────────────┼───────┼──────────────────────────────────────────┤
 │ Status wrong counts                   │ Add --json + --project filter                   │ P1    │ Agents need structured state             │
 ├───────────────────────────────────────┼─────────────────────────────────────────────────┼───────┼──────────────────────────────────────────┤
 │ No parallelism                        │ batch claim --count N + batch complete --ids    │ P1    │ Core protocol feature                    │
 ├───────────────────────────────────────┼─────────────────────────────────────────────────┼───────┼──────────────────────────────────────────┤
 │ Dep enforcement exists but hidden     │ Expose --respect-deps on claim-next             │ P1    │ Per-command flag, no new infra           │
 ├───────────────────────────────────────┼─────────────────────────────────────────────────┼───────┼──────────────────────────────────────────┤
 │ Reviewer gate exists but no skill     │ Add review-gate-skill.md                        │ P2    │ Skills teach protocol                    │
 ├───────────────────────────────────────┼─────────────────────────────────────────────────┼───────┼──────────────────────────────────────────┤
 │ No extend-lease skill                 │ Add extend-lease.md skill                       │ P2    │ Skills teach protocol                    │
 ├───────────────────────────────────────┼─────────────────────────────────────────────────┼───────┼──────────────────────────────────────────┤
 │ No claim-complete skill               │ Add batch-claim-task.md, batch-complete-task.md │ P2    │ Skills teach protocol                    │
 ├───────────────────────────────────────┼─────────────────────────────────────────────────┼───────┼──────────────────────────────────────────┤
 │ CLI polish (re-init, upgrade --check) │ Stop building                                   │ Never │ Not coordination protocol                │
 └───────────────────────────────────────┴─────────────────────────────────────────────────┴───────┴──────────────────────────────────────────┘

 Want me to dispatch this as kanban tasks?