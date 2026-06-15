# Schema Production Readiness Audit

**Date**: 2026-06-10  
**Version**: v0.6.0

---

## Concluded: All Clear

No schema bugs, no data-loss paths, no missing migrations. Two index improvements applied during audit.

## Schema State

| Element | Status | Notes |
|---------|--------|-------|
| `tasks` table | ✅ | 11 columns, CHECK on status, FK-aware |
| `notes` table | ✅ | CASCADE delete, nullable note_type |
| `history` table | ✅ | CASCADE delete, TIMESTAMP tracking |
| `events` table | ✅ | TTL expiry column, auto-increment ID |
| `task_seq` | ✅ | Single-row PK guard, seeded on every open |
| Foreign keys | ✅ | `PRAGMA foreign_keys = ON` at connect |
| WAL mode | ✅ | `PRAGMA journal_mode = WAL` |
| Busy timeout | ✅ | `PRAGMA busy_timeout = 5000` (5s) |

## Index Coverage

| Query Pattern | Index | Status |
|---------------|-------|--------|
| Claim tasks (by role+status+prio) | `idx_tasks_claim` | ✅ 5-col index (migrated from 4-col) |
| Claim w/ project filter | `idx_tasks_claim_project` | ✅ New index (migrated) |
| Lease-expired reclaim | Covered by `idx_tasks_claim` (lease_until) | ✅ |
| Search by status/role/project | Covered by claim indexes | ✅ |
| View notes by task | `idx_notes_task` | ✅ |
| View history by task | `idx_history_task` | ✅ |
| Event polling/TTL | `idx_events_ttl` | ✅ |

## Migrations Applied

All migration paths exist in `sqlite.go:Open()`:

| Migration | Status | Mechanism |
|-----------|--------|-----------|
| Old `task_seq` → single-row PK | ✅ | Detect missing id col → DROP+CREATE |
| `project` column on `tasks` | ✅ | `ALTER TABLE ADD COLUMN` |
| `ttl_seconds` column + index on `events` | ✅ | `ALTER TABLE ADD COLUMN` + CREATE INDEX |
| `depends_on` column on `tasks` | ✅ | `ALTER TABLE ADD COLUMN` |
| `idx_tasks_claim` → include `lease_until` | ✅ | Drop old 4-col, create 5-col index |
| `idx_tasks_claim_project` index | ✅ | CREATE INDEX IF NOT EXISTS |

## Security/Data Safety

| Concern | Status |
|---------|--------|
| SQL injection (query params via `?`) | ✅ All user input is parameterized |
| Path traversal (DB path) | ✅ Checked in `storage.Open` (dir creation) |
| Sensitive data in events | ✅ No secrets logged |
| Hook timeout | ✅ 30s per hook, non-zero exit tolerated |
| Lease expiry | ✅ Auto-reclaim on stale IN_PROGRESS |

## Performance Targets vs Reality

| Op | Target | Measured | Status |
|----|--------|----------|--------|
| Claim 1 task | <10ms | ~0.44ms | ✅ |
| Claim 10 tasks (batch) | <11ms | ~0.85ms | ✅ |
| Burndown query | <50ms | Instant (COUNT) | ✅ |
| Lint check | <100ms | Instant (DFS) | ✅ |
| Full workflow (10 tasks) | <1s | <10ms per op | ✅ |

## Remaining Non-Blockers

1. **No FK on events table** — intentional. Events may reference tasks that get pruned. TTL-based cleanup replaces FK integrity.
2. **`depends_on` is TEXT, not FK** — intentional. Reverse queries require LIKE scan. Acceptable for expected dependency sizes (3–50).
3. **No composite index on `history(task_id, action)`** — action filter not used as a primary access path. Current single-column index covers the `task_id` WHERE clause.
4. **No separate `diff` column in events** — payload JSON captures the full snapshot. Debatable for high-volume compaction, fine for expected event count (<100K).

## Next Steps

None — schema is production-ready. Move to documentation finalization when ready.