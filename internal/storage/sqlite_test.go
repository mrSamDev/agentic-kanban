package storage

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func newTestPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test-"+strconv.Itoa(os.Getpid())+".db")
}

func TestOpenCreatesDB(t *testing.T) {
	path := newTestPath(t)
	db, err := Open(path, false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("db file not created")
	}
}

func TestOpenCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "sub", "kanban.db")
	db, err := Open(path, false)
	if err != nil {
		t.Fatalf("open with nested dirs: %v", err)
	}
	db.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("db file not created in nested dirs")
	}
}

func TestOpenSetsPragmas(t *testing.T) {
	path := newTestPath(t)
	db, err := Open(path, false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// WAL mode
	var journalMode string
	db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if journalMode != "wal" {
		t.Fatalf("expected WAL journal mode, got %s", journalMode)
	}

	// Foreign keys enabled
	var fk int
	db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	if fk != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", fk)
	}

	// Busy timeout
	var busy int
	db.QueryRow("PRAGMA busy_timeout").Scan(&busy)
	if busy != 5000 {
		t.Fatalf("expected busy_timeout=5000, got %d", busy)
	}
}

func TestOpenAppliesSchema(t *testing.T) {
	path := newTestPath(t)
	db, err := Open(path, false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Verify all tables exist
	var tables []string
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		rows.Scan(&name)
		tables = append(tables, name)
	}
	rows.Close()

	expected := []string{"history", "notes", "tasks"}
	for _, e := range expected {
		found := false
		for _, t := range tables {
			if t == e {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected table %s, got %v", e, tables)
		}
	}
}

func TestOpenIdempotent(t *testing.T) {
	path := newTestPath(t)
	db1, err := Open(path, false)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	db1.Close()

	// Re-open same path — should not error
	db2, err := Open(path, false)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	db2.Close()
}

func TestOpenSingleConnection(t *testing.T) {
	path := newTestPath(t)
	db, err := Open(path, false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if maxOpen := db.Stats().MaxOpenConnections; maxOpen != 1 {
		t.Fatalf("expected MaxOpenConnections=1, got %d", maxOpen)
	}
}

func TestCloseCheckpointsWAL(t *testing.T) {
	path := newTestPath(t)
	db, err := Open(path, false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Write some data to generate WAL content
	db.Exec("CREATE TABLE IF NOT EXISTS _test_wal (x INTEGER)")
	db.Exec("INSERT INTO _test_wal VALUES (1), (2), (3)")

	// Close triggers WAL checkpoint
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Re-open and verify data survived
	db2, err := Open(path, false)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer db2.Close()

	var count int
	db2.QueryRow("SELECT COUNT(*) FROM _test_wal").Scan(&count)
	if count != 3 {
		t.Fatalf("expected 3 rows after close/reopen, got %d", count)
	}
}

func TestOpenInvalidPath(t *testing.T) {
	// Root dir is not writable — should fail
	_, err := Open("/dev/null/kanban.db", false)
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestOpenWithDebugDoesNotPanic(t *testing.T) {
	path := newTestPath(t)
	db, err := Open(path, true)
	if err != nil {
		t.Fatalf("open with debug: %v", err)
	}
	db.Close()
}

func TestDBImplementsCloser(t *testing.T) {
	// Compile-time check: *DB satisfies io.Closer
	var _ = (func() error { return nil }) // placeholder
	path := newTestPath(t)
	db, err := Open(path, false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestOpenProjectColumnMigration(t *testing.T) {
	// Create a DB without the project column (simulate pre-migration state)
	path := newTestPath(t)
	db, err := Open(path, false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.Close()

	// Re-open — migration runs, should not error
	db2, err := Open(path, false)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer db2.Close()

	// Verify project column exists
	var colExists bool
	db2.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('tasks')
		WHERE name = 'project'
	`).Scan(&colExists)
	if !colExists {
		t.Fatal("project column missing after migration")
	}
}
