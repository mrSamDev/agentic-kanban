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

type SkillInfo struct {
	Name string
	Role string
	File string
}

var SkillNames = map[string][]string{
	"manager":  {"dispatch-task", "dispatch-plan", "approve-plan", "review-backlog", "setup-hooks", "view-task"},
	"worker":   {"claim-next-task", "claim-task", "log-progress", "block-task", "complete-task"},
	"reviewer": {"claim-review", "approve-task", "reject-task"},
}

func ListSkills() []SkillInfo {
	var out []SkillInfo
	for _, role := range []string{"manager", "worker", "reviewer"} {
		for _, name := range SkillNames[role] {
			out = append(out, SkillInfo{Name: name, Role: role, File: name + ".md"})
		}
	}
	return out
}

func ReadSkill(name string) (string, string, error) {
	for _, dir := range []string{"manager", "worker", "reviewer"} {
		data, err := skillFiles.ReadFile(filepath.Join("embed", "skills", dir, name+".md"))
		if err == nil {
			return string(data), dir, nil
		}
	}
	return "", "", fmt.Errorf("skill %q not found", name)
}

func loadRoleSkills() (map[string]map[string]string, error) {
	result := make(map[string]map[string]string, len(SkillNames))
	for role, names := range SkillNames {
		skills := make(map[string]string, len(names))
		for _, name := range names {
			data, err := skillFiles.ReadFile(filepath.Join("embed", "skills", role, name+".md"))
			if err != nil {
				return nil, err
			}
			skills[name+".md"] = string(data)
		}
		result[role] = skills
	}
	return result, nil
}