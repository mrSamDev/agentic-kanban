package bootstrap

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const maxPlanSize = 1 << 20 // 1MB

type PlanTask struct {
	Title    string
	Role     string // defaults to "worker"
	Priority int    // defaults to 100
}

// ParsePlan extracts tasks from a plan file.
// Markdown: ## headings become task titles, - list items become notes.
// JSON: array of {title, role, priority}.
//
// Files larger than 1MB are rejected to prevent OOM.
func ParsePlan(path string) ([]PlanTask, []string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("stat plan: %w", err)
	}
	if info.Size() > maxPlanSize {
		return nil, nil, fmt.Errorf("plan file too large: %d bytes (max %d)", info.Size(), maxPlanSize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read plan: %w", err)
	}

	content := string(data)

	if trimmed := strings.TrimSpace(content); strings.HasPrefix(trimmed, "[") {
		return parseJSONPlan(trimmed)
	}

	return parseMarkdownPlan(content)
}

func parseMarkdownPlan(content string) ([]PlanTask, []string, error) {
	var tasks []PlanTask
	var notes []string
	var currentTitle string
	var currentNotes []string
	priority := 100

	flushTask := func() {
		if currentTitle == "" {
			return
		}
		tasks = append(tasks, PlanTask{
			Title:    currentTitle,
			Role:     "worker",
			Priority: priority,
		})
		if len(currentNotes) > 0 {
			notes = append(notes, currentNotes...)
		}
		currentNotes = nil
	}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "##") {
			flushTask()

			title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if title == "" {
				currentTitle = ""
				priority = 100
				continue
			}
			priority = extractPriority(title, &title)
			currentTitle = title
		} else if currentTitle != "" && strings.HasPrefix(trimmed, "- ") {
			note := strings.TrimPrefix(trimmed, "- ")
			currentNotes = append(currentNotes, note)
		}
	}

	flushTask()

	if len(tasks) == 0 {
		return nil, nil, fmt.Errorf("no tasks found in plan (use ## headings for task titles)")
	}

	return tasks, notes, nil
}

func extractPriority(title string, cleaned *string) int {
	// [p1]..[p999] pattern.
	idx := strings.Index(title, "[p")
	if idx >= 0 {
		end := strings.Index(title[idx:], "]")
		if end > 0 {
			var p int
			if _, err := fmt.Sscanf(title[idx+2:idx+end], "%d", &p); err == nil && p > 0 && p <= 999 {
				*cleaned = strings.TrimSpace(title[:idx] + title[idx+end+1:])
				return p
			}
		}
	}

	*cleaned = title
	return 100
}

func parseJSONPlan(content string) ([]PlanTask, []string, error) {
	var raw []struct {
		Title    string `json:"title"`
		Role     string `json:"role"`
		Priority int    `json:"priority"`
	}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, nil, fmt.Errorf("parse JSON: %w", err)
	}

	var tasks []PlanTask
	for _, r := range raw {
		if r.Title == "" {
			continue
		}
		role := r.Role
		if role == "" {
			role = "worker"
		}
		priority := r.Priority
		if priority == 0 {
			priority = 100
		}
		tasks = append(tasks, PlanTask{Title: r.Title, Role: role, Priority: priority})
	}
	return tasks, nil, nil
}