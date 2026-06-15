package task

import (
	"testing"
)

func TestLintCleanBoard(t *testing.T) {
	db := newTestDB(t)
	s := NewService(db.DB, db.Reader(), 0, "")
	s.Dispatch(t.Context(), "task a", "worker", "default", 10, nil)
	s.Dispatch(t.Context(), "task b", "worker", "default", 20, nil)

	issues, err := LintPlan(t.Context(), db.DB, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no issues on clean board, got %v", issues)
	}
}

func TestLintUnknownDep(t *testing.T) {
	db := newTestDB(t)
	s := NewService(db.DB, db.Reader(), 0, "")
	ghost := "TASK-99"
	s.Dispatch(t.Context(), "deploy", "worker", "default", 10, &ghost)

	issues, err := LintPlan(t.Context(), db.DB, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Severity != "warn" {
		t.Fatalf("expected warn, got %s", issues[0].Severity)
	}
	if issues[0].TaskID != "TASK-1" {
		t.Fatalf("expected TASK-1, got %s", issues[0].TaskID)
	}
}

func TestLintCycleDetected(t *testing.T) {
	db := newTestDB(t)
	s := NewService(db.DB, db.Reader(), 0, "")

	// TASK-1 and TASK-2 will be created; we then manually set depends_on to form a cycle
	s.Dispatch(t.Context(), "a", "worker", "default", 10, nil)
	s.Dispatch(t.Context(), "b", "worker", "default", 20, nil)
	// TASK-1 -> TASK-2 -> TASK-1
	db.DB.Exec("UPDATE tasks SET depends_on = 'TASK-2' WHERE id = 'TASK-1'")
	db.DB.Exec("UPDATE tasks SET depends_on = 'TASK-1' WHERE id = 'TASK-2'")

	issues, err := LintPlan(t.Context(), db.DB, "")
	if err != nil {
		t.Fatal(err)
	}

	var cycleErrors int
	for _, iss := range issues {
		if iss.Severity == "error" {
			cycleErrors++
		}
	}
	if cycleErrors == 0 {
		t.Fatalf("expected cycle error, got issues: %v", issues)
	}
}

func TestLintMissingRole(t *testing.T) {
	db := newTestDB(t)
	s := NewService(db.DB, db.Reader(), 0, "")
	s.Dispatch(t.Context(), "task", "worker", "default", 10, nil)
	db.DB.Exec("UPDATE tasks SET role_boundary = '' WHERE id = 'TASK-1'")

	issues, err := LintPlan(t.Context(), db.DB, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Severity != "warn" || issues[0].TaskID != "TASK-1" {
		t.Fatalf("expected 1 warn for missing role, got %v", issues)
	}
}

func TestLintErrorsBeforeWarns(t *testing.T) {
	db := newTestDB(t)
	s := NewService(db.DB, db.Reader(), 0, "")

	// Create a cycle (error) and a missing-dep (warn)
	s.Dispatch(t.Context(), "a", "worker", "default", 10, nil)
	s.Dispatch(t.Context(), "b", "worker", "default", 20, nil)
	db.DB.Exec("UPDATE tasks SET depends_on = 'TASK-2' WHERE id = 'TASK-1'")
	db.DB.Exec("UPDATE tasks SET depends_on = 'TASK-1' WHERE id = 'TASK-2'")

	ghost := "TASK-99"
	s.Dispatch(t.Context(), "c", "worker", "default", 30, &ghost)

	issues, err := LintPlan(t.Context(), db.DB, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) == 0 {
		t.Fatal("expected issues, got none")
	}
	// First issue must be an error
	if issues[0].Severity != "error" {
		t.Fatalf("expected errors first, first issue was %s", issues[0].Severity)
	}
}

func TestBurndownEmpty(t *testing.T) {
	s := newTestService(t)
	stats, err := s.Burndown(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 0 || stats.DoneCount != 0 || stats.PercentDone != 0 {
		t.Fatalf("expected zero stats on empty board, got %+v", stats)
	}
}

func TestBurndownCounts(t *testing.T) {
	s := newTestService(t)
	s.Dispatch(t.Context(), "a", "worker", "default", 10, nil)
	s.Dispatch(t.Context(), "b", "worker", "default", 20, nil)
	s.Dispatch(t.Context(), "c", "worker", "default", 30, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", false) // DONE

	stats, err := s.Burndown(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 3 {
		t.Fatalf("expected total 3, got %d", stats.Total)
	}
	if stats.DoneCount != 1 {
		t.Fatalf("expected done 1, got %d", stats.DoneCount)
	}
	want := 33.33
	if stats.PercentDone < 33.0 || stats.PercentDone > 34.0 {
		t.Fatalf("expected ~33%% done, got %.2f", want)
	}
}
