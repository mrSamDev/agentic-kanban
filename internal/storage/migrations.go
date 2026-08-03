package storage

import (
	"database/sql"
	"fmt"
	"log/slog"
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
			for _, v := range []int{1, 2, 5, 6, 8} {
				if _, err := db.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES(?)`, v); err != nil {
					return fmt.Errorf("seed migration %d: %w", v, err)
				}
			}
			if debug {
				slog.Info("schema_migrations seeded", "version_count", 4)
			}
			return nil
		}
	}

	migrations := []struct {
		version int
		desc    string
	}{
		{2, "add tasks.project column"},
		{5, "add tasks.claimed_by column"},
		{6, "rebuild claim indexes with lease_until"},
		{8, "add tasks.description column"},
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
		case 8:
			if err := addColumnIfMissing(tx, "tasks", "description", "TEXT NOT NULL DEFAULT ''"); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration 8: %w", err)
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