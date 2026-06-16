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
	rdb    *sql.DB
	debug  bool
}

func (db *DB) Reader() *sql.DB { return db.rdb }

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
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if debug {
		slog.Info("db WAL mode enabled")
	}

	if _, err := db.Exec("PRAGMA wal_autocheckpoint = 1000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set wal_autocheckpoint: %w", err)
	}
	if debug {
		slog.Info("db wal_autocheckpoint set", "value", 1000)
	}

	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign_keys: %w", err)
	}

	// --- task_seq migration (must run before schema apply) ---
	// Old task_seq had no id column and no PK, producing duplicate rows.
	// Drop it now so the new CREATE TABLE takes effect.
	// We detect old schema by checking if the id column is missing.
	var hasSeqID bool
	// Table may not exist yet on fresh DB; ignore error.
	_ = db.QueryRow(`SELECT COUNT(*) > 0 FROM pragma_table_info('task_seq') WHERE name = 'id'`).Scan(&hasSeqID)
	if !hasSeqID {
		if _, err := db.Exec(`DROP TABLE IF EXISTS task_seq`); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("drop old task_seq: %w", err)
		}
		if debug {
			slog.Info("old task_seq dropped, recreating with single-row PK")
		}
	}

	// Idempotent — all CREATE IF NOT EXISTS.
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("read embedded schema: %w", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if debug {
		slog.Info("db schema applied")
	}

	// Always seed task_seq from existing tasks on every open.
	if _, err := db.Exec(`
		UPDATE task_seq SET next_id = (
			SELECT COALESCE(MAX(CAST(substr(id,6) AS INTEGER)), 0) FROM tasks
		) WHERE id = 1
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("seed task_seq: %w", err)
	}
	if debug {
		slog.Info("task_seq seeded from existing tasks")
	}

	// Apply numbered migrations (replaces ad-hoc pragma_table_info checks).
	// task_seq PK fix (migration 1) is handled pre-schema above.
	// Migrations 2+ handle column additions, index rebuilds, etc.
	if err := runMigrations(db, debug); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	// Open read-replica connection for non-mutating queries.
	// Uses query_only pragma so writes are rejected at the driver level.
	rdb, err := sql.Open("sqlite", path)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open reader: %w", err)
	}
	rdb.SetMaxOpenConns(3)
	rdb.SetMaxIdleConns(3)

	if _, err := rdb.Exec("PRAGMA query_only = 1"); err != nil {
		_ = rdb.Close()
		_ = db.Close()
		return nil, fmt.Errorf("enable reader query_only: %w", err)
	}
	if _, err := rdb.Exec("PRAGMA busy_timeout = 10000"); err != nil {
		_ = rdb.Close()
		_ = db.Close()
		return nil, fmt.Errorf("set reader busy_timeout: %w", err)
	}
	if debug {
		slog.Info("reader connection opened", "path", path)
	}

	return &DB{db, rdb, debug}, nil
}

func (db *DB) Close() error {
	// Prevent unbounded WAL growth.
	if db.debug {
		slog.Info("db checkpointing WAL before close")
	}
	_, _ = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	if db.rdb != nil {
		_ = db.rdb.Close()
	}
	return db.DB.Close()
}
