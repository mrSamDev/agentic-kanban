package task

import (
	"fmt"
	"testing"
	"time"
)

func TestE2EFullWorkflow(t *testing.T) {
	s := newTestService(t)

	// Phase 1: Dispatch tasks with dependency chain
	t1, err := s.Dispatch(t.Context(), "Scaffold project", "", "worker", "default", 10, nil)
	if err != nil {
		t.Fatalf("dispatch 1: %v", err)
	}
	t.Logf("Dispatched: %s — %s", t1.ID, t1.Title)

	depID := t1.ID
	t2, err := s.Dispatch(t.Context(), "Build feature on scaffold", "", "worker", "default", 10, &depID)
	if err != nil {
		t.Fatalf("dispatch 2: %v", err)
	}
	t.Logf("Dispatched: %s — %s (depends on %s)", t2.ID, t2.Title, depID)

	t3, err := s.Dispatch(t.Context(), "Independent cleanup", "", "worker", "default", 5, nil)
	if err != nil {
		t.Fatalf("dispatch 3: %v", err)
	}
	t.Logf("Dispatched: %s — %s (independent)", t3.ID, t3.Title)

	// Phase 2: Claim one task — should get TASK-3 (priority 5, independent)
	// Then TASK-1 (priority 10, independent). TASK-2 blocked behind TASK-1.
	claimed1, err := s.ClaimNext(t.Context(), "worker-1", "worker", "")
	if err != nil {
		t.Fatalf("claim 1: %v", err)
	}
	if claimed1.ID != t3.ID {
		t.Fatalf("expected %s (highest prio, independent), got %s", t3.ID, claimed1.ID)
	}
	if claimed1.Status != StatusInProgress {
		t.Fatalf("expected IN_PROGRESS, got %s", claimed1.Status)
	}
	if *claimed1.AssignedAgent != "worker-1" {
		t.Fatalf("expected worker-1, got %s", *claimed1.AssignedAgent)
	}
	t.Logf("Claimed: %s by worker-1", claimed1.ID)

	// Phase 3: Log progress
	task, err := s.LogProgress(t.Context(), claimed1.ID, "worker-1", "Starting work", "PROGRESS")
	if err != nil {
		t.Fatalf("log progress: %v", err)
	}
	if task.LeaseUntil == nil || task.LeaseUntil.Before(time.Now()) {
		t.Fatal("lease should be valid after progress")
	}
	t.Logf("Progress logged for %s, lease until %v", claimed1.ID, task.LeaseUntil)

	// Phase 4: Complete independent task without review
	task, err = s.Complete(t.Context(), claimed1.ID, "worker-1", false)
	if err != nil {
		t.Fatalf("complete 1: %v", err)
	}
	if task.Status != StatusDone {
		t.Fatalf("expected DONE, got %s", task.Status)
	}
	t.Logf("Completed: %s (direct DONE)", claimed1.ID)

	// Phase 5: Claim scaffold (independent — should be available now)
	claimed2, err := s.ClaimNext(t.Context(), "worker-1", "worker", "")
	if err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	if claimed2.ID != t1.ID {
		t.Fatalf("expected %s (scaffold), got %s", t1.ID, claimed2.ID)
	}
	t.Logf("Claimed: %s by worker-1", claimed2.ID)

	// Phase 6: Complete scaffold with review submission
	task, err = s.Complete(t.Context(), claimed2.ID, "worker-1", true)
	if err != nil {
		t.Fatalf("complete 2: %v", err)
	}
	if task.Status != StatusInReview {
		t.Fatalf("expected IN_REVIEW, got %s", task.Status)
	}
	t.Logf("Submitted for review: %s (status IN_REVIEW)", claimed2.ID)

	// Phase 7: Review approval — worker-1 cannot approve own task
	_, err = s.ReviewApprove(t.Context(), claimed2.ID, "worker-1")
	if err != ErrSelfReview {
		t.Fatalf("expected ErrSelfReview for self-approve, got %v", err)
	}
	t.Logf("Self-review blocked for %s by worker-1 (expected)", claimed2.ID)

	// Phase 8: Different agent (reviewer-1) approves scaffold — now DONE
	task, err = s.ReviewApprove(t.Context(), claimed2.ID, "reviewer-1")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if task.Status != StatusDone {
		t.Fatalf("expected DONE after approve, got %s", task.Status)
	}
	t.Logf("Approved: %s by reviewer-1 → DONE", claimed2.ID)

	// Phase 9: Claim blocked task — TASK-2 now available since TASK-1 is DONE
	claimed3, err := s.ClaimNext(t.Context(), "worker-2", "worker", "")
	if err != nil {
		t.Fatalf("claim 3: %v", err)
	}
	if claimed3.ID != t2.ID {
		t.Fatalf("expected %s (feature, dep done), got %s", t2.ID, claimed3.ID)
	}
	t.Logf("Claimed: %s by worker-2 (dep TASK-1 now DONE)", claimed3.ID)

	// Phase 10: Complete feature task
	task, err = s.Complete(t.Context(), claimed3.ID, "worker-2", false)
	if err != nil {
		t.Fatalf("complete 3: %v", err)
	}
	if task.Status != StatusDone {
		t.Fatalf("expected DONE, got %s", task.Status)
	}
	t.Logf("Completed: %s (direct DONE)", claimed3.ID)

	// Phase 11: Verify final board state
	stats, err := s.Burndown(t.Context(), "")
	if err != nil {
		t.Fatalf("burndown: %v", err)
	}
	if stats.Total != 3 {
		t.Fatalf("expected 3 total tasks, got %d", stats.Total)
	}
	if stats.DoneCount != 3 {
		t.Fatalf("expected 3 DONE tasks, got %d", stats.DoneCount)
	}
	if stats.PercentDone != 100.0 {
		t.Fatalf("expected 100%% done, got %.1f%%", stats.PercentDone)
	}
	t.Logf("Final: %d/%d tasks done (100%%%%) ", stats.DoneCount, stats.Total)
}


func TestE2EFullWorkflowWithReject(t *testing.T) {
	s := newTestService(t)

	// Dispatch + claim + complete with review
	s.Dispatch(t.Context(), "needs rework", "", "worker", "default", 10, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true)

	// Verify IN_REVIEW
	task, _ := s.View(t.Context(), "TASK-1")
	if task.Status != StatusInReview {
		t.Fatalf("expected IN_REVIEW, got %s", task.Status)
	}

	// Reviewer rejects
	task, err := s.ReviewReject(t.Context(), "TASK-1", "dave", "Incomplete — missing error handling")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if task.Status != StatusTODO {
		t.Fatalf("expected TODO after reject, got %s", task.Status)
	}

	// Worker claims it again (back in queue)
	claimed, err := s.ClaimNext(t.Context(), "alice", "worker", "")
	if err != nil {
		t.Fatalf("re-claim: %v", err)
	}
	if claimed.ID != "TASK-1" {
		t.Fatalf("expected TASK-1, got %s", claimed.ID)
	}

	// Complete directly (no review this time)
	s.Complete(t.Context(), "TASK-1", "alice", false)

	task, _ = s.View(t.Context(), "TASK-1")
	if task.Status != StatusDone {
		t.Fatalf("expected DONE, got %s", task.Status)
	}

	// Verify burndown
	stats, _ := s.Burndown(t.Context(), "")
	if stats.DoneCount != 1 || stats.Total != 1 {
		t.Fatalf("expected 1/1 done, got %d/%d", stats.DoneCount, stats.Total)
	}
}

// --- Benchmarks ---


func TestE2EFullWorkflowBatchClaim(t *testing.T) {
	s := newTestService(t)

	// Dispatch 5 independent tasks
	for i := 0; i < 5; i++ {
		_, err := s.Dispatch(t.Context(), fmt.Sprintf("task-%d", i+1), "", "worker", "default", 100, nil)
		if err != nil {
			t.Fatalf("dispatch %d: %v", i+1, err)
		}
	}

	// Batch claim 3
	tasks, err := s.ClaimBatch(t.Context(), "batch-bot", "worker", "", 3, true)
	if err != nil {
		t.Fatalf("batch claim: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	for _, tsk := range tasks {
		if tsk.Status != StatusInProgress {
			t.Fatalf("expected IN_PROGRESS, got %s", tsk.Status)
		}
	}

	// Complete all 3 with review
	for _, tsk := range tasks {
		_, err := s.Complete(t.Context(), tsk.ID, "batch-bot", true)
		if err != nil {
			t.Fatalf("complete %s: %v", tsk.ID, err)
		}
	}

	// Approve all (need env var since batch-bot claimed)
	t.Setenv("TASKBIND_ALLOW_SELF_REVIEW", "true")
	approved, errs := s.ApproveAll(t.Context(), "batch-bot", "")
	if len(errs) > 0 {
		t.Fatalf("approve all: %v", errs)
	}
	if len(approved) != 3 {
		t.Fatalf("expected 3 approved, got %d", len(approved))
	}

	// Remaining 2 tasks
	stats, _ := s.Burndown(t.Context(), "")
	if stats.DoneCount != 3 {
		t.Fatalf("expected 3 done, got %d", stats.DoneCount)
	}
	if stats.Total != 5 {
		t.Fatalf("expected 5 total, got %d", stats.Total)
	}
}
