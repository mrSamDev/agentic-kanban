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
	s1 := filepath.Join(dir, "s1")
	s2 := filepath.Join(dir, "s2")
	makeHookD(t, hooksDir, "task.created", "a", "touch "+s1)
	makeHookD(t, hooksDir, "task.created", "b", "touch "+s2)
	s.SetHooksDir(hooksDir)

	if _, err := s.Dispatch(t.Context(), "task", "worker", "default", 50, nil); err != nil {
		t.Fatal(err)
	}
	waitForSentinel(t, s1)
	waitForSentinel(t, s2)
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
