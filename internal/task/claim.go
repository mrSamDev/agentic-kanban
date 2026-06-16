package task

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Uses Serializable isolation so two concurrent claimers never get the same task.
// Also reclaims tasks where the previous agent's lease expired.
func (s *Service) ClaimNext(ctx context.Context, agent, role, project string, respectDeps ...bool) (Task, error) {
	tasks, err := s.ClaimBatch(ctx, agent, role, project, 1, respectDeps...)
	if err != nil || len(tasks) == 0 {
		return Task{}, err
	}
	return tasks[0], nil
}

// ClaimBatch claims up to count eligible tasks in one Serializable transaction.
// This is the canonical claim implementation; ClaimNext delegates here.
// If respectDeps is true (default), tasks with unmet dependencies are skipped.
// Concurrency note: the current code uses one atomic SQL UPDATE (SELECT+UPDATE in one
// statement), which makes double-claim impossible. This refactor switches to a
// Serializable transaction with explicit SELECT-then-UPDATE — same correctness
// guarantee, but the concurrency contract is now explicit (Serializable isolation,
// not implicit atomicity). The SELECT-then-UPDATE approach is required to support
// dependency filtering (hasUnmetDeps) and batch claims.
func (s *Service) ClaimBatch(ctx context.Context, agent, role, project string, count int, respectDeps ...bool) ([]Task, error) {
	// Default: respect deps (backward compatible)
	filterDeps := true
	if len(respectDeps) > 0 {
		filterDeps = respectDeps[0]
	}
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
		defer func() { _ = tx.Rollback() }()

		// Step 1: select eligible candidates (cap at maxCandidateFetch to avoid OOM)
		maxFetch := count * 5
		if maxFetch > maxCandidateFetch {
			maxFetch = maxCandidateFetch
		}

		var rows *sql.Rows
		if project != "" {
			rows, err = tx.Query(`
				SELECT id, title, status, role_boundary, project, priority,
				       assigned_agent, lease_until, created_at, updated_at, depends_on, claimed_by
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
				       assigned_agent, lease_until, created_at, updated_at, depends_on, claimed_by
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

		// Filter out tasks with unmet deps (unless --respect-deps=false), collect up to count claimable
		var claimable []Task
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan candidate: %w", err)
			}
			if filterDeps {
				if depsBlocked, err := hasUnmetDeps(tx, t.ID); err != nil {
					_ = rows.Close()
					return fmt.Errorf("check deps for %s: %w", t.ID, err)
				} else if depsBlocked {
					continue
				}
			}
			claimable = append(claimable, t)
			if len(claimable) >= count {
				break
			}
		}
		_ = rows.Close()
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
        claimed_by = CASE WHEN claimed_by IS NULL THEN ? ELSE claimed_by END,
			        updated_at = CURRENT_TIMESTAMP
			  WHERE id = ? AND status IN ('TODO', 'IN_PROGRESS')`,
				agent, defaultLeaseMinutes, agent, t.ID,
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
				        assigned_agent, lease_until, created_at, updated_at, depends_on, claimed_by
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
		runHook(s.HookRunner, s.hooksDir, "task.claimed", EventPayload{
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

// ClaimByID claims a specific task by ID. Returns ErrNotFound if not found,
// ErrNotAssigned if already claimed, ErrInvalidState if not TODO,
// or ErrDependencyBlocked if dependencies are unmet.
func (s *Service) ClaimByID(ctx context.Context, id, agent string) (Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	var task Task
	var payload EventPayload
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return fmt.Errorf("claim-by-id begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		// Read current state
		var currentStatus string
		var assignedAgent sql.NullString
		err = tx.QueryRow(
			`SELECT status, assigned_agent FROM tasks WHERE id = ?`, id,
		).Scan(&currentStatus, &assignedAgent)
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("claim-by-id read task %s: %w", id, err)
		}

		// Must be TODO (or expired IN_PROGRESS that nobody reclaimed via claim-next)
		switch TaskStatus(currentStatus) {
		case StatusTODO:
			// fine
		case StatusInProgress:
			// Check lease expiry for reclamation
			if assignedAgent.Valid && assignedAgent.String == agent {
				return &ExitError{Code: 2, Message: fmt.Sprintf("already claimed by %s", agent)}
			}
			var leaseUntil sql.NullString
			tx.QueryRow(`SELECT lease_until FROM tasks WHERE id = ?`, id).Scan(&leaseUntil)
			if leaseUntil.Valid {
				parsed, err := parseLeaseTime(leaseUntil.String)
				if err == nil && parsed.After(time.Now()) {
					return ErrNotAssigned
				}
				// Lease expired — allow reclaim
			}
		default:
			return ErrInvalidState
		}

		// Check unmet dependencies
		if blocked, err := hasUnmetDeps(tx, id); err != nil {
			return fmt.Errorf("claim-by-id check deps %s: %w", id, err)
		} else if blocked {
			return &ExitError{Code: 2, Message: fmt.Sprintf("task %s has unmet dependencies", id)}
		}

		// Claim it
		res, err := tx.Exec(
			`UPDATE tasks
			    SET status = 'IN_PROGRESS', assigned_agent = ?,
			        lease_until = datetime('now', '+' || ? || ' minutes'),
        claimed_by = CASE WHEN claimed_by IS NULL THEN ? ELSE claimed_by END,
			        updated_at = CURRENT_TIMESTAMP
			  WHERE id = ?`,
			agent, defaultLeaseMinutes, agent, id,
		)
		if err != nil {
			return fmt.Errorf("claim-by-id update %s: %w", id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("claim-by-id rows affected %s: %w", id, err)
		}
		if n == 0 {
			return ErrNotFound
		}

		// Re-read for authoritative state
		task, err = reRead(tx, id)
		if err != nil {
			return fmt.Errorf("claim-by-id re-read %s: %w", id, err)
		}

		// History
		if _, err := tx.Exec(
			`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'CLAIM')`,
			id, agent,
		); err != nil {
			return fmt.Errorf("claim-by-id history %s: %w", id, err)
		}

		// Event
		payload = EventPayload{
			TaskID:       id,
			Agent:        agent,
			Title:        task.Title,
			Project:      task.Project,
			Priority:     fmt.Sprintf("%d", task.Priority),
			RoleBoundary: task.RoleBoundary,
		}
		if err := insertEvent(tx, "task.claimed", payload); err != nil {
			return fmt.Errorf("claim-by-id event %s: %w", id, err)
		}

		return tx.Commit()
	})
	if err != nil {
		return Task{}, err
	}
	runHook(s.HookRunner, s.hooksDir, "task.claimed", payload)
	return task, nil
}

// TransferClaim transfers a claimed task from one agent to another.
// The caller (fromAgent) must be the current assigned_agent.
// toAgent becomes the new owner with a fresh lease.
func (s *Service) TransferClaim(ctx context.Context, id, fromAgent, toAgent string) (Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	var task Task
	var payload EventPayload
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return fmt.Errorf("transfer begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		// Read current state
		var currentStatus string
		var assignedAgent sql.NullString
		err = tx.QueryRow(
			`SELECT status, assigned_agent FROM tasks WHERE id = ?`, id,
		).Scan(&currentStatus, &assignedAgent)
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("transfer read task %s: %w", id, err)
		}

		if TaskStatus(currentStatus) != StatusInProgress {
			return &ExitError{Code: 2, Message: fmt.Sprintf("task %s is not IN_PROGRESS", id)}
		}

		if !assignedAgent.Valid || assignedAgent.String != fromAgent {
			var actual string
			if assignedAgent.Valid {
				actual = assignedAgent.String
			} else {
				actual = "unclaimed"
			}
			return &ExitError{Code: 2, Message: fmt.Sprintf("task not assigned to %s (assigned to: %s)", fromAgent, actual)}
		}

		if fromAgent == toAgent {
			return &ExitError{Code: 2, Message: "cannot transfer task to yourself"}
		}

		// Transfer: reassign agent, reset lease
		res, err := tx.Exec(
			`UPDATE tasks
			    SET assigned_agent = ?,
			        lease_until = datetime('now', '+' || ? || ' minutes'),
			        updated_at = CURRENT_TIMESTAMP
			  WHERE id = ? AND assigned_agent = ?`,
			toAgent, defaultLeaseMinutes, id, fromAgent,
		)
		if err != nil {
			return fmt.Errorf("transfer update %s: %w", id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("transfer rows affected %s: %w", id, err)
		}
		if n == 0 {
			return ErrNotFound
		}

		// Re-read for authoritative state
		task, err = reRead(tx, id)
		if err != nil {
			return fmt.Errorf("transfer re-read %s: %w", id, err)
		}

		// History
		if _, err := tx.Exec(
			`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'TRANSFER')`,
			id, fromAgent+"→"+toAgent,
		); err != nil {
			return fmt.Errorf("transfer history %s: %w", id, err)
		}

		// Event
		payload = EventPayload{
			TaskID:    id,
			Agent:     toAgent,
			FromAgent: fromAgent,
			Title:     task.Title,
			Project:   task.Project,
			Priority:  fmt.Sprintf("%d", task.Priority),
		}
		if err := insertEvent(tx, "task.transferred", payload); err != nil {
			return fmt.Errorf("transfer event %s: %w", id, err)
		}

		return tx.Commit()
	})
	if err != nil {
		return Task{}, err
	}
	runHook(s.HookRunner, s.hooksDir, "task.transferred", payload)
	return task, nil
}

// Propagates error so the caller can fail safely rather than silently claiming a dep-blocked task.
func hasUnmetDeps(tx *sql.Tx, taskID string) (bool, error) {
	var count int
	err := tx.QueryRow(
		`SELECT COUNT(*) FROM task_dependencies d
		  JOIN tasks t ON t.id = d.depends_on_task_id
		 WHERE d.task_id = ? AND t.status != 'DONE'`,
		taskID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check deps for %s: %w", taskID, err)
	}
	return count > 0, nil
}
