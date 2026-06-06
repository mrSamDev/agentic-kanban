package bootstrap

import (
	"context"
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agent-kanban/internal/storage"
	"agent-kanban/internal/task"
)

type Harness string

const (
	HarnessPi      Harness = "pi"
	HarnessClaude  Harness = "claude"
	HarnessGeneric Harness = "generic"
)

var ValidHarnesses = map[Harness]bool{
	HarnessPi: true, HarnessClaude: true, HarnessGeneric: true,
}

type InitOptions struct {
	Dir      string
	DBPath   string
	Harness  Harness
	PlanPath string
}

func Init(opts InitOptions) error {
	if opts.Dir == "" {
		opts.Dir = "."
	}
	if opts.DBPath == "" {
		opts.DBPath = filepath.Join(opts.Dir, ".kanban", "kanban.db")
	}

	// Validate harness before any I/O.
	if opts.Harness != "" && !ValidHarnesses[opts.Harness] {
		return fmt.Errorf("invalid harness: %q (choose pi, claude, or generic)", opts.Harness)
	}

	// Open DB once, reuse for plan dispatch.
	db, err := storage.Open(opts.DBPath, false)
	if err != nil {
		return fmt.Errorf("create db: %w", err)
	}
	defer db.Close()

	// Resolve harness interactively if not set.
	harness := opts.Harness
	if harness == "" {
		h, err := promptHarness()
		if err != nil {
			return err
		}
		harness = h
	}

	// Scaffold skill files.
	if err := scaffoldHarness(harness, opts.Dir); err != nil {
		return fmt.Errorf("scaffold %s harness: %w", harness, err)
	}

	// Optionally dispatch tasks from a plan file.
	if opts.PlanPath != "" {
		if err := dispatchPlan(db.DB, opts.PlanPath); err != nil {
			return fmt.Errorf("dispatch plan: %w", err)
		}
	}

	return nil
}

func promptHarness() (Harness, error) {
	fmt.Print("Which agent harness? [pi / claude / generic]: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	input = strings.TrimSpace(strings.ToLower(input))

	h := Harness(input)
	if !ValidHarnesses[h] {
		return "", fmt.Errorf("invalid harness: %q (choose pi, claude, or generic)", input)
	}
	return h, nil
}

func dispatchPlan(sqlDB *sql.DB, planPath string) error {
	tasks, _, err := ParsePlan(planPath)
	if err != nil {
		return err
	}

	svc := task.NewService(sqlDB, 0)
	for _, pt := range tasks {
		role := pt.Role
		if role == "" {
			role = "worker"
		}
		priority := pt.Priority
		if priority == 0 {
			priority = 100
		}
		if _, err := svc.Dispatch(context.Background(), pt.Title, role, "default", priority); err != nil {
			return fmt.Errorf("dispatch %q: %w", pt.Title, err)
		}
	}
	return nil
}

func scaffoldHarness(harness Harness, dir string) error {
	for role, skills := range roleSkills {
		base := harnessBase(harness, dir)
		roleDir := filepath.Join(base, role, "skills")
		if err := os.MkdirAll(roleDir, 0755); err != nil {
			return fmt.Errorf("create %s dir: %w", roleDir, err)
		}
		for filename, content := range skills {
			path := filepath.Join(roleDir, filename)
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
		}
	}
	return nil
}

func harnessBase(h Harness, projectDir string) string {
	switch h {
	case HarnessPi:
		return filepath.Join(projectDir, ".pi", "agents")
	case HarnessClaude:
		return filepath.Join(projectDir, ".claude", "agents")
	default:
		return filepath.Join(projectDir, "agents")
	}
}

// Each role gets only the skills matching its responsibility — worker cannot complete, manager cannot claim.
var roleSkills = map[string]map[string]string{
	"manager": {
		"dispatch-task.md":  SkillDispatchTask,
		"review-backlog.md": SkillReviewBacklog,
		"view-task.md":      SkillViewTask,
	},
	"worker": {
		"claim-next-task.md": SkillClaimNextTask,
		"log-progress.md":    SkillLogProgress,
		"block-task.md":      SkillBlockTask,
		"complete-task.md":   SkillCompleteTask,
	},
	"reviewer": {
		"claim-review.md": SkillClaimReview,
		"approve-task.md": SkillApproveTask,
		"reject-task.md":  SkillRejectTask,
	},
}