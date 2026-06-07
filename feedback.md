# Senior Engineer Codebase Review: `agent-kanban`

SQLite-backed coordination protocol for multi-agent systems. ~5,800 Go LOC, 36 files. Two direct deps (cobra + sqlite). Agents claim tasks via leases, flow TODO→IN_PROGRESS→BLOCKED→IN_REVIEW→DONE, emit events that fire shell hooks.

---

## What's Genuinely Good (6 items)

**1. SQLite concurrency primer is correct.** Single connection (`MaxOpenConns=1`), WAL mode, `busy_timeout=5000`, retry with exponential backoff + jitter. Someone understood SQLite's model and addressed it explicitly. (*storage/sqlite.go*, *helpers.go*)

**2. Serializable isolation for claims.** `ClaimBatch` uses `sql.LevelSerializable` to prevent double-assignment. Combined with lease-based ownership. Correct approach for shared-nothing agents on SQLite. (*claim.go*)

**3. Test quality is above average.** 1,172 lines covering: concurrent claims (10 goroutines), concurrent claims with deps, lease reclaim, self-review rejection, dependency blocking, hook edge cases (missing, non-zero exit, `.d/` ordering, non-executable scripts). These test real failure modes, not just happy paths.

**4. Minimal dependency footprint.** cobra + sqlite. No ORM, no HTTP, no config library, no logger, no protobuf. Every dep justified. (*go.mod*)

**5. Hook system with semaphore-capped goroutines.** `.d/` directory pattern (synchronous single hook + concurrent siblings) is well-designed. 20-goroutine semaphore prevents unbounded spawns. 30s timeout per hook. (*hooks.go*)

**6. README is honest about limits.** "50 concurrent agents", "skip it when...", "past 1000, SQLite is the bottleneck." Documents its ceiling. Communicates tradeoffs. Rare for alpha software.

---

## Structural Problems (9 items)

### `depends_on TEXT` — 1NF violation, biggest design debt

Dependencies stored as comma-separated strings: `"TASK-1,TASK-3,TASK-7"`. This means:
- No foreign key enforcement
- No index for reverse lookups ("what depends on TASK-X?")
- Every dep check parses and splits strings
- JOINs impossible — lint system manually rebuilds an adjacency map from the full task list

The README doesn't mention or justify this. Silent oversight. A `task_deps` join table is ~15 lines of schema and eliminates all string manipulation.

**Evidence:** `depends_on TEXT` in schema → `strings.Split()` in `hasUnmetDeps` and `detectCycles`. (*schema.sql*, *claim.go:hasUnmetDeps*, *lint.go*)

### Ad-hoc schema migrations

Three column-sniffing blocks in `storage.Open()` using `PRAGMA table_info`. No version table, no ordering, no rollback. README presents this as a feature ("schema changes happen automatically on open"). Works for 3 columns; will break at 15 when one migration conflicts with another or a NOT NULL column is added without a default.

**Verdict:** Acceptable for alpha with a bounded use case (≤50 agents). Must be replaced before beta.

### Config in context.Context

`Config{DBPath, Debug}` stuffed into `context.WithValue`. Anti-pattern in Go — config is global, context is request-scoped. Every handler does an unchecked type assertion that panics on mismatch. (*cmd/kanban/config.go*)

### `Burndown` is a dead abstraction

Calls `Stats()`, adds `PercentDone = done/total*100`. One call site. Duplicated types (`TaskStats` vs `BurndownStats`). Unnecessary. (*queries.go*)

### Event payload denormalization

Every event carries full title/project/priority as strings — duplicated across all events for the same task. 100 progress events = 100 copies of the title. (*events.go:eventPayload*)

### `joinStrings` — seven lines to avoid one import

Hand-rolled `strings.Join` in `prune.go`. Comment says "replacement for strings.Join to avoid import" — but `fmt` is already imported and `strings.Join` is used in 4 sibling files. Most visible AI artifact in the codebase. (*prune.go:61-70*)

### Stale AI planning comment

```go
// After Step 4 refactor: delegates to ClaimBatch(..., 1) for single-task claim.
```

LLM planning output in production code. Tells reader what code was *refactored*, not what code *does*. (*claim.go:17*)

### `ValidStatuses` is dead code

Exported `map[TaskStatus]bool`, used only for validation in `Search` — which is redundant because the SQL CHECK constraint already rejects invalid statuses at the DB level. Defense-in-depth that defends nothing. (*model.go*, *queries.go*)

### `parseLeaseTime` fails silently

Returns zero-value `time.Time{}` on parse failure with no error. If SQLite's datetime format changes, all `LeaseUntil` values become zero-values. Zero-value → always expired → task immediately reclaimable. Not data corruption, but subtle and hard to debug. (*helpers.go:parseLeaseTime*)

---

## Operational Gaps (9 items)

### Zero observability beyond `--debug`

No structured logging in any service method: no duration, no operation name, no task ID, no success/failure signal. The README says `--debug` is the observability story — which is honest for alpha but insufficient for any production deployment.

**Missing metrics:** claim latency, hook duration, busy retry count, lease expiry rate.

### Inline TTL cleanup on every event insert

`insertEvent` runs a full DELETE against the events table (index range scan on `idx_events_ttl`) after every write. Every append transaction pays this tax. Not documented in the README. Not tested for performance. Silently grows. (*events.go:insertEvent*)

### `context.Background()` in hooks — no graceful shutdown

Hook goroutines use `context.Background()` with 30s timeout. Not linked to parent context. When CLI exits, goroutines leak. (*hooks.go:33*)

### Package-level global semaphore

`var hookSem = make(chan struct{}, 20)` — shared across all Service instances. Cannot be reset or tuned. (*hooks.go:12*)

**Note:** This is intentional — system-wide cap prevents OOM on burst. Not a bug, but undocumented.

### No retry-path test coverage

`retryOnBusy` is never tested. No test injects `SQLITE_BUSY`. If the backoff overflows or the error-code check (`Code() == 5 || Code() == 6`) is wrong, it won't be caught.

### `/tmp/` for test databases

Tests write DBs to `/tmp/kanban-test-<pid>.db`. Leaks on crash. Collides on CI. `t.TempDir()` exists to solve this. (*service_test.go*)

### Swallowed error in batch operations

```go
n, _ := res.RowsAffected()
```

Silent data loss risk. If `RowsAffected()` errors (driver bug, WAL corruption, connection drop), the counter reports success but the event isn't queued. (*batch.go:24, 68*)

### `detectCycles` is O(n²) on claim path

DFS runs on adjacency map rebuilt from full task list every claim. Not mentioned in README. Will slow down noticeably past ~500 tasks with complex dependency chains. (*lint.go*)

### `loadTaskMetas` uses fragile string interpolation

`joinStrings(placeholders, ",")` builds IN clause. Currently safe (placeholders are `?`), but pattern breaks if ever changed. (*events.go*)

---

## Concurrency Concerns (3 items)

1. **Serializable isolation = all writers serialize.** Under 50+ concurrent agents, the busy_timeout + retry loop is the bottleneck. README acknowledges this.
2. **`ClaimBatch` caps at 100 candidates.** Magic number, undocumented. Should be a const or config flag. (*claim.go*)
3. **Single connection forfeits WAL's concurrent-reader benefit.** Readers block on writers. A pool with one writer + N readers would give better throughput.

---

## AI-Generated Code Smells (8 artifacts)

1. **`joinStrings`** — reinventing `strings.Join` when it's imported in 4 sibling files
2. **"After Step 4 refactor"** — LLM planning dialogue in production code
3. **Comment density spikes** — "Prefix 'TASK-' for human-readable IDs" explains what, not why
4. **`Burndown` → `Stats` delegation** — abstraction for AI's convenience
5. **Three event-payload constructors** — `eventPayload`, `loadTaskMetas`, `buildPayload` all do the same thing with different input sources. Never decided on the abstraction boundary
6. **Copy-paste batch code** — `BatchUpdatePriority` and `BatchUpdateProject` share 80% identical structure. One generic function with a column parameter should exist
7. **`task_seq` table** — `CREATE TABLE task_seq (next_id INTEGER)` + `INSERT OR IGNORE ... VALUES (0)` reinventing SQLite AUTOINCREMENT/SEQUENCE. Partial miss: gives cross-driver portability, but `RETURNING id` after INSERT would be cleaner.
8. **`parseLeaseTime` silent zero-value** — no error path on parse failure. Code trusts data format without verifying

**The density of these artifacts tells a story:** ~60% of Go code was AI-generated, then partially reviewed. The human reviewed the architecture correctly (WAL, Serializable, leases) but did not review the implementation details.

---

## The Verdict

**Engineer level:** Mid-level Go engineer (3-5 years) with genuine distributed-systems intuition, writing with heavy AI assistance. The architecture decisions (WAL, Serializable, leases, single connection, hook `.d/` pattern) show real experience. The README is demonstrably human-written — honest, bounded, tradeoff-aware. But the implementation gaps (`joinStrings`, stale AI planning comments, three-way event-payload constructors, `depends_on TEXT`, swallowed errors, zero observability, `/tmp/` test DBs) are what a senior engineer would catch in a 20-minute review pass.

**Production survival:** Small scale (≤50 agents, ≤10k events/day) — yes. The README honestly says this. At larger scale — no. The system knows its envelope, which is more mature than most alpha projects.

**What breaks first (in order):**
1. Inline TTL cleanup — silent write-amplification on every event insert
2. SQLite write-lock contention under concurrent claims — acknowledged, but no metrics to detect it
3. Swallowed errors in batch operations — silent data inconsistency that surfaces days later
4. `depends_on TEXT` — string-parsing-based constraints slow down with more tasks
5. `detectCycles` O(n²) — DFS on full task list rebuild slows complex dependency chains

---

## Top 5 Highest Priority Refactors

1. **Decouple event TTL cleanup from insert path** (*events.go:insertEvent*). Move the DELETE into a background goroutine or periodic command. Removes the biggest scalability ceiling.

2. **Add structured logging to every service method** (all `internal/task/*.go`). Operation name, task ID, agent, duration, error. Foundation for debugging, metrics, alerting. Without it, production is blind.

3. **Schema migration versioning** (*storage/sqlite.go*). Replace 3 column-sniffing blocks with a `schema_version` table + ordered migrations. ~10 lines. Eliminates future breakage.

4. **Normalize dependencies into a join table** (*model.go*, *schema.sql*). `CREATE TABLE task_deps (task_id TEXT, depends_on_id TEXT, PRIMARY KEY(...), FOREIGN KEY(...))`. Eliminates string parsing, enables indexed JOINs, removes ~40 lines of split/trim logic.

5. **Consolidate the three event-payload constructors** (*events.go*). Merge `eventPayload`, `loadTaskMetas`, `buildPayload` into one function. The three-way split is the clearest "didn't decide the abstraction boundary" signal in the codebase.

**Honorable mentions (3-minute fixes):**
- Delete `joinStrings`, import `strings.Join` in `prune.go` — removes most visible AI artifact
- Replace `/tmp/` with `t.TempDir()` in tests — prevents CI collisions and crash leaks
- Handle `n, err := res.RowsAffected()` instead of `n, _` in `batch.go` — prevents silent data loss
- Make `ClaimBatch` candidate cap (100) a const or config flag — documents the magic number
- Add error return to `parseLeaseTime` — makes datetime parsing failures visible

---

## What This Review Missed (Added Context)

| Original Claim | Correction |
|----------------|------------|
| `task_seq` reinvents AUTOINCREMENT "for no benefit" | Partial miss: gives cross-driver portability. Still: `RETURNING id` after INSERT would be cleaner. |
| `parseLeaseTime` silent nil → "data corruption risk" | Overstated: returns zero-value, not nil. Zero-value → always expired → task reclaimable. Subtle, not corruption. |
| Global semaphore "cannot be reset or tuned" | True, but intentional. System-wide cap prevents OOM on burst. Reset capability unnecessary. |
| Single connection "forfeits WAL's concurrent-reader benefit" | True, but WAL still helps. WAL allows 1 writer + unlimited readers without blocking. Single connection is for writer serialization. |
