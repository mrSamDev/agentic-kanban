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

	tByID := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		tByID[t.ID] = true
	}

	// Load dependency edges from join table
	depRows, err := db.QueryContext(ctx,
		`SELECT task_id, depends_on_task_id FROM task_dependencies`)
	if err != nil {
		return nil, fmt.Errorf("lint load deps: %w", err)
	}
	defer depRows.Close()

	deps := make(map[string][]string, len(tasks))
	var nodeIDs []string
	var errors, warns []LintIssue

	for _, t := range tasks {
		nodeIDs = append(nodeIDs, t.ID)
		if strings.TrimSpace(t.RoleBoundary) == "" {
			warns = append(warns, LintIssue{TaskID: t.ID, Severity: "warn", Message: "no role_boundary set"})
		}
	}

	for depRows.Next() {
		var from, to string
		if err := depRows.Scan(&from, &to); err != nil {
			depRows.Close()
			return nil, fmt.Errorf("lint scan dep: %w", err)
		}
		if tByID[to] {
			deps[from] = append(deps[from], to)
		}
	}
	depRows.Close()
	if err := depRows.Err(); err != nil {
		return nil, fmt.Errorf("lint dep iterate: %w", err)
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
// nodes must contain every task ID in the graph.
func detectCycles(deps map[string][]string, nodes []string) [][]string {
	const (
		unvisited = 0
		inStack   = 1
		done      = 2
	)
	state := make(map[string]int)
	seen := make(map[string]bool)
	var cycles [][]string

	for _, start := range nodes {
		if state[start] != 0 {
			continue
		}
		type frame struct {
			id  string
			idx int
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
				var cycle []string
				for i := len(stack) - 1; i >= 0; i-- {
					cycle = append([]string{stack[i].id}, cycle...)
					if stack[i].id == child {
						break
					}
				}
				cycle = append(cycle, child)
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