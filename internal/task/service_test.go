package task

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestDispatch(t *testing.T) {
	s := newTestService(t)
	task, err := s.Dispatch(t.Context(), "test task", "", "worker", "default", 50, nil)
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
	t1, _ := s.Dispatch(t.Context(), "a", "", "worker", "default", 100, nil)
	t2, _ := s.Dispatch(t.Context(), "b", "", "worker", "default", 100, nil)
	t3, _ := s.Dispatch(t.Context(), "c", "", "worker", "default", 100, nil)
	if t1.ID != "TASK-1" || t2.ID != "TASK-2" || t3.ID != "TASK-3" {
		t.Fatalf("expected sequential IDs, got %s, %s, %s", t1.ID, t2.ID, t3.ID)
	}
}

func TestComplete(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
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
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
	_, err := s.Complete(t.Context(), "TASK-1", "alice", false)
	if err != ErrNotAssigned {
		t.Fatalf("expected ErrNotAssigned, got %v", err)
	}
}


func TestCompleteWrongAgent(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
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
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
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
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
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
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
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
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
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
	s.Dispatch(t.Context(), "hotfix", "", "worker", "default", 1, nil)
	s.Dispatch(t.Context(), "feature", "", "worker", "default", 100, nil)
	s.Dispatch(t.Context(), "review", "", "reviewer", "default", 50, nil)
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


func TestErrNotFound(t *testing.T) {
	s := newTestService(t)
	_, err := s.View(t.Context(), "NONEXIST")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}


func TestErrInvalidState(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
	// Can't complete a TODO task without claiming
	_, err := s.Complete(t.Context(), "TASK-1", "alice", false)
	if err == nil {
		t.Fatal("expected error completing unassigned task")
	}
}


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
			task, err := s.Dispatch(t.Context(), "stability task", "", "worker", "default", 100, nil)
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


func BenchmarkClaimBatchSize(b *testing.B) {
	ctx := context.Background()

	for _, batchSize := range []int{1, 5, 10} {
		b.Run(fmt.Sprintf("size-%d", batchSize), func(b *testing.B) {
			totalTasks := batchSize * 3

			for i := 0; i < b.N; i++ {
				b.StopTimer()
				s := newBenchService(b)
				for j := 0; j < totalTasks; j++ {
					s.Dispatch(ctx, fmt.Sprintf("t-%d-%d", i, j), "", "worker", "default", 100, nil)
				}

				b.StartTimer()
				claimed, err := s.ClaimBatch(ctx, "bench-agent", "worker", "", batchSize, true)
				b.StopTimer()
				if err != nil {
					b.Fatalf("ClaimBatch: %v", err)
				}
				_ = claimed
			}
		})
	}
}

func BenchmarkClaimBatchConcurrent(b *testing.B) {
	for _, agentCount := range []int{2, 5, 10} {
		b.Run(fmt.Sprintf("agents-%d", agentCount), func(b *testing.B) {
			ctx := context.Background()

			for i := 0; i < b.N; i++ {
				s := newBenchService(b)
				totalTasks := agentCount * 4 // enough for all agents to get some
				for j := 0; j < totalTasks; j++ {
					if _, err := s.Dispatch(ctx, fmt.Sprintf("t-%d-%d", i, j), "", "worker", "default", 100, nil); err != nil {
						b.Fatalf("dispatch: %v", err)
					}
				}

				b.StopTimer()
				var wg sync.WaitGroup
				errs := make(chan error, agentCount)
				b.StartTimer()

				for a := 0; a < agentCount; a++ {
					wg.Add(1)
					go func(agent string) {
						defer wg.Done()
						claimed, err := s.ClaimBatch(ctx, agent, "worker", "", 3, true)
						if err != nil {
							errs <- err
							return
						}
						_ = claimed
					}(fmt.Sprintf("agent-%d", a))
				}
				wg.Wait()
				b.StopTimer()

				close(errs)
				for err := range errs {
					b.Fatal(err)
				}
			}
		})
	}
}

