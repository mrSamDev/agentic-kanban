package task

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type LintIssue struct {
	TaskID   string `json:"task_id"`
	Severity string `json:"severity"` // "error" or "warn"
	Message  string `json:"message"`
}

// LintPlan checks all tasks in the project for structural problems.
// Returns errors first, then warnings, each sorted by task ID within severity.
func LintPlan(ctx context.Context, db *sql.DB, project string) ([]LintIssue, error) {
	query := `SELECT id, title, status, role_boundary, project, priority,
	                 assigned_agent, lease_until, created_at, updated_at, depends_on, claimed_by
	            FROM tasks`
	var args []any
	if project != "" {
		query += ` WHERE project = ?`
		args = append(args, project)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("lint load tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("lint scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lint iterate: %w", err)
	}

	taskByID := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		taskByID[t.ID] = true
	}

	// adjacency list for cycle detection (only known tasks)
	deps := make(map[string][]string, len(tasks))
	var nodeIDs []string

	var errors, warns []LintIssue

	for _, t := range tasks {
		nodeIDs = append(nodeIDs, t.ID)
		// Missing role_boundary (schema enforces NOT NULL, so this catches empty string "")
		if strings.TrimSpace(t.RoleBoundary) == "" {
			warns = append(warns, LintIssue{TaskID: t.ID, Severity: "warn", Message: "no role_boundary set"})
		}

		if t.DependsOn == nil || *t.DependsOn == "" {
			continue
		}
		for _, raw := range strings.Split(*t.DependsOn, ",") {
			depID := strings.TrimSpace(raw)
			if depID == "" {
				continue
			}
			if !taskByID[depID] {
				warns = append(warns, LintIssue{
					TaskID:   t.ID,
					Severity: "warn",
					Message:  fmt.Sprintf("depends on unknown task %s", depID),
				})
			} else {
				deps[t.ID] = append(deps[t.ID], depID)
			}
		}
	}

	for _, cycle := range detectCycles(deps, nodeIDs) {
		errors = append(errors, LintIssue{
			TaskID:   cycle[0],
			Severity: "error",
			Message:  fmt.Sprintf("cycle detected: %s", strings.Join(cycle, " → ")),
		})
	}

	return append(errors, warns...), nil
}

// detectCycles finds all cycles in the dependency graph using iterative DFS.
// Each returned slice is the cycle path with the start node repeated at the end.
// detectCycles finds all cycles in the dependency graph using iterative DFS.
// Each returned slice is the cycle path with the start node repeated at the end.
// nodes must contain every task ID in the graph; detectCycles iterates over it
// rather than map keys so nodes that are only targets (no outgoing edges) are
// still visited as starting points.
func detectCycles(deps map[string][]string, nodes []string) [][]string {
	const (
		unvisited = 0
		inStack   = 1
		done      = 2
	)
	state := make(map[string]int)
	seen := make(map[string]bool) // deduplicate reported cycles
	var cycles [][]string

	for _, start := range nodes {
		if state[start] != 0 {
			continue
		}
		type frame struct {
			id  string
			idx int // next child index to process
		}
		stack := []frame{{start, 0}}
		state[start] = inStack

		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			children := deps[top.id]

			if top.idx >= len(children) {
				state[top.id] = done
				stack = stack[:len(stack)-1]
				continue
			}

			child := children[top.idx]
			top.idx++

			switch state[child] {
			case unvisited:
				state[child] = inStack
				stack = append(stack, frame{child, 0})
			case inStack:
				// Back edge: extract the cycle from the stack
				var cycle []string
				for i := len(stack) - 1; i >= 0; i-- {
					cycle = append([]string{stack[i].id}, cycle...)
					if stack[i].id == child {
						break
					}
				}
				cycle = append(cycle, child) // close the loop
				key := strings.Join(cycle, ",")
				if !seen[key] {
					seen[key] = true
					cycles = append(cycles, cycle)
				}
			}
		}
	}
	return cycles
}
