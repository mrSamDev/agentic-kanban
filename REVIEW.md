# Review Template: `taskbind`

> **How to use:** Run each `How to verify` command. Mark PASS/FAIL. Fill summary tables. No narrative prose needed — a flash model can execute this mechanically.

---

## YAGNI Principle (Read First)

Before proposing any fix, apply these filters:

| If fix involves… | YAGNI says… |
|---|---|
| Custom CI dashboard, pipeline DSL, status page | One workflow file. `go test ./... && golangci-lint run`. |
| Doc-validation framework, schema diff tool | `grep`. Edit the files. |
| Query builder, ORM, schema reflection layer | One `const taskColumns = "..."`. |
| Configurable reaper daemon, cron integration, retention policy DSL | One `UPDATE` on startup. One `prune` command. |
| Hook output capture framework, buffering, tee-ing | `cmd.Stdout = os.Stderr`. |
| Plugin system, hook SDK, extension API | Executables in `.d/`. That's the interface. |
| Metrics exporter, tracing framework, dashboard | `slog.Info` on key transitions. `--debug` flag. |
| Migration framework, version negotiation, rollback | One `schema.sql` + one `migrations.go`. Test both. |

**Rule:** Fix the bug. Don't architect around it. If the fix is >50 lines, you're overbuilding.

---

## Scope

| Field | Value |
|---|---|
| Language | Go |
| Non-test LOC | ~4,400 (`internal/` + `cmd/`) |
| Test LOC | ~2,800 |
| Key dirs | `cmd/taskbind/`, `internal/task/`, `internal/bootstrap/embed/skills/` |
| DB | SQLite (WAL mode, `modernc.org/sqlite`) |
| CLI framework | Cobra |

---

## 1. CI Gate

### 1.1 Test CI
| What | How to verify | Accept |
|---|---|---|
| Tests run in CI | `ls .github/workflows/` → grep for `go test` | PASS: workflow runs `go test ./...`. FAIL: no test workflow. |

**Result: FAIL** — only `release.yml` + `pages.yml`. No `go test` anywhere.

### 1.2 Lint CI
| What | How to verify | Accept |
|---|---|---|
| Linter runs in CI | `ls .github/workflows/` → grep for `golangci-lint` | PASS: workflow runs lint. FAIL: no lint workflow. |

**Result: FAIL** — `.golangci.yml` exists but never executed in CI.

### 1.3 Convergence Test (Docs ↔ Code)
| What | How to verify | Accept |
|---|---|---|
| Every doc-claimed command exists in `cmd/` | For each command in README + skill tables: `grep -r "<command>" cmd/taskbind/` | PASS: all found. FAIL: any missing. |
| Every doc-claimed event is fired in `internal/` | For each event type in docs: `grep -r "insertEvent\|runHook" internal/task/` → check event type string exists | PASS: all fired. FAIL: any never fired. |
| Every doc-claimed column exists in `schema.sql` | For each column in AUDIT.md/README: `grep "<column>" schema.sql` | PASS: all found. FAIL: any missing. |

**Result: FAIL** — `batch set-priority`, `batch set-project` in README/skills, not in `cmd/`. `task.priority_updated`, `task.project_updated` in skills, never fired. `ttl_seconds`, `idx_events_ttl` in AUDIT.md, not in `schema.sql`.

**YAGNI fix:** One grep test in CI. Not a doc-validation framework.

---

## 2. Schema Integrity

### 2.1 Single Source of Truth
| What | How to verify | Accept |
|---|---|---|
| Schema defined in one place | `grep -rl "CREATE TABLE" --include="*.go" --include="*.sql"` | PASS: 1 file (`schema.sql`). FAIL: >1 file. |
| Migrations tested with old DB | `grep -r "migrat" *_test.go` → check if test opens old DB dump | PASS: old-DB migration test exists. FAIL: only fresh-DB tests. |

**Result: FAIL** — schema mutations in `schema.sql` + `migrations.go` + `sqlite.go` (3 places). No old-DB migration test.

**YAGNI fix:** Don't build migration framework. Test: dump old schema, run `Open()`, assert columns match `schema.sql`.

### 2.2 ID Monotonicity
| What | How to verify | Accept |
|---|---|---|
| `task_seq` never decreases | Read `sqlite.go` `Open()`: `UPDATE task_seq SET next_id = ...` | PASS: `MAX(next_id, MAX(id))` — seed up only. FAIL: `next_id = MAX(id)` — can decrease on delete. |
| Test covers delete + re-seed | `grep -r "delete.*seq\|seq.*delete\|re-?seed" *_test.go` | PASS: test exists. FAIL: no test. |

**Result: FAIL** — `next_id = (SELECT COALESCE(MAX(CAST(substr(id,6) AS INTEGER)), 0) FROM tasks)` — reseeds downward on delete. No test.

**YAGNI fix:** `MAX(next_id, (SELECT COALESCE(MAX(...), 0) FROM tasks))`. One line. Add test: delete task, reopen, assert next_id didn't drop.

### 2.3 Invariants Enforced by Schema
| What | How to verify | Accept |
|---|---|---|
| "Immutable" columns have CHECK/TRIGGER | For each immutability claim in docs: `grep "CHECK\|TRIGGER" schema.sql` near column | PASS: constraint exists. FAIL: enforced only in app code. |

**Result: FAIL** — `claimed_by` "immutable" enforced only by `CASE WHEN claimed_by IS NULL THEN ? ELSE claimed_by END` in two UPDATEs. No CHECK, no TRIGGER.

**YAGNI fix:** Add `CHECK` constraint or `BEFORE UPDATE` trigger. Not a column-audit-log framework.

---

## 3. Concurrency Correctness

### 3.1 Write Serialization
| What | How to verify | Accept |
|---|---|---|
| Writer pinned to 1 conn | `grep "SetMaxOpenConns(1)" internal/task/sqlite.go` | PASS: found. FAIL: not found. |
| WAL mode enabled | `grep "WAL\|_journal_mode=wal" internal/task/sqlite.go` | PASS: WAL enabled. FAIL: not enabled. |
| Busy timeout set | `grep "busy_timeout" internal/task/sqlite.go` | PASS: >0. FAIL: not set. |

**Result: PASS** — `SetMaxOpenConns(1)`, WAL mode, `busy_timeout=5000` (writer) + `10000` (reader).

### 3.2 Retry Consistency
| What | How to verify | Accept |
|---|---|---|
| All write paths use `retryOnBusy` | For each: Dispatch, ClaimBatch, ClaimByID, Complete, Block, Approve, SetPriority → `grep -A5 "func.*Dispatch\|func.*Claim\|func.*Complete\|func.*Block\|func.*Approve\|func.*SetPriority" internal/task/service.go` → check for `retryOnBusy` | PASS: all use retryOnBusy. FAIL: any skips it. |

**Result: FAIL** — `Dispatch` does NOT use `retryOnBusy`. All other write paths do. Inconsistent.

**YAGNI fix:** Wrap `Dispatch`'s `BeginTx`/`Commit` in `retryOnBusy`. 3 lines.

### 3.3 Concurrency Tests
| What | How to verify | Accept |
|---|---|---|
| Claim race test | `grep -l "TestClaimNextAtomic\|TestClaimBatch.*[Rr]ace\|concurrent.*claim" *_test.go` | PASS: test exists. FAIL: no test. |
| Sequence race test | `grep -l "TestSequenceStability\|concurrent.*seq\|seq.*concurrent" *_test.go` | PASS: test exists. FAIL: no test. |
| Dependency concurrency test | `grep -l "TestDependsOnConcurrent\|dep.*concurrent\|concurrent.*dep" *_test.go` | PASS: test exists. FAIL: no test. |
| Hook rollback test | `grep -l "TestHookDoesNotFireOnRollback\|rollback.*hook\|hook.*rollback" *_test.go` | PASS: test exists. FAIL: no test. |

**Result: PASS** — `TestClaimNextAtomic`, `TestSequenceStability`, `TestDependsOnConcurrent`, `TestHookDoesNotFireOnRollback` all exist.

---

## 4. Hook Safety

### 4.1 Output Isolation
| What | How to verify | Accept |
|---|---|---|
| Hook stdout ≠ CLI stdout | Read `internal/task/hooks.go`: `cmd.Stdout = os.Stdout` or `cmd.Stdout = os.Stderr`? | PASS: `os.Stderr` or buffer. FAIL: `os.Stdout`. |
| Test covers stdout isolation | `grep -r "hook.*stdout\|stdout.*hook\|hook.*json\|json.*hook" *_test.go` | PASS: test exists. FAIL: no test. |

**Result: FAIL** — `hooks.go:82`: `cmd.Stdout = os.Stdout`. CLI `writeJSON` also writes to `os.Stdout`. First hook that logs → corrupts JSON. No test.

**YAGNI fix:** `cmd.Stdout = os.Stderr`. One line. Test: run hook that prints, assert JSON output clean.

### 4.2 Lifecycle
| What | How to verify | Accept |
|---|---|---|
| Hooks fire after tx commit | Read `internal/task/service.go`: `runHook` called after `tx.Commit()`? | PASS: after commit. FAIL: before commit. |
| SIGTERM drains hooks (not just `events tail`) | `grep -r "signal\|SIGTERM\|os.Signal" cmd/taskbind/` → check `main.go` or root command | PASS: main handles SIGTERM + drains hooks. FAIL: only `events tail` handles signals. |

**Result: MIXED** — Hooks fire after commit (PASS). Only `events tail` installs signal handler; `dispatch`/`claim`/`complete` orphan hook goroutines on SIGTERM (FAIL).

**YAGNI fix:** `signal.NotifyContext` in `main.go`. Not a process supervisor.

---

## 5. Data Integrity

### 5.1 Lease Reclamation
| What | How to verify | Accept |
|---|---|---|
| Expired leases reclaimed automatically | `grep -r "lease_until.*<\|expired.*lease\|reaper\|reclaim\|sweep.*IN_PROGRESS" --include="*.go"` | PASS: startup sweep or reaper command. FAIL: only on-demand (next claimer). |
| Test covers reclamation | `grep -r "expired.*lease.*test\|reaper.*test\|reclaim.*test\|sweep.*test" *_test.go` | PASS: test exists. FAIL: no test. |

**Result: FAIL** — `Stats` reports `ExpiredLeases` but nothing acts on them. Reclamation only when another agent claims same role. No reaper, no startup sweep. No test.

**YAGNI fix:** `UPDATE tasks SET status='TODO', assigned_agent=NULL WHERE status='IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP` on `Open()`. One line. Add test.

### 5.2 Events Retention
| What | How to verify | Accept |
|---|---|---|
| Events table has TTL or auto-prune | `grep "ttl\|retention\|auto.*prune\|sweep.*event" --include="*.go" --include="*.sql"` | PASS: TTL column + sweeper or auto-prune. FAIL: only manual `prune` command. |
| Docs don't claim TTL that doesn't exist | `grep -r "ttl\|auto.*expire\|expire.*day\|expire.*after" README.md AUDIT.md internal/bootstrap/embed/skills/` → cross-check with `schema.sql` | PASS: docs match code. FAIL: docs claim TTL, code has none. |

**Result: FAIL** — No TTL column, no auto-prune. Only manual `taskbind prune --before 30d`. README + AUDIT.md + `taskbind.md` skill all claim events auto-expire in 3 days. Fiction.

**YAGNI fix:** Delete TTL claims from docs. Not a retention policy framework.

### 5.3 Batch Atomicity
| What | How to verify | Accept |
|---|---|---|
| Atomic batch option exists | `grep -r "atomic\|all-or-nothing\|rollback.*batch\|batch.*rollback\|--atomic" --include="*.go"` | PASS: `--atomic` flag or rollback-on-any-failure. FAIL: only partial-success batch. |

**Result: FAIL** — `BatchComplete` commits partial successes. No `--atomic` flag. No rollback-on-any-failure.

**YAGNI fix:** Add `--atomic` flag to `BatchComplete`: wrap all items in one tx, rollback on first error. Not a saga orchestrator.

---

## 6. SELECT Column Duplication

### 6.1 Centralized Column List
| What | How to verify | Accept |
|---|---|---|
| One SELECT column constant | `grep -r "taskColumns\|const.*SELECT\|columnList\|allColumns" --include="*.go"` | PASS: single const used everywhere. FAIL: duplicated string literals. |
| Count distinct SELECT strings | `grep -roh 'SELECT .* FROM tasks' --include="*.go" | sort -u | wc -l` | PASS: 1 (all use const). FAIL: >1. |

**Result: FAIL** — 6 distinct copies of the 12-column SELECT list. No shared constant. Schema change → must edit all 6 in sync. No test asserts they're identical.

**YAGNI fix:** `const taskColumns = "id, title, description, status, role_boundary, project, priority, assigned_agent, lease_until, created_at, updated_at, claimed_by"` in `task.go`. Use in all 6 sites. Not a query builder.

---

## 7. Observability

### 7.1 Non-Debug Logging
| What | How to verify | Accept |
|---|---|---|
| Key events logged without `--debug` | `grep -r "slog.Info\|slog.Warn" --include="*.go"` → check if gated by `if cfg.Debug` | PASS: claim/complete/hook fire logged always. FAIL: all logging gated behind `--debug`. |

**Result: FAIL** — All `slog.Info`/`slog.Warn` gated behind `--debug`. Normal operation: zero observability.

**YAGNI fix:** `slog.Info("claimed", "task", id, "agent", agent)` without debug gate on key transitions. Not a metrics framework.

### 7.2 Health Check
| What | How to verify | Accept |
|---|---|---|
| Doctor/health command | `ls cmd/taskbind/` → grep for `doctor\|health\|check` | PASS: command exists. FAIL: no health check. |

**Result: FAIL** — No `doctor` or health check command.

**YAGNI fix:** `taskbind doctor` that prints: WAL size, migration versions, orphaned lease count, events table row count. One command, ~30 lines. Not a monitoring dashboard.

---

## 8. Dependency Hygiene

### 8.1 Clean go.mod
| What | How to verify | Accept |
|---|---|---|
| Direct deps ≤ 5 | `grep -c "require (" go.mod` → count direct block entries | PASS: ≤5. FAIL: >5. |
| No CGo required | `grep "cgo\|CGO" go.mod` | PASS: not found. FAIL: CGo required. |

**Result: PASS** — `cobra` + `modernc.org/sqlite` + few indirects. Pure Go, no CGo.

### 8.2 No Foreign Lockfiles at Root
| What | How to verify | Accept |
|---|---|---|
| Root lockfiles match project language | `ls package-lock.json pnpm-lock.yaml yarn.lock Cargo.lock 2>/dev/null` at repo root | PASS: none, or only matching language. FAIL: foreign lockfile at root. |

**Result: FAIL** — `pnpm-lock.yaml` + `tsconfig.json` at Go repo root (belong to `docs/` Astro site).

**YAGNI fix:** Move `docs/` to separate repo, or pin lockfile under `docs/`. Not a monorepo tool.

### 8.3 No Build Artifacts or AI Exhaust Committed
| What | How to verify | Accept |
|---|---|---|
| No `test-results/` in git | `git ls-files test-results/` | PASS: empty. FAIL: files committed. |
| No `feedbacks/` critique dumps | `ls feedbacks/ 2>/dev/null | wc -l` | PASS: 0. FAIL: >0. |
| No stray extension files | `ls .pi/extensions/ 2>/dev/null` | PASS: empty or doesn't exist. FAIL: files present. |

**Result: FAIL** — `test-results/.last-run.json` committed. 8 `feedbacks/*.txt` files. `.pi/extensions/protocol-guard.ts` stray file.

**YAGNI fix:** `git rm --cached test-results/.last-run.json feedbacks/*.txt .pi/extensions/protocol-guard.ts`. Add to `.gitignore`. Not a repo cleaner tool.

---

## 9. Security

### 9.1 SQL Injection
| What | How to verify | Accept |
|---|---|---|
| All queries parameterized | `grep -rn "fmt.Sprintf.*SELECT\|fmt.Sprintf.*INSERT\|fmt.Sprintf.*UPDATE\|fmt.Sprintf.*DELETE" --include="*.go"` | PASS: 0 results, or only allowlisted table names. FAIL: unguarded interpolation. |

**Result: PASS** — All user input uses `?` placeholders. Two interpolation sites (`pruneTable`, `LIMIT %d`) are allowlist-guarded.

### 9.2 Self-Review Gate Hardness
| What | How to verify | Accept |
|---|---|---|
| Gate not bypassable by env var | `grep -r "ALLOW_SELF_REVIEW\|BYPASS.*REVIEW\|SKIP.*REVIEW\|DISABLE.*REVIEW" --include="*.go"` | PASS: no env bypass. FAIL: env var disables gate. |
| Gate not bypassable by NULL `claimed_by` | Read `internal/task/review.go` `checkSelfReview`: does it block when `claimed_by IS NULL`? | PASS: NULL → gate blocks. FAIL: NULL → gate passes. |
| `claimed_by` immutable in schema | `grep "claimed_by.*CHECK\|CHECK.*claimed_by\|TRIGGER.*claimed_by" schema.sql` | PASS: constraint exists. FAIL: only app-code enforcement. |

**Result: FAIL** — `TASKBIND_ALLOW_SELF_REVIEW=true` disables gate. NULL `claimed_by` (task created directly in `IN_REVIEW`) → gate passes. `claimed_by` mutable by any code with DB access. Three bypass vectors.

**YAGNI fix:** Remove env bypass. Make `checkSelfReview` fail on NULL `claimed_by`. Add CHECK/TRIGGER. Not an auth framework.

---

## 10. Query Plan

### 10.1 Claim Query Index Usage
| What | How to verify | Accept |
|---|---|---|
| No `OR` that defeats claim index | Read `ClaimBatch` WHERE clause: `OR` across indexed `status` column? | PASS: `UNION ALL` of two seeks, or single `=` condition. FAIL: `OR` on status. |
| EXPLAIN QUERY PLAN tested | `grep -r "EXPLAIN\|query.*plan\|index.*scan" *_test.go` | PASS: test runs EXPLAIN and asserts index usage. FAIL: no query plan test. |

**Result: FAIL** — `WHERE role_boundary = ? AND (status = 'TODO' OR (status = 'IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP))` — `OR` defeats `idx_tasks_claim`. No EXPLAIN test.

**YAGNI fix:** Split into two `SELECT ... UNION ALL ...` queries, one per status arm. Add EXPLAIN QUERY PLAN test. Not an index advisor.

---

## 11. Docs vs Code Divergence Table

Mechanical cross-check: every claim in docs must exist in code.

| Doc claim | Source file | Code check | Result |
|---|---|---|---|
| `batch set-priority --ids --priority` | README.md, `taskbind.md` skill | `grep -r "batch.*set-priority\|set-priority.*batch" cmd/taskbind/` | **FAIL** — not found |
| `batch set-project --ids --project` | README.md | `grep -r "batch.*set-project\|set-project.*batch" cmd/taskbind/` | **FAIL** — not found |
| `task.priority_updated` hook event | `taskbind.md` skill | `grep -r "priority_updated\|priority.*updated" internal/task/` | **FAIL** — never fired |
| `task.project_updated` hook event | `taskbind.md` skill | `grep -r "project_updated\|project.*updated" internal/task/` | **FAIL** — never fired |
| Events expire after 3 days (`ttl_seconds`) | README.md, AUDIT.md, `taskbind.md` skill | `grep "ttl_seconds" schema.sql` | **FAIL** — column doesn't exist |
| `idx_events_ttl` index | AUDIT.md | `grep "idx_events_ttl" schema.sql` | **FAIL** — index doesn't exist |
| `internal/bootstrap/taskbind_extension.go` | README.md project structure | `ls internal/bootstrap/taskbind_extension.go` | **FAIL** — file doesn't exist |
| "Requires Go 1.24 or later" | README.md | `head -1 go.mod` | **FAIL** — `go 1.25.0` |
| "Lease expires → next claim-next reclaims" (automatic) | README.md | `grep -r "reaper\|sweep\|reclaim.*startup\|Open.*IN_PROGRESS" internal/task/` | **FAIL** — on-demand only |

**Verdict:** 9 doc claims, 9 failures. Protocol spec and protocol implementation have diverged.

---

## Summary

### ✅ What Works (PASS)

| # | Item | Section |
|---|---|---|
| 1 | Writer pinned to 1 conn + WAL + busy_timeout | 3.1 |
| 2 | `retryOnBusy` with exponential backoff + jitter | 3.2 (partial — Dispatch missing) |
| 3 | `ClaimBatch` Serializable + `RowsAffected()` race detection | 3.2 |
| 4 | `nextID` via `UPDATE ... RETURNING` (atomic) | 2.2 (partial — re-seed bug) |
| 5 | Hooks fire after tx commit, rollback-doesn't-fire tested | 4.2 |
| 6 | Concurrency tests exist + pass | 3.3 |
| 7 | Lean `go.mod` (cobra + pure-Go SQLite, no CGo) | 8.1 |
| 8 | All queries parameterized, no SQL injection | 9.1 |
| 9 | Cycle-detection DFS + dispatch-time cycle check | — |

### ❌ What Doesn't (FAIL)

| # | Item | Section | Severity |
|---|---|---|---|
| 1 | No test CI | 1.1 | **CRITICAL** |
| 2 | No lint CI | 1.2 | HIGH |
| 3 | Docs ↔ code divergence (9 false claims) | 1.3, 11 | **CRITICAL** |
| 4 | Schema in 3 places, no old-DB migration test | 2.1 | HIGH |
| 5 | `task_seq` re-seeds downward on delete | 2.2 | **CRITICAL** |
| 6 | `claimed_by` "immutable" is convention, not constraint | 2.3 | MEDIUM |
| 7 | `Dispatch` skips `retryOnBusy` | 3.2 | MEDIUM |
| 8 | Hook stdout → `os.Stdout` (corrupts JSON) | 4.1 | **CRITICAL** |
| 9 | No SIGTERM handling outside `events tail` | 4.2 | MEDIUM |
| 10 | No lease reaper (orphaned `IN_PROGRESS` forever) | 5.1 | **CRITICAL** |
| 11 | Events table unbounded, docs claim TTL that doesn't exist | 5.2 | HIGH |
| 12 | No atomic batch option | 5.3 | MEDIUM |
| 13 | 6 duplicated SELECT column strings | 6.1 | HIGH |
| 14 | Zero observability without `--debug` | 7.1 | MEDIUM |
| 15 | No `doctor`/health check command | 7.2 | MEDIUM |
| 16 | `pnpm-lock.yaml` at Go repo root | 8.2 | LOW |
| 17 | Build artifacts + AI exhaust committed | 8.3 | LOW |
| 18 | Self-review gate bypassable 3 ways | 9.2 | MEDIUM |
| 19 | `ClaimBatch` `OR` defeats claim index | 10.1 | HIGH |

### 🔜 What's Next (Top 5 by severity × leverage)

| # | Fix | Section | Effort | YAGNI guard |
|---|---|---|---|---|
| 1 | **Add CI** — `go test ./...` + `golangci-lint` + convergence grep test | 1.1, 1.2, 1.3 | 1 file: `.github/workflows/ci.yml` | Don't build CI dashboard. One workflow file. |
| 2 | **Reconcile docs** — delete 9 false claims from README + AUDIT + skills. `git rm` artifacts. | 11, 8.3 | Edit 4 files | Don't build doc validator. `grep` + edit. |
| 3 | **Fix hook stdout** — `cmd.Stdout = os.Stderr` + regression test | 4.1 | 1 line + 1 test | Don't build hook output framework. |
| 4 | **Centralize SELECT columns** — `const taskColumns` in `task.go`, use in all 6 sites | 6.1 | 1 const + 6 edits | Don't build query builder. One constant. |
| 5 | **Fix data integrity** — monotonic `task_seq`, startup reaper, `UNION ALL` claim query | 2.2, 5.1, 10.1 | ~30 lines + 3 tests | Don't build reaper daemon. One `UPDATE` on `Open()`. |

### One-Line

*Senior-grade SQLite core, spec layer drifted from engine across README/AUDIT/skills — no CI, no convergence test, no reaper. Engine works. Spec doesn't. For a coordination protocol, that's the same as not working.*
