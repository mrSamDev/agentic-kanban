package bootstrap

import (
	"embed"
	"fmt"
	"path/filepath"
)

//go:embed embed/agents/pi/*.md
//go:embed embed/agents/claude/*.md
//go:embed embed/agents/generic/*.md
var agentFiles embed.FS

//go:embed embed/skills/manager/*.md
//go:embed embed/skills/worker/*.md
//go:embed embed/skills/reviewer/*.md
var skillFiles embed.FS

func readAgentFile(harness Harness, role string) (string, error) {
	dir := filepath.Join("embed", "agents", string(harness))
	data, err := agentFiles.ReadFile(filepath.Join(dir, role+".md"))
	if err != nil {
		return "", fmt.Errorf("read agent %s/%s: %w", harness, role, err)
	}
	return string(data), nil
}

func readAgentDefs(harness Harness) (map[string]string, error) {
	roles := []string{"manager", "worker", "reviewer"}
	defs := make(map[string]string, len(roles))
	for _, role := range roles {
		content, err := readAgentFile(harness, role)
		if err != nil {
			return nil, err
		}
		defs[role+".md"] = content
	}
	return defs, nil
}

func readSkillFile(filename string) (string, error) {
	for _, dir := range []string{"manager", "worker", "reviewer"} {
		data, err := skillFiles.ReadFile(filepath.Join("embed", "skills", dir, filename))
		if err == nil {
			return string(data), nil
		}
	}
	return "", fmt.Errorf("skill %s not found", filename)
}

func loadRoleSkills() (map[string]map[string]string, error) {
	roles := map[string][]string{
		"manager":  {"dispatch-task.md", "dispatch-plan.md", "approve-plan.md", "review-backlog.md", "view-task.md"},
		"worker":   {"claim-next-task.md", "log-progress.md", "block-task.md", "complete-task.md"},
		"reviewer": {"claim-review.md", "approve-task.md", "reject-task.md"},
	}
	result := make(map[string]map[string]string, len(roles))
	for role, filenames := range roles {
		skills := make(map[string]string, len(filenames))
		for _, f := range filenames {
			content, err := readSkillFile(f)
			if err != nil {
				return nil, err
			}
			skills[f] = content
		}
		result[role] = skills
	}
	return result, nil
}