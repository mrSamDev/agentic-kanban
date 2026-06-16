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

	if opts.Harness != "" && !ValidHarnesses[opts.Harness] {
		return fmt.Errorf("invalid harness: %q (choose pi, claude, or generic)", opts.Harness)
	}

	db, err := storage.Open(opts.DBPath, false)
	if err != nil {
		return fmt.Errorf("create db: %w", err)
	}
	defer func() { _ = db.Close() }()

	harness := opts.Harness
	if harness == "" {
		h, err := promptHarness()
		if err != nil {
			return err
		}
		harness = h
	}

	if err := scaffoldHarness(harness, opts.Dir); err != nil {
		return fmt.Errorf("scaffold %s harness: %w", harness, err)
	}

	if opts.PlanPath != "" {
		if err := dispatchPlan(db.DB, opts.PlanPath); err != nil {
			return fmt.Errorf("dispatch plan: %w", err)
		}
	}

	return nil
}

func ReInit(opts InitOptions) error {
	if opts.Dir == "" {
		opts.Dir = "."
	}
	if opts.DBPath == "" {
		opts.DBPath = filepath.Join(opts.Dir, ".kanban", "kanban.db")
	}

	if opts.Harness != "" && !ValidHarnesses[opts.Harness] {
		return fmt.Errorf("invalid harness: %q (choose pi, claude, or generic)", opts.Harness)
	}

	harness := opts.Harness
	if harness == "" {
		h, err := promptHarness()
		if err != nil {
			return err
		}
		harness = h
	}

	if err := scaffoldHarness(harness, opts.Dir); err != nil {
		return fmt.Errorf("re-scaffold %s harness: %w", harness, err)
	}

	if opts.PlanPath != "" {
		db, err := storage.Open(opts.DBPath, false)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer func() { _ = db.Close() }()
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

	svc := task.NewService(sqlDB, nil, 0, "", nil)
	for _, pt := range tasks {
		role := pt.Role
		if role == "" {
			role = "worker"
		}
		priority := pt.Priority
		if priority == 0 {
			priority = 100
		}
		if _, err := svc.Dispatch(context.Background(), pt.Title, role, "default", priority, nil); err != nil {
			return fmt.Errorf("dispatch %q: %w", pt.Title, err)
		}
	}
	return nil
}

func scaffoldHarness(harness Harness, dir string) error {
	switch harness {
	case HarnessPi:
		return scaffoldPi(dir)
	case HarnessClaude:
		return scaffoldClaude(dir)
	default:
		return scaffoldGeneric(dir)
	}
}

func scaffoldClaude(dir string) error {
	agentsDir := filepath.Join(dir, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("create .claude/agents dir: %w", err)
	}

	agentDefs, err := readAgentDefs(HarnessClaude)
	if err != nil {
		return err
	}
	for filename, content := range agentDefs {
		path := filepath.Join(agentsDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("write .claude/agents/%s: %w", filename, err)
		}
	}

	return writeFlatSkills(filepath.Join(dir, ".claude", "skills"))
}

func scaffoldPi(dir string) error {
	agentsDir := filepath.Join(dir, ".pi", "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("create .pi/agents dir: %w", err)
	}

	agentDefs, err := readAgentDefs(HarnessPi)
	if err != nil {
		return err
	}
	for filename, content := range agentDefs {
		path := filepath.Join(agentsDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("write .pi/agents/%s: %w", filename, err)
		}
	}

	return writeFlatSkills(filepath.Join(dir, ".pi", "skills"))
}

func scaffoldGeneric(dir string) error {
	agentsDir := filepath.Join(dir, ".agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("create .agents dir: %w", err)
	}

	agentDefs, err := readAgentDefs(HarnessGeneric)
	if err != nil {
		return err
	}
	for filename, content := range agentDefs {
		path := filepath.Join(agentsDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("write .agents/%s: %w", filename, err)
		}
	}

	return writeFlatSkills(filepath.Join(dir, ".agents", "skills"))
}

func writeFlatSkills(skillsDir string) error {
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return fmt.Errorf("create skills dir: %w", err)
	}

	roleSkillMap, err := loadRoleSkills()
	if err != nil {
		return err
	}

	allSkills := map[string]string{}

	// Read top-level overview skill
	if data, err := skillFiles.ReadFile("embed/skills/kanban.md"); err == nil {
		allSkills["kanban.md"] = string(data)
	}

	for _, skills := range roleSkillMap {
		for name, content := range skills {
			allSkills[name] = content
		}
	}
	for filename, content := range allSkills {
		path := filepath.Join(skillsDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("write skill %s: %w", filename, err)
		}
	}

	// Write role index alongside flat skills
	var sb strings.Builder
	sb.WriteString("# Skill Index\n\n")
	sb.WriteString("system:kanban.md\n")
	for role, names := range SkillNames {
		for _, name := range names {
			fmt.Fprintf(&sb, "%s:%s.md\n", role, name)
		}
	}
	indexPath := filepath.Join(skillsDir, "INDEX")
	if err := os.WriteFile(indexPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("write skill index: %w", err)
	}

	return nil
}