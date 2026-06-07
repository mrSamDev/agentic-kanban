package storage

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaFS embed.FS

// DB wraps *sql.DB with a single connection so per-connection pragmas persist.
type DB struct {
	*sql.DB
	debug bool
}

func Open(path string, debug bool) (*DB, error) {
	// SQLite won't create intermediate dirs.
	dir := filepath.Dir(path)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create db dir %s: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if debug {
		slog.Info("db opened", "path", path)
	}

	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if debug {
		slog.Info("db WAL mode enabled")
	}

	if _, err := db.Exec("PRAGMA wal_autocheckpoint = 1000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set wal_autocheckpoint: %w", err)
	}
	if debug {
		slog.Info("db wal_autocheckpoint set", "value", 1000)
	}

	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign_keys: %w", err)
	}

	// --- task_seq migration (must run before schema apply) ---
	// Old task_seq had no id column and no PK, producing duplicate rows.
	// Drop it now so the new CREATE TABLE takes effect.
	// We detect old schema by checking if the id column is missing.
	var hasSeqID bool
	if err := db.QueryRow(`SELECT COUNT(*) > 0 FROM pragma_table_info('task_seq') WHERE name = 'id'`).Scan(&hasSeqID); err != nil {
		// Table may not exist yet on fresh DB; ignore.
	}
	if !hasSeqID {
		if _, err := db.Exec(`DROP TABLE IF EXISTS task_seq`); err != nil {
			db.Close()
			return nil, fmt.Errorf("drop old task_seq: %w", err)
		}
		if debug {
			slog.Info("old task_seq dropped, recreating with single-row PK")
		}
	}

	// Idempotent — all CREATE IF NOT EXISTS.
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("read embedded schema: %w", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if debug {
		slog.Info("db schema applied")
	}

	// Always seed task_seq from existing tasks on every open.
	// Without this, a DB opened by a version that didn't migrate old task_seq
	// keeps next_id = 0 and nextID() could generate IDs that collide with
	// pre-existing tasks from prior sessions.
	if _, err := db.Exec(`
		UPDATE task_seq SET next_id = (
			SELECT COALESCE(MAX(CAST(substr(id,6) AS INTEGER)), 0) FROM tasks
		) WHERE id = 1
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("seed task_seq: %w", err)
	}
	if debug {
		slog.Info("task_seq seeded from existing tasks")
	}

	var hasProject bool
	db.QueryRow(`SELECT COUNT(*) > 0 FROM pragma_table_info('tasks') WHERE name = 'project'`).Scan(&hasProject)
	if !hasProject {
		if _, err := db.Exec("ALTER TABLE tasks ADD COLUMN project TEXT NOT NULL DEFAULT 'default'"); err != nil {
			db.Close()
			return nil, fmt.Errorf("add project column: %w", err)
		}
		if debug {
			slog.Info("db project column migration applied")
		}
	}

	var hasTTL bool
	db.QueryRow(`SELECT COUNT(*) > 0 FROM pragma_table_info('events') WHERE name = 'ttl_seconds'`).Scan(&hasTTL)
	if !hasTTL {
		if _, err := db.Exec("ALTER TABLE events ADD COLUMN ttl_seconds INTEGER DEFAULT 259200"); err != nil {
			db.Close()
			return nil, fmt.Errorf("add ttl_seconds column: %w", err)
		}
		if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_events_ttl ON events(ttl_seconds, created_at)"); err != nil {
			db.Close()
			return nil, fmt.Errorf("create events ttl index: %w", err)
		}
	}

	var hasDependsOn bool
	db.QueryRow(`SELECT COUNT(*) > 0 FROM pragma_table_info('tasks') WHERE name = 'depends_on'`).Scan(&hasDependsOn)
	if !hasDependsOn {
		if _, err := db.Exec("ALTER TABLE tasks ADD COLUMN depends_on TEXT"); err != nil {
			db.Close()
			return nil, fmt.Errorf("add depends_on column: %w", err)
		}
		if debug {
			slog.Info("db depends_on column migration applied")
		}
	}

	return &DB{db, debug}, nil
}

func (db *DB) Close() error {
	// Prevent unbounded WAL growth.
	if db.debug {
		slog.Info("db checkpointing WAL before close")
	}
	db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return db.DB.Close()
}
