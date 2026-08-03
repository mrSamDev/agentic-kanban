package task

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestBatchCompletePartialFailure(t *testing.T) {
	s := newTestService(t)

	// Dispatch 3 tasks
	for i := 0; i < 3; i++ {
		s.Dispatch(t.Context(), fmt.Sprintf("task-%d", i+1), "", "worker", "default", 10, nil)
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
	s.Dispatch(t.Context(), "low", "", "worker", "default", 100, nil)
	s.Dispatch(t.Context(), "high", "", "worker", "default", 1, nil)
	s.Dispatch(t.Context(), "medium", "", "worker", "default", 50, nil)

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
		s.Dispatch(t.Context(), fmt.Sprintf("task-%d", i+1), "", "worker", "default", 100, nil)
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
	s.Dispatch(t.Context(), "indep-a", "", "worker", "default", 1, nil) // TASK-1
	dep1 := "TASK-1"
	s.Dispatch(t.Context(), "dep-on-a", "", "worker", "default", 2, &dep1) // TASK-2
	s.Dispatch(t.Context(), "indep-b", "", "worker", "default", 3, nil)    // TASK-3
	dep2 := "TASK-3"
	s.Dispatch(t.Context(), "dep-on-b", "", "worker", "default", 4, &dep2) // TASK-4

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
	s.Dispatch(t.Context(), "review-me", "", "worker", "default", 10, nil)
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

