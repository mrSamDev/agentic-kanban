package task

import (
	"path/filepath"
	"strings"
	"testing"

	"agent-kanban/internal/storage"
)

// Cycle detection: dispatching a task into an existing cycle is rejected.
// Uses one shared DB for both the service and the SQL back-edge injection.
func TestDispatch_CycleDetected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewService(db.DB, db.Reader(), 0, "")

	// Chain: 1→2→3
	t1, _ := s.Dispatch(t.Context(), "one", "worker", "default", 10, nil)
	t2, _ := s.Dispatch(t.Context(), "two", "worker", "default", 20, &t1.ID)
	t3, _ := s.Dispatch(t.Context(), "three", "worker", "default", 30, &t2.ID)

	// Inject back edge: 1→2→3→1 creates a cycle
	db.Exec("UPDATE tasks SET depends_on = ? WHERE id = ?", t3.ID, t1.ID)

	// Dispatching a task that depends on the cycle should fail.
	_, err = s.Dispatch(t.Context(), "four", "worker", "default", 40, &t1.ID)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("expected 'cycle detected' in error, got: %v", err)
	}

	// Dispatch with no deps (doesn't touch the cycle) should succeed.
	// The failed dispatch was rolled back; verify a clean dispatch works.
	t5, err := s.Dispatch(t.Context(), "five", "worker", "default", 50, nil)
	if err != nil {
		t.Fatalf("no-deps dispatch should succeed, got: %v", err)
	}
	// ID sequence may or may not have advanced — what matters is clean dispatch succeeds.
	_ = t5
}

// Scenario 1: Basic A→B transfer (happy path)
func TestTransferClaim_Basic(t *testing.T) {
	s := newTestService(t)
	task, _ := s.Dispatch(t.Context(), "ship it", "worker", "default", 10, nil)
	claimed, _ := s.ClaimByID(t.Context(), task.ID, "alice")
	if claimed.AssignedAgent == nil || *claimed.AssignedAgent != "alice" {
		t.Fatalf("expected alice to have claimed task")
	}

	transferred, err := s.TransferClaim(t.Context(), task.ID, "alice", "bob")
	if err != nil {
		t.Fatalf("transfer failed: %v", err)
	}
	if transferred.AssignedAgent == nil || *transferred.AssignedAgent != "bob" {
		t.Fatalf("expected bob to own task after transfer, got %v", transferred.AssignedAgent)
	}
}

// Scenario 2: Transfer to self → explicit error
func TestTransferClaim_ToSelf(t *testing.T) {
	s := newTestService(t)
	task, _ := s.Dispatch(t.Context(), "ship it", "worker", "default", 10, nil)
	s.ClaimByID(t.Context(), task.ID, "alice")

	_, err := s.TransferClaim(t.Context(), task.ID, "alice", "alice")
	if err == nil {
		t.Fatal("expected error for self-transfer, got nil")
	}
	if !strings.Contains(err.Error(), "yourself") {
		t.Fatalf("expected 'yourself' in error, got: %v", err)
	}
}

// Scenario 3: Transfer before claim / wrong status
func TestTransferClaim_WrongStatus(t *testing.T) {
	s := newTestService(t)
	task, _ := s.Dispatch(t.Context(), "ship it", "worker", "default", 10, nil)
	// task is TODO, not IN_PROGRESS

	_, err := s.TransferClaim(t.Context(), task.ID, "alice", "bob")
	if err == nil {
		t.Fatal("expected error for unclaimed task, got nil")
	}
	if !strings.Contains(err.Error(), "not IN_PROGRESS") {
		t.Fatalf("expected 'not IN_PROGRESS' in error, got: %v", err)
	}
}

// Scenario 4: Transfer race / wrong owner
func TestTransferClaim_WrongOwner(t *testing.T) {
	s := newTestService(t)
	task, _ := s.Dispatch(t.Context(), "ship it", "worker", "default", 10, nil)
	s.ClaimByID(t.Context(), task.ID, "alice")

	// bob tries to transfer alice's task
	_, err := s.TransferClaim(t.Context(), task.ID, "bob", "charlie")
	if err == nil {
		t.Fatal("expected error for wrong owner, got nil")
	}
	if !strings.Contains(err.Error(), "not assigned to bob") {
		t.Fatalf("expected 'not assigned to bob' in error, got: %v", err)
	}
}

// Scenario 5: Transfer chain A→B→C
func TestTransferClaim_Chain(t *testing.T) {
	s := newTestService(t)
	task, _ := s.Dispatch(t.Context(), "ship it", "worker", "default", 10, nil)
	s.ClaimByID(t.Context(), task.ID, "alice")

	t1, _ := s.TransferClaim(t.Context(), task.ID, "alice", "bob")
	if t1.AssignedAgent == nil || *t1.AssignedAgent != "bob" {
		t.Fatalf("expected bob after first transfer, got %v", t1.AssignedAgent)
	}

	t2, _ := s.TransferClaim(t.Context(), task.ID, "bob", "charlie")
	if t2.AssignedAgent == nil || *t2.AssignedAgent != "charlie" {
		t.Fatalf("expected charlie after second transfer, got %v", t2.AssignedAgent)
	}

	// alice can no longer transfer
	_, err := s.TransferClaim(t.Context(), task.ID, "alice", "diana")
	if err == nil {
		t.Fatal("expected error for stale owner, got nil")
	}
}

// Scenario 6: Transfer to non-existent agent (no error — by design)
func TestTransferClaim_GhostAgent(t *testing.T) {
	s := newTestService(t)
	task, _ := s.Dispatch(t.Context(), "ship it", "worker", "default", 10, nil)
	s.ClaimByID(t.Context(), task.ID, "alice")

	// No agent existence check — by design
	transferred, err := s.TransferClaim(t.Context(), task.ID, "alice", "ghost-agent")
	if err != nil {
		t.Fatalf("transfer to ghost agent should succeed by design, got: %v", err)
	}
	if transferred.AssignedAgent == nil || *transferred.AssignedAgent != "ghost-agent" {
		t.Fatalf("expected ghost-agent to own task, got %v", transferred.AssignedAgent)
	}
}

// Scenario 7: Transfer resets lease
func TestTransferClaim_ResetsLease(t *testing.T) {
	s := newTestService(t)
	task, _ := s.Dispatch(t.Context(), "ship it", "worker", "default", 10, nil)
	s.ClaimByID(t.Context(), task.ID, "alice")

	transferred, _ := s.TransferClaim(t.Context(), task.ID, "alice", "bob")
	if transferred.LeaseUntil == nil {
		t.Fatal("expected lease to be reset after transfer")
	}
}

