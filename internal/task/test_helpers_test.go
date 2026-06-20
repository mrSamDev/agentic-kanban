package task

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
	"os"

	"taskbind/internal/storage"
)

func newTestDB(t *testing.T) *storage.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.Open(path, false)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestService(t *testing.T) *Service {
	db := newTestDB(t)
	return NewService(db.DB, db.Reader(), 0, "", nil)
}

func newTestServiceWithHooks(t *testing.T, hooksDir string) *Service {
	t.Helper()
	db := newTestDB(t)
	return NewService(db.DB, db.Reader(), 0, hooksDir, nil)
}

func newBenchDB(b *testing.B) *storage.DB {
	b.Helper()
	path := filepath.Join(b.TempDir(), "test.db")
	db, err := storage.Open(path, false)
	if err != nil {
		b.Fatalf("open bench db: %v", err)
	}
	b.Cleanup(func() { db.Close() })
	return db
}

func newBenchService(b *testing.B) *Service {
	db := newBenchDB(b)
	return NewService(db.DB, db.Reader(), 0, "", nil)
}

func makeHook(t *testing.T, hooksDir, eventType, script string) {
	t.Helper()
	name := strings.ReplaceAll(eventType, ".", "-")
	path := filepath.Join(hooksDir, name)
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
}

// waitForSentinel polls for a file up to 2s (hooks run async now).
func waitForSentinel(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for sentinel: %s", path)
}

func makeHookD(t *testing.T, hooksDir, eventType, name, script string) {
	t.Helper()
	dir := filepath.Join(hooksDir, strings.ReplaceAll(eventType, ".", "-")+".d")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
}