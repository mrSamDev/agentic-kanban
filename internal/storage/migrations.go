package storage

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

// runMigrations applies schema additions that are not in the base schema.sql.
// Uses idempotent ADD COLUMN IF NOT EXISTS via pragma_table_info checks.
// Migration version tracking prevents re-application on subsequent opens.
//
// When schema_migrations table already shows all known versions (seeded on fresh DB),
// runMigrations is a no-op.
func runMigrations(db *sql.DB, debug bool) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read current migration version: %w", err)
	}

	// Fresh DB: tasks already exist → seeded from current schema.sql already.
	// Just mark all known versions as applied.
	if current == 0 {
		var hasTasks bool
		_ = db.QueryRow(`SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&hasTasks)
		if hasTasks {
			for _, v := range []int{1, 2, 4, 5, 6, 7} {
				if _, err := db.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES(?)`, v); err != nil {
					return fmt.Errorf("seed migration %d: %w", v, err)
				}
			}
			if debug {
				slog.Info("schema_migrations seeded", "version_count", 6)
			}
			return nil
		}
	}

	migrations := []struct {
		version int
		desc    string
	}{
		{2, "add tasks.project column"},
		{4, "add tasks.depends_on column"},
		{5, "add tasks.claimed_by column"},
		{6, "rebuild claim indexes with lease_until"},
		{7, "migrate depends_on text into task_dependencies"},
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.version, err)
		}

		switch m.version {
		case 2:
			if err := addColumnIfMissing(tx, "tasks", "project", "TEXT NOT NULL DEFAULT 'default'"); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration %d: %w", m.version, err)
			}
		case 4:
			if err := addColumnIfMissing(tx, "tasks", "depends_on", "TEXT"); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration %d: %w", m.version, err)
			}
		case 5:
			if err := addColumnIfMissing(tx, "tasks", "claimed_by", "TEXT"); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration %d: %w", m.version, err)
			}
		case 6:
			if _, err := tx.Exec("DROP INDEX IF EXISTS idx_tasks_claim"); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration 6 drop index: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_tasks_claim
				ON tasks(role_boundary, status, priority, created_at, lease_until)`); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration 6 create claim index: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_tasks_claim_project
				ON tasks(role_boundary, project, status, priority, created_at, lease_until)`); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration 6 create claim_project index: %w", err)
			}
		case 7:
			if err := migrateDependsOnData(tx); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration 7: %w", err)
			}
		}

		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, m.version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d record: %w", m.version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %d commit: %w", m.version, err)
		}

		if debug {
			slog.Info("migration applied", "version", m.version, "desc", m.desc)
		}
	}

	return nil
}

func addColumnIfMissing(tx *sql.Tx, table, column, definition string) error {
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

// migrateDependsOnData reads existing depends_on TEXT and inserts into task_dependencies.
func migrateDependsOnData(tx *sql.Tx) error {
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
		for _, p := range strings.Split(dependsOn, ",") {
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