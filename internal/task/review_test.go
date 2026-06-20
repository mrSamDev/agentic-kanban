package task

import (
	"fmt"
	"strings"
	"testing"
)

func TestReviewApprove(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
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
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
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
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
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
	t.Setenv("TASKBIND_ALLOW_SELF_REVIEW", "true")
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
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
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
	s.db.Exec("UPDATE tasks SET status = 'IN_REVIEW' WHERE id = 'TASK-1'")

	task, err := s.ReviewApprove(t.Context(), "TASK-1", "alice")
	if err != nil {
		t.Fatalf("expected no error for direct IN_REVIEW, got %v", err)
	}
	if task.Status != StatusDone {
		t.Fatalf("expected DONE, got %s", task.Status)
	}
}


func TestReviewerCannotClaimInReview(t *testing.T) {
	s := newTestService(t)

	// Worker dispatches + claims + completes with review.
	s.Dispatch(t.Context(), "worker task needing review", "", "worker", "default", 50, nil)
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


func TestApproveAll(t *testing.T) {
	s := newTestService(t)

	// Dispatch 3 tasks, claim and submit for review
	for i := 0; i < 3; i++ {
		s.Dispatch(t.Context(), fmt.Sprintf("task-%d", i+1), "", "worker", "default", 10*i, nil)
	}
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true) // IN_REVIEW
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-2", "alice", true) // IN_REVIEW

	// TASK-3 still TODO — not in review

	// ApproveAll — should approve 2 tasks
	tasks, errs := s.ApproveAll(t.Context(), "bob", "")
	if len(errs) > 0 {
		t.Fatal(errs[0])
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

	s.Dispatch(t.Context(), "proj-a-task", "", "worker", "project-a", 10, nil)
	s.Dispatch(t.Context(), "proj-b-task", "", "worker", "project-b", 20, nil)

	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true)

	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-2", "alice", true)

	// ApproveAll only for project-a — should approve 1 task
	tasks, errs := s.ApproveAll(t.Context(), "bob", "project-a")
	if len(errs) > 0 {
		t.Fatal(errs[0])
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

	s.Dispatch(t.Context(), "task", "", "worker", "default", 10, nil)
	// Not submitted for review

	tasks, errs := s.ApproveAll(t.Context(), "bob", "")
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 approved (none in review), got %d", len(tasks))
	}
}


func TestApproveAllRespectsSelfReview(t *testing.T) {
	s := newTestService(t)

	s.Dispatch(t.Context(), "task", "", "worker", "default", 10, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true)

	// alice tries to approve all — self-review should block
	_, errs := s.ApproveAll(t.Context(), "alice", "")
	if len(errs) == 0 {
		t.Fatal("expected self-review error")
	}
	if !strings.Contains(errs[0].Error(), ErrSelfReview.Error()) {
		t.Fatalf("expected ErrSelfReview, got %v", errs[0])
	}
}


func TestApproveAllSelfReviewAllowedWithEnv(t *testing.T) {
	t.Setenv("TASKBIND_ALLOW_SELF_REVIEW", "true")
	s := newTestService(t)

	s.Dispatch(t.Context(), "task", "", "worker", "default", 10, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true)

	tasks, errs := s.ApproveAll(t.Context(), "alice", "")
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 approved, got %d", len(tasks))
	}
}

