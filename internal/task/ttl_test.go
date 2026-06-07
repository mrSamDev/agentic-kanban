package task

import (
	"testing"
	"time"
)

func TestTTLDefaultOnInsert(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "test", "worker", "default", 1, nil)
	// Event should have default TTL of 259200 (3 days)
	var ttl *int
	err := s.db.QueryRow("SELECT ttl_seconds FROM events WHERE event_type = 'task.created'").Scan(&ttl)
	if err != nil {
		t.Fatal(err)
	}
	if ttl == nil {
		t.Fatal("expected default TTL, got NULL")
	}
	if *ttl != 259200 {
		t.Fatalf("expected TTL 259200, got %d", *ttl)
	}
}

func TestTTLAutoCleanup(t *testing.T) {
	s := newTestService(t)

	// Insert an event with a 1-second TTL
	_, err := s.db.Exec(
		`INSERT INTO events (event_type, payload, ttl_seconds) VALUES (?, ?, 1)`,
		"test.expired", `{"test":true}`,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for it to expire (2s to account for SQLite second-level precision)
	time.Sleep(2 * time.Second)

	// Insert another event — this should trigger cleanup
	s.PruneEvents(t.Context())
	// The expired event should be gone
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM events WHERE event_type = 'test.expired'").Scan(&count)
	if count != 0 {
		t.Fatalf("expected expired event to be cleaned up, got %d", count)
	}
}

func TestTTLNullNeverExpires(t *testing.T) {
	s := newTestService(t)

	// Insert an event with NULL TTL
	_, err := s.db.Exec(
		`INSERT INTO events (event_type, payload, ttl_seconds) VALUES (?, ?, NULL)`,
		"test.permanent", `{"test":true}`,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Insert another event — cleanup should not affect NULL TTL events
	s.PruneEvents(t.Context())
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM events WHERE event_type = 'test.permanent'").Scan(&count)
	if count != 1 {
		t.Fatalf("expected NULL TTL event to survive cleanup, got %d", count)
	}
}

func TestTTLAutoCleanupNonExpired(t *testing.T) {
	s := newTestService(t)

	// Insert an event with a long TTL
	_, err := s.db.Exec(
		`INSERT INTO events (event_type, payload, ttl_seconds) VALUES (?, ?, 86400)`,
		"test.fresh", `{"test":true}`,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Insert another event — cleanup should not affect non-expired events
	s.PruneEvents(t.Context())
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM events WHERE event_type = 'test.fresh'").Scan(&count)
	if count != 1 {
		t.Fatalf("expected non-expired event to survive cleanup, got %d", count)
	}
}

func TestTTLPruneClearTTL(t *testing.T) {
	s := newTestService(t)

	// Insert events with TTL
	_, err := s.db.Exec(
		`INSERT INTO events (event_type, payload, ttl_seconds) VALUES (?, ?, 3600)`,
		"test.clearable", `{"test":true}`,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Clear TTL
	n, err := s.PruneClearTTL(t.Context(), []string{"1"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row affected, got %d", n)
	}

	// Verify TTL is now NULL
	var ttl *int
	s.db.QueryRow("SELECT ttl_seconds FROM events WHERE id = 1").Scan(&ttl)
	if ttl != nil {
		t.Fatal("expected TTL to be NULL after clear")
	}
}
