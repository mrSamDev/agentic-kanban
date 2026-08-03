package task

import (
	"strconv"
	"sync"
	"testing"
)

func TestClaimNext(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "urgent", "", "worker", "default", 1, nil)
	s.Dispatch(t.Context(), "normal", "", "worker", "default", 100, nil)
	task, err := s.ClaimNext(t.Context(), "alice", "worker", "")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "TASK-1" {
		t.Fatalf("expected TASK-1 (highest priority), got %s", task.ID)
	}
	if task.Status != StatusInProgress {
		t.Fatalf("expected IN_PROGRESS, got %s", task.Status)
	}
	if *task.AssignedAgent != "alice" {
		t.Fatalf("expected alice, got %v", *task.AssignedAgent)
	}
	if task.LeaseUntil == nil {
		t.Fatal("lease should be set")
	}

	// Claim again — should get second task
	task2, err := s.ClaimNext(t.Context(), "bob", "worker", "")
	if err != nil {
		t.Fatal(err)
	}
	if task2.ID != "TASK-2" {
		t.Fatalf("expected TASK-2, got %s", task2.ID)
	}
}


func TestClaimNextNoWork(t *testing.T) {
	s := newTestService(t)
	// No tasks at all
	task, err := s.ClaimNext(t.Context(), "alice", "worker", "")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "" {
		t.Fatal("expected empty task when no work")
	}
}


func TestClaimNextOnlyForRole(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "worker task", "", "worker", "default", 10, nil)
	s.Dispatch(t.Context(), "reviewer task", "", "reviewer", "default", 5, nil)
	// Reviewer claims — should get the reviewer task despite lower priority
	task, err := s.ClaimNext(t.Context(), "dave", "reviewer", "")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "TASK-2" {
		t.Fatalf("expected TASK-2 (reviewer), got %s", task.ID)
	}

	// Worker claims — should get the worker task
	task2, err := s.ClaimNext(t.Context(), "alice", "worker", "")
	if err != nil {
		t.Fatal(err)
	}
	if task2.ID != "TASK-1" {
		t.Fatalf("expected TASK-1 (worker), got %s", task2.ID)
	}
}


func TestClaimNextAtomic(t *testing.T) {
	s := newTestService(t)
	for i := 0; i < 10; i++ {
		s.Dispatch(t.Context(), "task", "", "worker", "default", 100, nil)
	}

	var mu sync.Mutex
	claimed := make(map[string]string) // taskID -> agent
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(agent string) {
			defer wg.Done()
			task, err := s.ClaimNext(t.Context(), agent, "worker", "")
			if err != nil {
				return // no work left
			}
			mu.Lock()
			claimed[task.ID] = agent
			mu.Unlock()
		}("agent-" + strconv.Itoa(i))
	}
	wg.Wait()

	// Every claimed task should be unique — no two agents got the same ID
	seen := make(map[string]bool)
	for taskID, agent := range claimed {
		if seen[taskID] {
			t.Fatalf("duplicate claim: task %s claimed by %s", taskID, agent)
		}
		seen[taskID] = true
	}
}


func TestClaimByID(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)

	// Claim by ID
	task, err := s.ClaimByID(t.Context(), "TASK-1", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "TASK-1" {
		t.Fatalf("expected TASK-1, got %s", task.ID)
	}
	if task.Status != StatusInProgress {
		t.Fatalf("expected IN_PROGRESS, got %s", task.Status)
	}
	if *task.AssignedAgent != "alice" {
		t.Fatalf("expected alice, got %s", *task.AssignedAgent)
	}
	if task.LeaseUntil == nil {
		t.Fatal("lease should be set")
	}

	// Claiming same task again should fail
	_, err = s.ClaimByID(t.Context(), "TASK-1", "bob")
	if err == nil {
		t.Fatal("expected error claiming already-claimed task")
	}
}


func TestClaimByIDNotFound(t *testing.T) {
	s := newTestService(t)
	_, err := s.ClaimByID(t.Context(), "TASK-999", "alice")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}


func TestClaimByIDWrongState(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)

	// Complete without claiming first — should be DONE
	s.ClaimByID(t.Context(), "TASK-1", "alice")
	s.Complete(t.Context(), "TASK-1", "alice", false)

	// Now claim should fail — status is DONE
	_, err := s.ClaimByID(t.Context(), "TASK-1", "bob")
	if err == nil {
		t.Fatal("expected error claiming DONE task")
	}
}


func TestClaimByIDBlocksOnDeps(t *testing.T) {
	s := newTestService(t)
	dep, _ := s.Dispatch(t.Context(), "dependency", "", "worker", "default", 1, nil)
	blocked := dep.ID
	s.Dispatch(t.Context(), "blocked", "", "worker", "default", 5, &blocked)

	// Try to claim blocked task directly — should fail
	_, err := s.ClaimByID(t.Context(), "TASK-2", "alice")
	if err == nil {
		t.Fatal("expected error claiming task with unmet deps")
	}

	// Claim and complete the dependency
	s.ClaimByID(t.Context(), "TASK-1", "alice")
	s.Complete(t.Context(), "TASK-1", "alice", false)

	// Now blocked task should be claimable
	task, err := s.ClaimByID(t.Context(), "TASK-2", "alice")
	if err != nil {
		t.Fatalf("expected success after dep resolved, got %v", err)
	}
	if task.Status != StatusInProgress {
		t.Fatalf("expected IN_PROGRESS, got %s", task.Status)
	}
}


func TestClaimByIDReclaimsExpiredLease(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)

	// Claim by agent-1
	s.ClaimByID(t.Context(), "TASK-1", "agent-1")

	// Manually expire the lease
	s.db.Exec("UPDATE tasks SET lease_until = datetime('now', '-1 minute') WHERE id = 'TASK-1'")

	// Agent-2 should be able to reclaim
	task, err := s.ClaimByID(t.Context(), "TASK-1", "agent-2")
	if err != nil {
		t.Fatalf("expected reclaim on expired lease, got %v", err)
	}
	if *task.AssignedAgent != "agent-2" {
		t.Fatalf("expected agent-2, got %s", *task.AssignedAgent)
	}
}


func TestDependsOnBlocksClaim(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "independent", "", "worker", "default", 10, nil)
	dep, _ := s.Dispatch(t.Context(), "dependency", "", "worker", "default", 20, nil)
	blocked := dep.ID
	// Blocked task depends on the first task
	s.Dispatch(t.Context(), "blocked", "", "worker", "default", 5, &blocked)

	// Claim should get independent (highest priority with no unmet deps)
	task, err := s.ClaimNext(t.Context(), "alice", "worker", "")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "TASK-1" {
		t.Fatalf("expected TASK-1 (independent), got %s", task.ID)
	}

	// Blocked task (TASK-3) should still be TODO
	blockedTask, _ := s.View(t.Context(), "TASK-3")
	if blockedTask.Status != StatusTODO {
		t.Fatalf("expected blocked task TODO, got %s", blockedTask.Status)
	}
}


func TestDependsOnReleasesAfterComplete(t *testing.T) {
	s := newTestService(t)
	dep, _ := s.Dispatch(t.Context(), "dependency", "", "worker", "default", 1, nil)
	blocked := dep.ID
	s.Dispatch(t.Context(), "blocked", "", "worker", "default", 5, &blocked)

	// Claim and complete the dependency
	task, _ := s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), task.ID, "alice", false)

	// Now the blocked task should be claimable
	next, err := s.ClaimNext(t.Context(), "alice", "worker", "")
	if err != nil {
		t.Fatal(err)
	}
	if next.ID != "TASK-2" {
		t.Fatalf("expected TASK-2 (blocked) now claimable, got %s", next.ID)
	}
	if next.Status != StatusInProgress {
		t.Fatalf("expected IN_PROGRESS, got %s", next.Status)
	}
}


func TestDependsOnConcurrent(t *testing.T) {
	s := newTestService(t)
	dep, _ := s.Dispatch(t.Context(), "dependency", "", "worker", "default", 1, nil)
	blocked := dep.ID
	s.Dispatch(t.Context(), "blocked", "", "worker", "default", 1, &blocked)

	var mu sync.Mutex
	claimed := make(map[string]string)
	var wg sync.WaitGroup

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(agent string) {
			defer wg.Done()
			task, err := s.ClaimNext(t.Context(), agent, "worker", "")
			if err != nil {
				return
			}
			if task.ID == "" {
				return
			}
			mu.Lock()
			claimed[task.ID] = agent
			mu.Unlock()
		}("agent-" + strconv.Itoa(i))
	}
	wg.Wait()

	// Only TASK-1 (the dep) should be claimable — TASK-2 depends on TASK-1, which is not DONE
	if len(claimed) > 1 {
		t.Fatalf("at most 1 task should be claimed (TASK-1), got %d: %v", len(claimed), claimed)
	}
	if len(claimed) == 1 {
		for id := range claimed {
			if id != "TASK-1" {
				t.Fatalf("claimed %s but should only claim TASK-1", id)
			}
		}
	}

	// TASK-2 must remain TODO or IN_PROGRESS-by-TASK-1-agent (heartbeat race)
	blockedTask, _ := s.View(t.Context(), "TASK-2")
	if blockedTask.Status == StatusDone {
		t.Fatalf("TASK-2 should not be done (depends on TASK-1), got %s", blockedTask.Status)
	}
}


func TestExtendLease(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")

	// Record pre-extension lease
	pre, _ := s.View(t.Context(), "TASK-1")
	preLease := *pre.LeaseUntil

	task, err := s.ExtendLease(t.Context(), "TASK-1", "alice", 60)
	if err != nil {
		t.Fatal(err)
	}
	if task.LeaseUntil == nil || !task.LeaseUntil.After(preLease) {
		t.Fatal("lease should have been extended")
	}
	if task.AssignedAgent == nil || *task.AssignedAgent != "alice" {
		t.Fatal("task should still be assigned to alice")
	}
	if task.Status != StatusInProgress {
		t.Fatalf("expected IN_PROGRESS, got %s", task.Status)
	}
}


func TestExtendLeaseWrongAgent(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")

	_, err := s.ExtendLease(t.Context(), "TASK-1", "bob", 15)
	if err != ErrNotAssigned {
		t.Fatalf("expected ErrNotAssigned, got %v", err)
	}
}


func TestExtendLeaseNotFound(t *testing.T) {
	s := newTestService(t)
	_, err := s.ExtendLease(t.Context(), "NONEXIST", "alice", 15)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}


func TestExtendLeaseDefaultsMinutes(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")

	pre, _ := s.View(t.Context(), "TASK-1")
	preLease := *pre.LeaseUntil

	// With minutes=0, should default to 15
	task, err := s.ExtendLease(t.Context(), "TASK-1", "alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	if task.LeaseUntil == nil || task.LeaseUntil.Before(preLease) {
		t.Fatal("lease should have been extended with default minutes")
	}
}


func TestLeaseReclaim(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "stale task", "", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")

	// Manually expire the lease in the DB
	s.db.Exec("UPDATE tasks SET lease_until = datetime('now', '-1 minute') WHERE id = 'TASK-1'")

	// Another agent should be able to reclaim
	task, err := s.ClaimNext(t.Context(), "bob", "worker", "")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "TASK-1" {
		t.Fatalf("expected TASK-1 (reclaimed), got %s", task.ID)
	}
	if *task.AssignedAgent != "bob" {
		t.Fatalf("expected bob, got %s", *task.AssignedAgent)
	}
}

