package task

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Uses Serializable isolation so two concurrent claimers never get the same task.
// Also reclaims tasks where the previous agent's lease expired.
func (s *Service) ClaimNext(ctx context.Context, agent, role, project string) (Task, error) {
	tasks, err := s.ClaimBatch(ctx, agent, role, project, 1)
	if err != nil || len(tasks) == 0 {
		return Task{}, err
	}
	return tasks[0], nil
}

// ClaimBatch claims up to count eligible tasks in one Serializable transaction.
// This is the canonical claim implementation; ClaimNext delegates here.
// Concurrency note: the current code uses one atomic SQL UPDATE (SELECT+UPDATE in one
// statement), which makes double-claim impossible. This refactor switches to a
// Serializable transaction with explicit SELECT-then-UPDATE — same correctness
// guarantee, but the concurrency contract is now explicit (Serializable isolation,
// not implicit atomicity). The SELECT-then-UPDATE approach is required to support
// dependency filtering (hasUnmetDeps) and batch claims.
func (s *Service) ClaimBatch(ctx context.Context, agent, role, project string, count int) ([]Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	if count < 1 {
		count = 1
	}

	var claimed []Task
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return fmt.Errorf("batch claim begin tx: %w", err)
		}
		defer tx.Rollback()

		// Step 1: select eligible candidates (cap at maxCandidateFetch to avoid OOM)
		maxFetch := count * 5
		if maxFetch > maxCandidateFetch {
			maxFetch = maxCandidateFetch
		}

		var rows *sql.Rows
		if project != "" {
			rows, err = tx.Query(`
				SELECT id, title, status, role_boundary, project, priority,
				       assigned_agent, lease_until, created_at, updated_at, depends_on
				  FROM tasks
				 WHERE role_boundary = ?
				   AND project = ?
				   AND (status = 'TODO'
				        OR (status = 'IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP))
				 ORDER BY priority ASC, created_at ASC
				 LIMIT ?`, role, project, maxFetch,
			)
		} else {
			rows, err = tx.Query(`
				SELECT id, title, status, role_boundary, project, priority,
				       assigned_agent, lease_until, created_at, updated_at, depends_on
				  FROM tasks
				 WHERE role_boundary = ?
				   AND (status = 'TODO'
				        OR (status = 'IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP))
				 ORDER BY priority ASC, created_at ASC
				 LIMIT ?`, role, maxFetch,
			)
		}
		if err != nil {
			return fmt.Errorf("batch claim candidates: %w", err)
		}

		// Filter out tasks with unmet deps, collect up to count claimable
		var claimable []Task
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				rows.Close()
				return fmt.Errorf("scan candidate: %w", err)
			}
			if depsBlocked, err := hasUnmetDeps(tx, t); err != nil {
				rows.Close()
				return fmt.Errorf("check deps for %s: %w", t.ID, err)
			} else if depsBlocked {
				continue
			}
			claimable = append(claimable, t)
			if len(claimable) >= count {
				break
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate candidates: %w", err)
		}

		// Step 2: claim up to count tasks in same transaction
		claimed = make([]Task, 0, len(claimable))
		for _, t := range claimable {
			if len(claimed) >= count {
				break
			}
			res, err := tx.Exec(
				`UPDATE tasks
				    SET status = 'IN_PROGRESS', assigned_agent = ?,
				        lease_until = datetime('now', '+' || ? || ' minutes'),
				        updated_at = CURRENT_TIMESTAMP
				  WHERE id = ? AND status IN ('TODO', 'IN_PROGRESS')`,
				agent, defaultLeaseMinutes, t.ID,
			)
			if err != nil {
				return fmt.Errorf("batch claim update for %s: %w", t.ID, err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return fmt.Errorf("batch claim rows affected for %s: %w", t.ID, err)
			}
			if n == 0 {
				continue // race lost, skip
			}

			// Re-read from DB to get authoritative status, updated_at, lease_until
			row := tx.QueryRow(
				`SELECT id, title, status, role_boundary, project, priority,
				        assigned_agent, lease_until, created_at, updated_at, depends_on
				   FROM tasks WHERE id = ?`, t.ID,
			)
			claimedEntry, err := scanTask(row)
			if err != nil {
				return fmt.Errorf("re-read claimed task %s: %w", t.ID, err)
			}

			if _, err := tx.Exec(
				`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'CLAIM')`,
				t.ID, agent,
			); err != nil {
				return fmt.Errorf("insert claim history for %s: %w", t.ID, err)
			}

			if err := insertEvent(tx, "task.claimed", EventPayload{
				TaskID:       t.ID,
				Agent:        agent,
				Title:        t.Title,
				Project:      t.Project,
				Priority:     fmt.Sprintf("%d", t.Priority),
				RoleBoundary: t.RoleBoundary,
			}); err != nil {
				return fmt.Errorf("insert event for %s: %w", t.ID, err)
			}

			claimed = append(claimed, claimedEntry)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("batch claim commit: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("batch claim after retries: %w", err)
	}

	// Fire hooks outside tx
	for _, t := range claimed {
		runHook(s.hooksDir, "task.claimed", EventPayload{
			TaskID:       t.ID,
			Agent:        agent,
			Title:        t.Title,
			Project:      t.Project,
			Priority:     fmt.Sprintf("%d", t.Priority),
			RoleBoundary: t.RoleBoundary,
		})
	}
	return claimed, nil
}

// hasUnmetDeps checks whether any of t's dependencies are still not DONE.
// Propagates error so the caller can fail safely rather than silently claiming a dep-blocked task.
func hasUnmetDeps(tx *sql.Tx, t Task) (bool, error) {
	if t.DependsOn == nil || *t.DependsOn == "" {
		return false, nil
	}
	parts := strings.Split(*t.DependsOn, ",")
	var ids []string
	for _, p := range parts {
		id := strings.TrimSpace(p)
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return false, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	var count int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM tasks WHERE id IN (`+strings.Join(placeholders, ",")+`) AND status != 'DONE'`,
		args...,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("check deps for %s: %w", t.ID, err)
	}
	return count > 0, nil
}
