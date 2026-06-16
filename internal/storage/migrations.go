package storage

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

// migration is a numbered DDL/DML step applied after base schema.
// Either upSQL (pure SQL) or upFunc (Go function) must be set.
type migration struct {
	version int
	upSQL   string
	upFunc  func(*sql.Tx) error
}

// migrations are applied in version order after the base schema runs.
// Migration 1 (task_seq PK fix) runs inline in Open() before schema apply.
// These numbered migrations start at 2 for idempotent post-schema changes.
// Additive migrations (ADD COLUMN) use Go funcs with pragma_table_info guard.
var migrations = []migration{
	{
		version: 2,
		upFunc:  addMissingColumn("tasks", "project", "TEXT NOT NULL DEFAULT 'default'"),
	},
	{
		version: 3,
		upFunc: addMissingColumn("events", "ttl_seconds", "INTEGER DEFAULT 259200"),
	},
	{
		version: 4,
		upFunc: addMissingColumn("tasks", "depends_on", "TEXT"),
	},
	{
		version: 5,
		upFunc: addMissingColumn("tasks", "claimed_by", "TEXT"),
	},
	{
		version: 6,
		upFunc: func(tx *sql.Tx) error {
			// Rebuild idx_tasks_claim with lease_until. Drop old if present.
			if _, err := tx.Exec("DROP INDEX IF EXISTS idx_tasks_claim"); err != nil {
				return fmt.Errorf("drop old idx_tasks_claim: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_tasks_claim
				ON tasks(role_boundary, status, priority, created_at, lease_until)`); err != nil {
				return fmt.Errorf("create idx_tasks_claim: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_tasks_claim_project
				ON tasks(role_boundary, project, status, priority, created_at, lease_until)`); err != nil {
				return fmt.Errorf("create idx_tasks_claim_project: %w", err)
			}
			return nil
		},
	},
	{
		version: 7,
		upFunc: migrateDependsOn,
	},
}

// addMissingColumn returns a migration func that adds a column if it doesn't exist.
// This is idempotent: safe to run against DBs that already have the column.
func addMissingColumn(table, column, definition string) func(*sql.Tx) error {
	return func(tx *sql.Tx) error {
		var exists bool
		err := tx.QueryRow(
			`SELECT COUNT(*) > 0 FROM pragma_table_info(?) WHERE name = ?`,
			table, column,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check %s.%s: %w", table, column, err)
		}
		if exists {
			return nil
		}
		_, err = tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
		if err != nil {
			return fmt.Errorf("add %s.%s: %w", table, column, err)
		}
		return nil
	}
}

// runMigrations applies any pending numbered migrations in order.
// Each migration runs in its own transaction so partial failures don't
// roll back earlier migrations.
//
// When schema_migrations is empty but tasks already exists (fresh DB),
// seed with all migration versions instead of replaying.
func runMigrations(db *sql.DB, debug bool) error {
	// Ensure schema_migrations table exists (may not on pre-migration DBs).
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// Get current migration version
	var current int
	err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current)
	if err != nil {
		return fmt.Errorf("read current migration version: %w", err)
	}

	// Fresh-DB seed: schema_migrations empty but tasks exists → DB created from current schema.
	if current == 0 {
		var hasTasks bool
		_ = db.QueryRow(`SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&hasTasks)
		if hasTasks {
			for _, m := range migrations {
				if _, err := db.Exec(
					`INSERT OR IGNORE INTO schema_migrations(version) VALUES(?)`, m.version,
				); err != nil {
					return fmt.Errorf("seed migration %d: %w", m.version, err)
				}
			}
			if debug {
				slog.Info("schema_migrations seeded", "version_count", len(migrations))
			}
			return nil
		}
	}

	// Apply pending migrations in version order.
	for _, m := range migrations {
		if m.version <= current {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.version, err)
		}

		if m.upFunc != nil {
			if err := m.upFunc(tx); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration %d: %w", m.version, err)
			}
		} else if m.upSQL != "" {
			if _, err := tx.Exec(m.upSQL); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration %d: %w", m.version, err)
			}
		}

		if _, err := tx.Exec(
			`INSERT INTO schema_migrations(version) VALUES(?)`, m.version,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d record: %w", m.version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %d commit: %w", m.version, err)
		}

		if debug {
			slog.Info("migration applied", "version", m.version)
		}
	}

	return nil
}

// migrateDependsOn reads existing depends_on TEXT and inserts into task_dependencies.
// Keeps the column for backwards compat; a future v1 migration will drop it.
func migrateDependsOn(tx *sql.Tx) error {
	var hasDependsOn bool
	err := tx.QueryRow(`SELECT COUNT(*) > 0 FROM pragma_table_info('tasks') WHERE name = 'depends_on'`).Scan(&hasDependsOn)
	if err != nil {
		return fmt.Errorf("check depends_on column: %w", err)
	}
	if !hasDependsOn {
		return nil
	}

	rows, err := tx.Query(`SELECT id, depends_on FROM tasks WHERE depends_on IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("read depends_on data: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, dependsOn string
		if err := rows.Scan(&id, &dependsOn); err != nil {
			return fmt.Errorf("scan depends_on row: %w", err)
		}
		parts := strings.Split(dependsOn, ",")
		for _, p := range parts {
			depID := strings.TrimSpace(p)
			if depID == "" {
				continue
			}
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO task_dependencies(task_id, depends_on_task_id) VALUES(?, ?)`,
				id, depID,
			); err != nil {
				return fmt.Errorf("insert dependency %s -> %s: %w", id, depID, err)
			}
		}
	}
	return rows.Err()
}