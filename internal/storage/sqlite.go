package storage

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaFS embed.FS

// DB wraps *sql.DB with a single-connection guarantee so per-connection
// pragmas (foreign_keys, busy_timeout) persist for the whole session.
type DB struct {
	*sql.DB
	debug bool
}

// Open opens (or creates) the SQLite database at path, sets required pragmas,
// and runs the embedded schema migration. Parent directories are auto-created.
func Open(path string, debug bool) (*DB, error) {
	// Ensure parent directory exists — SQLite won't create intermediate dirs.
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

	// Single connection guarantees pragmas set below apply to all queries.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if debug {
		log.Println("[db] opened:", path)
	}

	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if debug {
		log.Println("[db] WAL mode enabled")
	}

	if _, err := db.Exec("PRAGMA wal_autocheckpoint = 1000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set wal_autocheckpoint: %w", err)
	}
	if debug {
		log.Println("[db] wal_autocheckpoint = 1000")
	}

	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign_keys: %w", err)
	}

	// Run schema migration (idempotent — all CREATE IF NOT EXISTS).
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
		log.Println("[db] schema applied")
	}

	// Migrate existing databases: add project column if missing.
	// Ignore error if column already exists (expected for new/already-migrated DBs).
	_, _ = db.Exec("ALTER TABLE tasks ADD COLUMN project TEXT NOT NULL DEFAULT 'default'")
	if debug {
		log.Println("[db] project column migration applied")
	}

	return &DB{db, debug}, nil
}

// Close shuts down the underlying database connection.
func (db *DB) Close() error {
	// Checkpoint WAL before closing to prevent unbounded growth
	if db.debug {
		log.Println("[db] checkpointing WAL before close")
	}
	db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return db.DB.Close()
}
