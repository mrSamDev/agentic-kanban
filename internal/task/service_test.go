package task

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"agent-kanban/internal/storage"
)

func newTestDB(t *testing.T) *storage.DB {
	t.Helper()
	path := "/tmp/kanban-test-" + strconv.Itoa(os.Getpid()) + "-" + t.Name() + ".db"
	os.Remove(path)
	db, err := storage.Open(path, false)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.Remove(path)
	})
	return db
}

func newTestService(t *testing.T) *Service {
	db := newTestDB(t)
	return NewService(db.DB, 0)
}

func TestDispatch(t *testing.T) {
	s := newTestService(t)
	task, err := s.Dispatch(t.Context(), "test task", "worker", "default", 50)
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
	t1, _ := s.Dispatch(t.Context(), "a", "worker", "default", 100)
	t2, _ := s.Dispatch(t.Context(), "b", "worker", "default", 100)
	t3, _ := s.Dispatch(t.Context(), "c", "worker", "default", 100)
	if t1.ID != "TASK-1" || t2.ID != "TASK-2" || t3.ID != "TASK-3" {
		t.Fatalf("expected sequential IDs, got %s, %s, %s", t1.ID, t2.ID, t3.ID)
	}
}

func TestClaimNext(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "urgent", "worker", "default", 1)
	s.Dispatch(t.Context(), "normal", "worker", "default", 100)

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
	s.Dispatch(t.Context(), "worker task", "worker", "default", 10)
	s.Dispatch(t.Context(), "reviewer task", "reviewer", "default", 5)

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
		s.Dispatch(t.Context(), "task", "worker", "default", 100)
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

func TestComplete(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1)
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
	s.Dispatch(t.Context(), "task", "worker", "default", 1)

	_, err := s.Complete(t.Context(), "TASK-1", "alice", false)
	if err != ErrNotAssigned {
		t.Fatalf("expected ErrNotAssigned, got %v", err)
	}
}

func TestCompleteWrongAgent(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "worker", "default", 1)
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
	s.Dispatch(t.Context(), "task", "worker", "default", 1)
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
	s.Dispatch(t.Context(), "task", "worker", "default", 1)
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
	s.Dispatch(t.Context(), "task", "worker", "default", 1)
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
	s.Dispatch(t.Context(), "task", "worker", "default", 1)
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
	s.Dispatch(t.Context(), "hotfix", "worker", "default", 1)
	s.Dispatch(t.Context(), "feature", "worker", "default", 100)
	s.Dispatch(t.Context(), "review", "reviewer", "default", 50)

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
	s.Dispatch(t.Context(), "task", "worker", "default", 1)
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
	s.Dispatch(t.Context(), "task", "worker", "default", 1)
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

func TestLeaseReclaim(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "stale task", "worker", "default", 1)
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
	s.Dispatch(t.Context(), "task", "worker", "default", 1)

	// Can't complete a TODO task without claiming
	_, err := s.Complete(t.Context(), "TASK-1", "alice", false)
	if err == nil {
		t.Fatal("expected error completing unassigned task")
	}
}

func TestReviewerCannotClaimInReview(t *testing.T) {
	s := newTestService(t)

	// Worker dispatches + claims + completes with review.
	s.Dispatch(t.Context(), "worker task needing review", "worker", "default", 50)
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
