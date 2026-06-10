package task

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-kanban/internal/storage"
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
	return NewService(db.DB, 0)
}

func TestDispatch(t *testing.T) {
	s := newTestService(t)
	task, err := s.Dispatch(t.Context(), "test task", "worker", "default", 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "TASK-1" {
		t.Fatalf("expected TASK-1, got %s", task.ID)
	}
	if task.Title != "test task" {
		t.Fatalf("expected 'test task', got %s", task.Title)
	}
	if task.Status != StatusTODO {
		t.Fatalf("expected TODO, got %s", task.Status)
	}
	if task.Priority != 50 {
		t.Fatalf("expected priority 50, got %d", task.Priority)
	}
}

func TestDispatchSequentialIDs(t *testing.T) {
	s := newTestService(t)
	t1, _ := s.Dispatch(t.Context(), "a", "worker", "default", 100, nil)
	t2, _ := s.Dispatch(t.Context(), "b", "worker", "default", 100, nil)
	t3, _ := s.Dispatch(t.Context(), "c", "worker", "default", 100, nil)
	if t1.ID != "TASK-1" || t2.ID != "TASK-2" || t3.ID != "TASK-3" {
		t.Fatalf("expected sequential IDs, got %s, %s, %s", t1.ID, t2.ID, t3.ID)
	}
}

func TestClaimNext(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "urgent", "worker", "default", 1, nil)
	s.Dispatch(t.Context(), "normal", "worker", "default", 100, nil)
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
	s.Dispatch(t.Context(), "worker task", "worker", "default", 10, nil)
	s.Dispatch(t.Context(), "reviewer task", "reviewer", "default", 5, nil)
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
		s.Dispatch(t.Context(), "task", "worker", "default", 100, nil)
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
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)

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
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)

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
	dep, _ := s.Dispatch(t.Context(), "dependency", "worker", "default", 1, nil)
	blocked := dep.ID
	s.Dispatch(t.Context(), "blocked", "worker", "default", 5, &blocked)

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
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)

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

func TestComplete(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")

	task, err := s.Complete(t.Context(), "TASK-1", "alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusDone {
		t.Fatalf("expected DONE, got %s", task.Status)
	}
	if task.AssignedAgent != nil {
		t.Fatal("assigned_agent should be nil after complete")
	}
}

func TestCompleteUnassigned(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
	_, err := s.Complete(t.Context(), "TASK-1", "alice", false)
	if err != ErrNotAssigned {
		t.Fatalf("expected ErrNotAssigned, got %v", err)
	}
}

func TestCompleteWrongAgent(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")

	_, err := s.Complete(t.Context(), "TASK-1", "bob", false)
	if err == nil {
		t.Fatal("expected error when wrong agent")
	}
	// Error should include the actual assigned agent
	if !strings.Contains(err.Error(), "assigned to: alice") {
		t.Fatalf("expected error to include actual agent, got %v", err)
	}
}

func TestCompleteToReview(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")

	task, err := s.Complete(t.Context(), "TASK-1", "alice", true)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusInReview {
		t.Fatalf("expected IN_REVIEW, got %s", task.Status)
	}
}

func TestLogProgress(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")

	// Check lease time before
	pre, _ := s.View(t.Context(), "TASK-1")
	preLease := *pre.LeaseUntil

	task, err := s.LogProgress(t.Context(), "TASK-1", "alice", "Making progress", "PROGRESS")
	if err != nil {
		t.Fatal(err)
	}

	// Lease should be renewed (or at least not stale)
	if task.LeaseUntil == nil || task.LeaseUntil.Before(preLease) {
		t.Fatal("lease should have been renewed")
	}
}

func TestBlock(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")

	task, err := s.Block(t.Context(), "TASK-1", "alice", "Blocked on upstream")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusBlocked {
		t.Fatalf("expected BLOCKED, got %s", task.Status)
	}
	if task.AssignedAgent != nil {
		t.Fatal("assigned_agent should be nil after block")
	}
}

func TestViewDetail(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.LogProgress(t.Context(), "TASK-1", "alice", "working", "PROGRESS")

	detail, err := s.ViewDetail(t.Context(), "TASK-1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Task.ID != "TASK-1" {
		t.Fatalf("expected TASK-1, got %s", detail.Task.ID)
	}
	if len(detail.Notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(detail.Notes))
	}
	if detail.Notes[0].Content != "working" {
		t.Fatalf("expected 'working', got %s", detail.Notes[0].Content)
	}
	if len(detail.History) < 3 {
		t.Fatalf("expected at least 3 history entries, got %d", len(detail.History))
	}
}

func TestSearch(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "hotfix", "worker", "default", 1, nil)
	s.Dispatch(t.Context(), "feature", "worker", "default", 100, nil)
	s.Dispatch(t.Context(), "review", "reviewer", "default", 50, nil)
	// Search all
	all, _ := s.Search(t.Context(), SearchParams{})
	if len(all) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(all))
	}

	// Search by role
	workers, _ := s.Search(t.Context(), SearchParams{Role: "worker"})
	if len(workers) != 2 {
		t.Fatalf("expected 2 worker tasks, got %d", len(workers))
	}

	// Search by status
	s.ClaimNext(t.Context(), "alice", "worker", "")
	todos, _ := s.Search(t.Context(), SearchParams{Status: StatusTODO})
	if len(todos) != 2 {
		t.Fatalf("expected 2 TODO tasks, got %d", len(todos))
	}

	// Order: lowest priority first
	if workers[0].ID != "TASK-1" {
		t.Fatalf("expected TASK-1 first (priority 1), got %s", workers[0].ID)
	}
}

func TestReviewApprove(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true)

	// Approve directly on IN_REVIEW — no claim needed.
	task, err := s.ReviewApprove(t.Context(), "TASK-1", "dave")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusDone {
		t.Fatalf("expected DONE, got %s", task.Status)
	}
}

func TestReviewReject(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true)

	// Reject directly on IN_REVIEW — no claim needed.
	task, err := s.ReviewReject(t.Context(), "TASK-1", "dave", "Needs more tests")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusTODO {
		t.Fatalf("expected TODO (back to queue), got %s", task.Status)
	}
}

func TestSelfReviewRejected(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true)

	// Same agent tries to approve — should fail
	_, err := s.ReviewApprove(t.Context(), "TASK-1", "alice")
	if err != ErrSelfReview {
		t.Fatalf("expected ErrSelfReview, got %v", err)
	}

	// Same agent tries to reject — should fail
	_, err = s.ReviewReject(t.Context(), "TASK-1", "alice", "nope")
	if err != ErrSelfReview {
		t.Fatalf("expected ErrSelfReview, got %v", err)
	}
}

func TestSelfReviewAllowedWithEnv(t *testing.T) {
	t.Setenv("KANBAN_ALLOW_SELF_REVIEW", "true")
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true)

	// Same agent can approve when env var is set
	task, err := s.ReviewApprove(t.Context(), "TASK-1", "alice")
	if err != nil {
		t.Fatalf("expected no error with env override, got %v", err)
	}
	if task.Status != StatusDone {
		t.Fatalf("expected DONE after self-approval, got %s", task.Status)
	}
}

func TestSelfReviewDirectInReview(t *testing.T) {
	// Task created directly in IN_REVIEW (no CLAIM history) — approve should work
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
	s.db.Exec("UPDATE tasks SET status = 'IN_REVIEW' WHERE id = 'TASK-1'")

	task, err := s.ReviewApprove(t.Context(), "TASK-1", "alice")
	if err != nil {
		t.Fatalf("expected no error for direct IN_REVIEW, got %v", err)
	}
	if task.Status != StatusDone {
		t.Fatalf("expected DONE, got %s", task.Status)
	}
}

func TestDependsOnBlocksClaim(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "independent", "worker", "default", 10, nil)
	dep, _ := s.Dispatch(t.Context(), "dependency", "worker", "default", 20, nil)
	blocked := dep.ID
	// Blocked task depends on the first task
	s.Dispatch(t.Context(), "blocked", "worker", "default", 5, &blocked)

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
	dep, _ := s.Dispatch(t.Context(), "dependency", "worker", "default", 1, nil)
	blocked := dep.ID
	s.Dispatch(t.Context(), "blocked", "worker", "default", 5, &blocked)

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
	dep, _ := s.Dispatch(t.Context(), "dependency", "worker", "default", 1, nil)
	blocked := dep.ID
	s.Dispatch(t.Context(), "blocked", "worker", "default", 1, &blocked)

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
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
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
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
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
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
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
	s.Dispatch(t.Context(), "stale task", "worker", "default", 1, nil)
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

func TestErrNotFound(t *testing.T) {
	s := newTestService(t)
	_, err := s.View(t.Context(), "NONEXIST")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestErrInvalidState(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
	// Can't complete a TODO task without claiming
	_, err := s.Complete(t.Context(), "TASK-1", "alice", false)
	if err == nil {
		t.Fatal("expected error completing unassigned task")
	}
}

func TestListEvents(t *testing.T) {
	s := newTestService(t)

	// No events yet
	events, err := s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}

	// Dispatch → task.created
	s.Dispatch(t.Context(), "test", "worker", "default", 1, nil)
	events, err = s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event after dispatch, got %d", len(events))
	}
	if events[0].EventType != "task.created" {
		t.Fatalf("expected task.created, got %s", events[0].EventType)
	}

	// Claim → task.claimed
	s.ClaimNext(t.Context(), "alice", "worker", "")
	events, err = s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events after claim, got %d", len(events))
	}
	if events[1].EventType != "task.claimed" {
		t.Fatalf("expected task.claimed, got %s", events[1].EventType)
	}

	// Complete → task.completed
	s.Complete(t.Context(), "TASK-1", "alice", false)
	events, err = s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events after complete, got %d", len(events))
	}
	if events[2].EventType != "task.completed" {
		t.Fatalf("expected task.completed, got %s", events[2].EventType)
	}

	// Verify payloads are inline JSON, not double-encoded strings
	var payload map[string]string
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal task.created payload: %v", err)
	}
	if payload["task_id"] != "TASK-1" {
		t.Fatalf("expected task_id TASK-1, got %s", payload["task_id"])
	}

	// Limit test
	events, err = s.ListEvents(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events with limit=2, got %d", len(events))
	}
}

func TestListEventsReviewPath(t *testing.T) {
	s := newTestService(t)

	// Dispatch + claim + complete with --review → task.submitted_for_review, not task.completed
	s.Dispatch(t.Context(), "needs review", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true)

	events, err := s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}

	// Should have task.created + task.claimed + task.submitted_for_review
	if len(events) != 3 {
		t.Fatalf("expected 3 events (task.created, task.claimed, task.submitted_for_review), got %d", len(events))
	}
	if events[2].EventType != "task.submitted_for_review" {
		t.Fatalf("expected task.submitted_for_review, got %s", events[2].EventType)
	}

	// Approve → review.approved
	s.ReviewApprove(t.Context(), "TASK-1", "dave")
	events, err = s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 events after approve, got %d", len(events))
	}
	if events[3].EventType != "review.approved" {
		t.Fatalf("expected review.approved, got %s", events[3].EventType)
	}
}

func TestListEventsBlockAndReject(t *testing.T) {
	s := newTestService(t)

	s.Dispatch(t.Context(), "task", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")

	// Block → task.blocked
	s.Block(t.Context(), "TASK-1", "alice", "blocked")
	events, err := s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events after block, got %d", len(events))
	}
	if events[2].EventType != "task.blocked" {
		t.Fatalf("expected task.blocked, got %s", events[2].EventType)
	}

	// Dispatch a separate task for the review+reject path
	s.Dispatch(t.Context(), "needs review", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "bob", "worker", "")
	s.Complete(t.Context(), "TASK-2", "bob", true)
	s.ReviewReject(t.Context(), "TASK-2", "dave", "needs work")
	events, err = s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	// Events: task.created(TASK-1), task.claimed(TASK-1), task.blocked(TASK-1),
	//         task.created(TASK-2), task.claimed(TASK-2),
	//         task.submitted_for_review(TASK-2), review.rejected(TASK-2)
	if len(events) != 7 {
		t.Fatalf("expected 7 events total, got %d", len(events))
	}
	if events[6].EventType != "review.rejected" {
		t.Fatalf("expected review.rejected, got %s", events[6].EventType)
	}
}

func TestReviewerCannotClaimInReview(t *testing.T) {
	s := newTestService(t)

	// Worker dispatches + claims + completes with review.
	s.Dispatch(t.Context(), "worker task needing review", "worker", "default", 50, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true)

	// Reviewer should NOT get the IN_REVIEW task via claim-next.
	empty, err := s.ClaimNext(t.Context(), "dave", "reviewer", "")
	if err != nil {
		t.Fatal(err)
	}
	if empty.ID != "" {
		t.Fatalf("reviewer should not claim IN_REVIEW tasks, got %s", empty.ID)
	}

	// But reviewer CAN approve directly.
	task, err := s.ReviewApprove(t.Context(), "TASK-1", "dave")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusDone {
		t.Fatalf("expected DONE after approval, got %s", task.Status)
	}

	// Worker should NOT be able to claim the done task.
	empty2, err := s.ClaimNext(t.Context(), "bob", "worker", "")
	if err != nil {
		t.Fatal(err)
	}
	if empty2.ID != "" {
		t.Fatalf("worker should not claim already-done task, got %s", empty2.ID)
	}
}

func TestMaxEventID(t *testing.T) {
	s := newTestService(t)

	// Empty table → 0
	id, err := s.MaxEventID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if id != 0 {
		t.Fatalf("expected 0 for empty events, got %d", id)
	}

	// After dispatch → max ID should be 1
	s.Dispatch(t.Context(), "test", "worker", "default", 1, nil)
	id, err = s.MaxEventID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Fatalf("expected 1 after dispatch, got %d", id)
	}

	// After claim → max ID should be 2
	s.ClaimNext(t.Context(), "alice", "worker", "")
	id, err = s.MaxEventID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if id != 2 {
		t.Fatalf("expected 2 after claim, got %d", id)
	}
}

func TestPollEvents(t *testing.T) {
	s := newTestService(t)

	// No events → empty result
	events, err := s.PollEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}

	// Dispatch + claim
	s.Dispatch(t.Context(), "test", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")

	// Poll from 0 → both events
	events, err = s.PollEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].EventType != "task.created" {
		t.Fatalf("expected task.created first, got %s", events[0].EventType)
	}
	if events[1].EventType != "task.claimed" {
		t.Fatalf("expected task.claimed second, got %s", events[1].EventType)
	}

	// Poll from 1 → only the second event
	events, err = s.PollEvents(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event after ID 1, got %d", len(events))
	}
	if events[0].EventType != "task.claimed" {
		t.Fatalf("expected task.claimed, got %s", events[0].EventType)
	}

	// Poll from 2 → no new events
	events, err = s.PollEvents(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events after ID 2, got %d", len(events))
	}
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

func TestHookFiresAfterDispatch(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "fired")
	makeHook(t, filepath.Join(dir, "hooks"), "task.created", "touch "+sentinel)
	s.SetHooksDir(filepath.Join(dir, "hooks"))

	if _, err := s.Dispatch(t.Context(), "hook test", "worker", "default", 50, nil); err != nil {
		t.Fatal(err)
	}
	waitForSentinel(t, sentinel)
}

func TestHookDoesNotFireOnRollback(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "fired")
	makeHook(t, filepath.Join(dir, "hooks"), "task.completed", "touch "+sentinel)
	s.SetHooksDir(filepath.Join(dir, "hooks"))

	// Complete with wrong agent — guaranteed rollback, no hook
	s.Dispatch(t.Context(), "task", "worker", "default", 50, nil)
	s.ClaimNext(t.Context(), "agent-a", "worker", "")
	s.Complete(t.Context(), "TASK-1", "wrong-agent", false)

	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("hook fired on rollback")
	}
}

func TestHookClaimNextEmptyResultNoFire(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "fired")
	makeHook(t, filepath.Join(dir, "hooks"), "task.claimed", "touch "+sentinel)
	s.SetHooksDir(filepath.Join(dir, "hooks"))

	// No tasks in DB — ClaimNext returns zero-value task, hook must not fire
	s.ClaimNext(t.Context(), "agent", "worker", "")

	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("hook fired on empty ClaimNext result")
	}
}

func TestHookMissingIsSilent(t *testing.T) {
	s := newTestService(t)
	s.SetHooksDir(t.TempDir()) // empty dir, no hook scripts

	if _, err := s.Dispatch(t.Context(), "task", "worker", "default", 50, nil); err != nil {
		t.Fatalf("Dispatch failed with missing hook: %v", err)
	}
}

func TestHookNonZeroExitIsSilent(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	makeHook(t, filepath.Join(dir, "hooks"), "task.created", "exit 1")
	s.SetHooksDir(filepath.Join(dir, "hooks"))

	if _, err := s.Dispatch(t.Context(), "task", "worker", "default", 50, nil); err != nil {
		t.Fatalf("Dispatch failed with non-zero hook exit: %v", err)
	}
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

func TestHookDAllFire(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	s1 := filepath.Join(dir, "s1")
	s2 := filepath.Join(dir, "s2")
	makeHookD(t, hooksDir, "task.created", "slack", "touch "+s1)
	makeHookD(t, hooksDir, "task.created", "metrics", "touch "+s2)
	s.SetHooksDir(hooksDir)

	if _, err := s.Dispatch(t.Context(), "task", "worker", "default", 50, nil); err != nil {
		t.Fatal(err)
	}
	waitForSentinel(t, s1)
	waitForSentinel(t, s2)
}

func TestHookDLexOrder(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	order := filepath.Join(dir, "order")
	makeHookD(t, hooksDir, "task.created", "a", "echo a >>"+order)
	makeHookD(t, hooksDir, "task.created", "b", "echo b >>"+order)
	makeHookD(t, hooksDir, "task.created", "c", "echo c >>"+order)
	s.SetHooksDir(hooksDir)

	if _, err := s.Dispatch(t.Context(), "task", "worker", "default", 50, nil); err != nil {
		t.Fatal(err)
	}
	// Poll until all three hooks have written
	var lines []string
	for range 50 {
		data, _ := os.ReadFile(order)
		lines = strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(lines) < 3 {
		t.Fatalf("expected 3 hook executions, got %d", len(lines))
	}
	expected := []string{"a", "b", "c"}
	for i, line := range lines[:3] {
		if line != expected[i] {
			t.Fatalf("hook order: expected %s, got %s at position %d", expected[i], line, i)
		}
	}
}

func TestHookDNonExecutableSkipped(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	dDir := filepath.Join(hooksDir, "task-created.d")
	sentinel := filepath.Join(dir, "fired")
	os.MkdirAll(dDir, 0755)
	// write without execute bit
	os.WriteFile(filepath.Join(dDir, "nope"), []byte("#!/bin/sh\ntouch "+sentinel+"\n"), 0644)
	s.SetHooksDir(hooksDir)

	if _, err := s.Dispatch(t.Context(), "task", "worker", "default", 50, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Error("non-executable hook should not have fired")
	}
}

func TestHookDAndSingleFileBothFire(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	single := filepath.Join(dir, "single")
	multi := filepath.Join(dir, "multi")
	makeHook(t, hooksDir, "task.created", "touch "+single)
	makeHookD(t, hooksDir, "task.created", "extra", "touch "+multi)
	s.SetHooksDir(hooksDir)

	if _, err := s.Dispatch(t.Context(), "task", "worker", "default", 50, nil); err != nil {
		t.Fatal(err)
	}
	waitForSentinel(t, single)
	waitForSentinel(t, multi)
}

func TestHookDFailingEntryDoesNotBlockSiblings(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	sentinel := filepath.Join(dir, "fired")
	makeHookD(t, hooksDir, "task.created", "a", "exit 1")
	makeHookD(t, hooksDir, "task.created", "b", "touch "+sentinel)
	s.SetHooksDir(hooksDir)

	if _, err := s.Dispatch(t.Context(), "task", "worker", "default", 50, nil); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	waitForSentinel(t, sentinel)
}

func TestBatchPriorityHookFiresPerTask(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task-a", "worker", "default", 10, nil)
	s.Dispatch(t.Context(), "task-b", "worker", "default", 20, nil)
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	s.SetHooksDir(hooksDir)
	// Each hook invocation touches a unique file named after the task_id from stdin
	makeHook(t, hooksDir, "task.priority_updated", `cat > /dev/null; echo $(( $(cat `+filepath.Join(dir, `count`)+` 2>/dev/null || echo 0) + 1 )) > `+filepath.Join(dir, `count`))

	n, err := s.BatchUpdatePriority(t.Context(), []string{"TASK-1", "TASK-2"}, 99)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 updated, got %d", n)
	}

	var count int
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		data, _ := os.ReadFile(filepath.Join(dir, "count"))
		if len(data) > 0 {
			fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &count)
			if count == 2 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if count != 2 {
		t.Fatalf("expected hook to fire 2 times, got %d", count)
	}
}

func TestBatchProjectHookFiresPerTask(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task-a", "worker", "default", 10, nil)
	s.Dispatch(t.Context(), "task-b", "worker", "default", 20, nil)
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	s.SetHooksDir(hooksDir)
	makeHook(t, hooksDir, "task.project_updated", `cat > /dev/null; echo $(( $(cat `+filepath.Join(dir, `count`)+` 2>/dev/null || echo 0) + 1 )) > `+filepath.Join(dir, `count`))

	n, err := s.BatchUpdateProject(t.Context(), []string{"TASK-1", "TASK-2"}, "new-project")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 updated, got %d", n)
	}

	var count int
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		data, _ := os.ReadFile(filepath.Join(dir, "count"))
		if len(data) > 0 {
			fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &count)
			if count == 2 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if count != 2 {
		t.Fatalf("expected hook to fire 2 times, got %d", count)
	}
}

func TestBatchHookPayloadEnriched(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "my task", "worker", "default", 10, nil)
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	payloadFile := filepath.Join(dir, "payload")
	// Write the full stdin (event+payload wrapper) to file
	makeHook(t, hooksDir, "task.priority_updated", `cat > `+payloadFile)
	s.SetHooksDir(hooksDir)

	_, err := s.BatchUpdatePriority(t.Context(), []string{"TASK-1"}, 99)
	if err != nil {
		t.Fatal(err)
	}
	waitForSentinel(t, payloadFile)

	data, _ := os.ReadFile(payloadFile)
	var envelope struct {
		Event   string            `json:"event"`
		Payload map[string]string `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	p := envelope.Payload
	if envelope.Event != "task.priority_updated" {
		t.Fatalf("expected event task.priority_updated, got %s", envelope.Event)
	}
	if p["task_id"] != "TASK-1" {
		t.Fatalf("expected task_id TASK-1, got %s", p["task_id"])
	}
	if p["title"] != "my task" {
		t.Fatalf("expected title 'my task', got %s", p["title"])
	}
	if p["project"] != "default" {
		t.Fatalf("expected project 'default', got %s", p["project"])
	}
	if p["priority"] != "99" {
		t.Fatalf("expected priority 99, got %s", p["priority"])
	}
}

func TestBatchHookDoesNotFireOnEmptyUpdate(t *testing.T) {
	s := newTestService(t)

	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	fired := filepath.Join(dir, "fired")
	makeHook(t, hooksDir, "task.priority_updated", "touch "+fired)
	s.SetHooksDir(hooksDir)

	n, err := s.BatchUpdatePriority(t.Context(), []string{"NONEXIST"}, 99)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 updated, got %d", n)
	}

	if _, err := os.Stat(fired); err == nil {
		t.Fatal("hook fired when no tasks were updated")
	}
}

// --- Stability tests ---

func TestSequenceStability(t *testing.T) {
	s := newTestService(t)
	n := 50
	ids := make(chan string, n)

	// Dispatch n tasks concurrently to stress-test nextID()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := s.Dispatch(t.Context(), "stability task", "worker", "default", 100, nil)
			if err != nil {
				t.Errorf("dispatch: %v", err)
				return
			}
			ids <- task.ID
		}()
	}
	wg.Wait()
	close(ids)

	// Collect all IDs
	var collected []string
	for id := range ids {
		collected = append(collected, id)
	}

	if len(collected) < n {
		t.Fatalf("expected %d tasks, got %d — some dispatches failed", n, len(collected))
	}

	// Verify all IDs are unique
	seen := make(map[string]bool)
	for _, id := range collected {
		if seen[id] {
			t.Fatalf("duplicate ID: %s", id)
		}
		seen[id] = true
	}

	// Verify IDs are sequential (no gaps after sorting numeric part)
	type idNum struct {
		raw string
		num int
	}
	var parsed []idNum
	for _, id := range collected {
		var n int
		if _, err := fmt.Sscanf(id, "TASK-%d", &n); err != nil {
			t.Fatalf("unexpected ID format: %s", id)
		}
		parsed = append(parsed, idNum{id, n})
	}

	// Find min and max to verify range is fully populated
	min, max := parsed[0].num, parsed[0].num
	for _, p := range parsed {
		if p.num < min {
			min = p.num
		}
		if p.num > max {
			max = p.num
		}
	}

	expectedCount := max - min + 1
	if len(parsed) != expectedCount {
		t.Fatalf("ID range %d-%d has %d values, expected %d (gaps detected)",
			min, max, len(parsed), expectedCount)
	}

	// Verify task_seq is single-row (regression: old bug produced 39 rows of 0)
	var seqRows int
	s.db.QueryRow("SELECT COUNT(*) FROM task_seq").Scan(&seqRows)
	if seqRows != 1 {
		t.Fatalf("task_seq should have 1 row after 50 dispatches, got %d", seqRows)
	}
}

func TestBatchCompletePartialFailure(t *testing.T) {
	s := newTestService(t)

	// Dispatch 3 tasks
	for i := 0; i < 3; i++ {
		s.Dispatch(t.Context(), fmt.Sprintf("task-%d", i+1), "worker", "default", 10, nil)
	}

	// Claim 2 of them with agent-a
	s.ClaimNext(t.Context(), "agent-a", "worker", "", true)
	s.ClaimNext(t.Context(), "agent-a", "worker", "", true)

	// Batch complete 3 tasks: 2 claimed by agent-a, 1 unclaimed
	completed, errs := s.BatchComplete(t.Context(), []string{"TASK-1", "TASK-2", "TASK-3"}, "agent-a", false)

	// Verify: 2 completed, 1 error
	if len(completed) != 2 {
		t.Fatalf("expected 2 completed, got %d", len(completed))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error (unclaimed task), got %d", len(errs))
	}

	// Verify completed tasks are DONE
	for _, tsk := range completed {
		if tsk.Status != StatusDone {
			t.Fatalf("task %s expected DONE, got %s", tsk.ID, tsk.Status)
		}
	}

	// Verify error mentions the unclaimed task
	if !strings.Contains(errs[0].Error(), "TASK-3") {
		t.Fatalf("expected error about TASK-3, got: %v", errs[0])
	}
}

func TestBatchClaimPriorityOrder(t *testing.T) {
	s := newTestService(t)

	// Dispatch tasks with mixed priority
	s.Dispatch(t.Context(), "low", "worker", "default", 100, nil)
	s.Dispatch(t.Context(), "high", "worker", "default", 1, nil)
	s.Dispatch(t.Context(), "medium", "worker", "default", 50, nil)

	// Batch claim 3 tasks
	tasks, err := s.ClaimBatch(t.Context(), "agent-a", "worker", "", 3, true)
	if err != nil {
		t.Fatal(err)
	}

	if len(tasks) != 3 {
		t.Fatalf("expected 3 claimed tasks, got %d", len(tasks))
	}

	// Order should be by priority: high(1), medium(50), low(100)
	expected := []string{"TASK-2", "TASK-3", "TASK-1"}
	for i, tsk := range tasks {
		if tsk.ID != expected[i] {
			t.Fatalf("position %d: expected %s, got %s", i, expected[i], tsk.ID)
		}
	}
}

func TestConcurrentClaimBatchNoDoubleClaim(t *testing.T) {
	s := newTestService(t)

	// Dispatch 20 tasks
	for i := 0; i < 20; i++ {
		s.Dispatch(t.Context(), fmt.Sprintf("task-%d", i+1), "worker", "default", 100, nil)
	}

	var mu sync.Mutex
	claimed := make(map[string]string) // taskID -> agent
	var wg sync.WaitGroup

	// 5 agents each batch-claim 3 tasks (total 15 max)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(agent string) {
			defer wg.Done()
			tasks, err := s.ClaimBatch(t.Context(), agent, "worker", "", 3, true)
			if err != nil {
				return
			}
			mu.Lock()
			for _, tsk := range tasks {
				claimed[tsk.ID] = agent
			}
			mu.Unlock()
		}("agent-" + strconv.Itoa(i))
	}
	wg.Wait()

	// Every claimed task must be unique — no double-claims
	seen := make(map[string]bool)
	for taskID, agent := range claimed {
		if seen[taskID] {
			t.Fatalf("duplicate claim: task %s claimed by %s", taskID, agent)
		}
		seen[taskID] = true
	}

	// Remaining unclaimed tasks should still be TODO
	remaining, _ := s.Search(t.Context(), SearchParams{Status: StatusTODO})
	if len(claimed)+len(remaining) != 20 {
		t.Fatalf("claimed(%d) + remaining(%d) != 20 tasks", len(claimed), len(remaining))
	}
}

func TestBatchClaimRespectsDepsInConcurrent(t *testing.T) {
	s := newTestService(t)

	// Create 4 tasks in a dependency chain: TASK-2 deps TASK-1, TASK-4 deps TASK-3
	s.Dispatch(t.Context(), "indep-a", "worker", "default", 1, nil)   // TASK-1
	dep1 := "TASK-1"
	s.Dispatch(t.Context(), "dep-on-a", "worker", "default", 2, &dep1) // TASK-2
	s.Dispatch(t.Context(), "indep-b", "worker", "default", 3, nil)   // TASK-3
	dep2 := "TASK-3"
	s.Dispatch(t.Context(), "dep-on-b", "worker", "default", 4, &dep2) // TASK-4

	// 2 agents batch-claim 2 tasks each — only TASK-1 and TASK-3 eligible
	var mu sync.Mutex
	claimed := make(map[string]string)
	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(agent string) {
			defer wg.Done()
			tasks, err := s.ClaimBatch(t.Context(), agent, "worker", "", 2, true)
			if err != nil {
				return
			}
			mu.Lock()
			for _, tsk := range tasks {
				claimed[tsk.ID] = agent
			}
			mu.Unlock()
		}("agent-" + strconv.Itoa(i))
	}
	wg.Wait()

	// Should claim exactly TASK-1 and TASK-3 (the independents)
	if len(claimed) != 2 {
		t.Fatalf("expected 2 claims (independents only), got %d: %v", len(claimed), claimed)
	}

	// TASK-2 and TASK-4 must stay TODO (deps not DONE)
	for _, id := range []string{"TASK-2", "TASK-4"} {
		task, _ := s.View(t.Context(), id)
		if task.Status != StatusTODO {
			t.Fatalf("%s expected TODO (dep unmet), got %s", id, task.Status)
		}
	}
}

func TestBatchCompleteToReview(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "review-me", "worker", "default", 10, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "", true)

	completed, errs := s.BatchComplete(t.Context(), []string{"TASK-1"}, "alice", true)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(completed) != 1 {
		t.Fatalf("expected 1 completed, got %d", len(completed))
	}
	if completed[0].Status != StatusInReview {
		t.Fatalf("expected IN_REVIEW, got %s", completed[0].Status)
	}
}

func TestApproveAll(t *testing.T) {
	s := newTestService(t)

	// Dispatch 3 tasks, claim and submit for review
	for i := 0; i < 3; i++ {
		s.Dispatch(t.Context(), fmt.Sprintf("task-%d", i+1), "worker", "default", 10*i, nil)
	}
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true) // IN_REVIEW
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-2", "alice", true) // IN_REVIEW

	// TASK-3 still TODO — not in review

	// ApproveAll — should approve 2 tasks
	tasks, err := s.ApproveAll(t.Context(), "bob", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 approved, got %d", len(tasks))
	}

	for _, tsk := range tasks {
		task, _ := s.View(t.Context(), tsk.ID)
		if task.Status != StatusDone {
			t.Fatalf("%s expected DONE, got %s", tsk.ID, task.Status)
		}
	}

	// TASK-3 should still be TODO
	task3, _ := s.View(t.Context(), "TASK-3")
	if task3.Status != StatusTODO {
		t.Fatalf("TASK-3 expected TODO, got %s", task3.Status)
	}
}

func TestApproveAllFiltersByProject(t *testing.T) {
	s := newTestService(t)

	s.Dispatch(t.Context(), "proj-a-task", "worker", "project-a", 10, nil)
	s.Dispatch(t.Context(), "proj-b-task", "worker", "project-b", 20, nil)

	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true)

	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-2", "alice", true)

	// ApproveAll only for project-a — should approve 1 task
	tasks, err := s.ApproveAll(t.Context(), "bob", "project-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 approved (project-a), got %d", len(tasks))
	}
	if tasks[0].ID != "TASK-1" {
		t.Fatalf("expected TASK-1, got %s", tasks[0].ID)
	}

	// TASK-2 (project-b) should still be IN_REVIEW
	task2, _ := s.View(t.Context(), "TASK-2")
	if task2.Status != StatusInReview {
		t.Fatalf("TASK-2 expected IN_REVIEW, got %s", task2.Status)
	}
}

func TestApproveAllNoTasksInReview(t *testing.T) {
	s := newTestService(t)

	s.Dispatch(t.Context(), "task", "worker", "default", 10, nil)
	// Not submitted for review

	tasks, err := s.ApproveAll(t.Context(), "bob", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 approved (none in review), got %d", len(tasks))
	}
}

func TestApproveAllRespectsSelfReview(t *testing.T) {
	s := newTestService(t)

	s.Dispatch(t.Context(), "task", "worker", "default", 10, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true)

	// alice tries to approve all — self-review should block
	_, err := s.ApproveAll(t.Context(), "alice", "")
	if err == nil {
		t.Fatal("expected self-review error")
	}
	if !strings.Contains(err.Error(), ErrSelfReview.Error()) {
		t.Fatalf("expected ErrSelfReview, got %v", err)
	}
}

func TestApproveAllSelfReviewAllowedWithEnv(t *testing.T) {
	t.Setenv("KANBAN_ALLOW_SELF_REVIEW", "true")
	s := newTestService(t)

	s.Dispatch(t.Context(), "task", "worker", "default", 10, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true)

	tasks, err := s.ApproveAll(t.Context(), "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 approved, got %d", len(tasks))
	}
}
