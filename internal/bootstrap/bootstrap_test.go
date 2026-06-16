package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"agent-kanban/internal/storage"
	"agent-kanban/internal/task"
)

func TestParsePlanMarkdownHeadings(t *testing.T) {
	content := `## Set up auth [p1]
- Implement login
- Add JWT middleware

## Add CI pipeline

## Review everything [p1]
`

	tmp := t.TempDir()
	path := filepath.Join(tmp, "plan.md")
	os.WriteFile(path, []byte(content), 0644)

	tasks, notes, err := ParsePlan(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	// Task 1: [p1] hint
	if tasks[0].Title != "Set up auth" {
		t.Fatalf("expected 'Set up auth', got %q", tasks[0].Title)
	}
	if tasks[0].Priority != 1 {
		t.Fatalf("expected priority 1, got %d", tasks[0].Priority)
	}
	if tasks[0].Role != "worker" {
		t.Fatalf("expected default role 'worker', got %q", tasks[0].Role)
	}

	// Task 2: no priority hint
	if tasks[1].Title != "Add CI pipeline" {
		t.Fatalf("expected 'Add CI pipeline', got %q", tasks[1].Title)
	}
	if tasks[1].Priority != 100 {
		t.Fatalf("expected default priority 100, got %d", tasks[1].Priority)
	}

	// Task 3: [p1] hint
	if tasks[2].Title != "Review everything" {
		t.Fatalf("expected 'Review everything', got %q", tasks[2].Title)
	}
	if tasks[2].Priority != 1 {
		t.Fatalf("expected priority 1 from [p1], got %d", tasks[2].Priority)
	}

	// Notes from list items
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes from list items, got %d", len(notes))
	}
}

func TestParsePlanJSON(t *testing.T) {
	content := `[
		{"title": "Fix auth bug", "role": "worker", "priority": 1},
		{"title": "Review PR", "role": "reviewer", "priority": 5}
	]`

	tmp := t.TempDir()
	path := filepath.Join(tmp, "plan.json")
	os.WriteFile(path, []byte(content), 0644)

	tasks, _, err := ParsePlan(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].Title != "Fix auth bug" || tasks[0].Role != "worker" || tasks[0].Priority != 1 {
		t.Fatalf("task 0 mismatch: %+v", tasks[0])
	}
	if tasks[1].Title != "Review PR" || tasks[1].Role != "reviewer" || tasks[1].Priority != 5 {
		t.Fatalf("task 1 mismatch: %+v", tasks[1])
	}
}

func TestParsePlanEmptyFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.md")
	os.WriteFile(path, []byte("Some text without headings"), 0644)

	_, _, err := ParsePlan(path)
	if err == nil {
		t.Fatal("expected error for plan with no tasks")
	}
}

func TestInitCreatesDBAndSkills(t *testing.T) {
	tmp := t.TempDir()

	err := Init(InitOptions{
		Dir:     tmp,
		DBPath:  filepath.Join(tmp, ".kanban", "kanban.db"),
		Harness: HarnessGeneric,
	})
	if err != nil {
		t.Fatal(err)
	}

	// DB created.
	if _, err := os.Stat(filepath.Join(tmp, ".kanban", "kanban.db")); os.IsNotExist(err) {
		t.Fatal("DB not created")
	}

	// Skills scaffolded flat under .agents/skills/.
	skillsDir := filepath.Join(tmp, ".agents", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no skill files")
	}

	// Agent files scaffolded.
	agentsDir := filepath.Join(tmp, ".agents")
	for _, agent := range []string{"manager.md", "worker.md", "reviewer.md"} {
		if _, err := os.Stat(filepath.Join(agentsDir, agent)); os.IsNotExist(err) {
			t.Fatalf("agent not created: %s", agent)
		}
	}
}

func TestInitWithPlanDispatchesTasks(t *testing.T) {
	tmp := t.TempDir()
	planPath := filepath.Join(tmp, "plan.md")
	os.WriteFile(planPath, []byte("## Test task\n- Note one\n"), 0644)

	err := Init(InitOptions{
		Dir:      tmp,
		DBPath:   filepath.Join(tmp, ".kanban", "kanban.db"),
		Harness:  HarnessGeneric,
		PlanPath: planPath,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify DB has the task by searching (re-open DB).
	db, err := storage.Open(filepath.Join(tmp, ".kanban", "kanban.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := task.NewService(db.DB, db.Reader(), 0, "", nil)
	tasks, err := svc.Search(t.Context(), task.SearchParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task from plan, got %d", len(tasks))
	}
	if tasks[0].Title != "Test task" {
		t.Fatalf("expected 'Test task', got %q", tasks[0].Title)
	}
}

func TestReInitScaffoldsSkills(t *testing.T) {
	tmp := t.TempDir()

	err := Init(InitOptions{
		Dir:     tmp,
		DBPath:  filepath.Join(tmp, ".kanban", "kanban.db"),
		Harness: HarnessGeneric,
	})
	if err != nil {
		t.Fatal(err)
	}

	// DB created by init.
	if _, err := os.Stat(filepath.Join(tmp, ".kanban", "kanban.db")); os.IsNotExist(err) {
		t.Fatal("DB not created by init")
	}

	skillsDir := filepath.Join(tmp, ".agents", "skills")

	// Remove one skill to simulate stale scaffold.
	err = os.Remove(filepath.Join(skillsDir, "setup-hooks.md"))
	if err != nil {
		t.Fatalf("remove setup-hooks.md: %v", err)
	}

	// Re-init.
	err = ReInit(InitOptions{
		Dir:     tmp,
		DBPath:  filepath.Join(tmp, ".kanban", "kanban.db"),
		Harness: HarnessGeneric,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Deleted skill restored.
	if _, err := os.Stat(filepath.Join(skillsDir, "setup-hooks.md")); os.IsNotExist(err) {
		t.Fatal("setup-hooks.md not restored by re-init")
	}

	// DB still intact.
	if _, err := os.Stat(filepath.Join(tmp, ".kanban", "kanban.db")); os.IsNotExist(err) {
		t.Fatal("DB missing after re-init")
	}

	// Agent files still present.
	agentsDir := filepath.Join(tmp, ".agents")
	for _, agent := range []string{"manager.md", "worker.md", "reviewer.md"} {
		if _, err := os.Stat(filepath.Join(agentsDir, agent)); os.IsNotExist(err) {
			t.Fatalf("agent missing after re-init: %s", agent)
		}
	}
}