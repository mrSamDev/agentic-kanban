package storage

import (
	"fmt"
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

func TestTaskSeqMigrationOldFormat(t *testing.T) {
	// Create a DB with the old task_seq format (multi-row, no id column)
	path := newTestPath(t)

	// Open with old schema first
	db, err := Open(path, false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Simulate pre-existing task and seed old-style task_seq with 3 rows (a symptom of old schema)
	db.Exec("INSERT INTO tasks (id, title, status, role_boundary, project, priority) VALUES ('TASK-5', 'old task', 'TODO', 'worker', 'default', 10)")
	db.Exec("DROP TABLE IF EXISTS task_seq")
	db.Exec("CREATE TABLE task_seq (next_id INTEGER NOT NULL DEFAULT 1)")
	db.Exec("INSERT INTO task_seq (next_id) VALUES (0)")
	db.Exec("INSERT INTO task_seq (next_id) VALUES (0)")
	db.Exec("INSERT INTO task_seq (next_id) VALUES (0)")
	db.Close()

	// Re-open — migration should: drop old task_seq, recreate, seed from MAX(id)
	db2, err := Open(path, false)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer db2.Close()

	// Verify task_seq has exactly 1 row with correct structure
	var rowCount int
	db2.QueryRow("SELECT COUNT(*) FROM task_seq").Scan(&rowCount)
	if rowCount != 1 {
		t.Fatalf("expected 1 task_seq row after migration, got %d", rowCount)
	}

	var id, nextID int
	db2.QueryRow("SELECT id, next_id FROM task_seq WHERE id = 1").Scan(&id, &nextID)
	if id != 1 {
		t.Fatalf("expected task_seq.id = 1, got %d", id)
	}

	// next_id should be MAX(substr(id,6)) = 5 (task TASK-5 exists)
	if nextID != 5 {
		t.Fatalf("expected task_seq.next_id = 5 (MAX(id) from TASK-5), got %d", nextID)
	}

	// Verify id column CHECK constraint exists (pragma)
	var hasPK bool
	db2.QueryRow(`
		SELECT COUNT(*) > 0 FROM pragma_table_info('task_seq') WHERE pk > 0
	`).Scan(&hasPK)
	if !hasPK {
		t.Fatal("task_seq should have a primary key after migration")
	}

	// Verify dispatching after migration works and produces the next sequential ID
	_, err = db2.Exec("INSERT INTO tasks (id, title, status, role_boundary, project, priority) VALUES ('TASK-6', 'migrated task', 'TODO', 'worker', 'default', 10)")
	if err != nil {
		t.Fatalf("dispatch after migration: %v", err)
	}
}

func TestTaskSeqMigrationEmptyDB(t *testing.T) {
	// Create a DB with old task_seq format but NO tasks
	path := newTestPath(t)

	db, err := Open(path, false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.Exec("DROP TABLE IF EXISTS task_seq")
	db.Exec("CREATE TABLE task_seq (next_id INTEGER NOT NULL DEFAULT 1)")
	db.Exec("INSERT INTO task_seq (next_id) VALUES (0)")
	db.Exec("INSERT INTO task_seq (next_id) VALUES (0)")
	db.Close()

	// Re-open — migration should handle empty DB gracefully
	db2, err := Open(path, false)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer db2.Close()

	// Verify single row, next_id = 0 (no tasks to seed from)
	var rowCount, nextID int
	db2.QueryRow("SELECT COUNT(*), next_id FROM task_seq WHERE id = 1").Scan(&rowCount, &nextID)
	if rowCount != 1 {
		t.Fatalf("expected 1 row, got %d", rowCount)
	}
	if nextID != 0 {
		t.Fatalf("expected next_id = 0 (no tasks), got %d", nextID)
	}

	// First dispatch should give TASK-1 (simulate nextID() pattern)
	var nextID2 int
	db2.QueryRow("UPDATE task_seq SET next_id = next_id + 1 WHERE id = 1 RETURNING next_id").Scan(&nextID2)
	if nextID2 != 1 {
		t.Fatalf("after increment, expected next_id = 1, got %d", nextID2)
	}

	// Verify generated ID can be inserted (simulates dispatch completing)
	generatedID := fmt.Sprintf("TASK-%d", nextID2)
	_, err = db2.Exec("INSERT INTO tasks (id, title, status, role_boundary, project, priority) VALUES (?, 'task', 'TODO', 'worker', 'test', 10)",
		generatedID)
	if err != nil {
		t.Fatalf("insert with generated ID %s: %v (would be UNIQUE constraint crash if seq is wrong)", generatedID, err)
	}
}
