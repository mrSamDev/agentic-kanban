package task

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Service) Dispatch(ctx context.Context, title, roleBoundary, project string, priority int) (Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	if len(title) > maxTitleLength {
		return Task{}, &ExitError{Code: 2, Message: fmt.Sprintf("title too long (max %d)", maxTitleLength)}
	}
	if project == "" {
		project = "default"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, fmt.Errorf("dispatch begin tx: %w", err)
	}
	defer tx.Rollback()

	id, err := nextID(tx)
	if err != nil {
		return Task{}, err
	}

	_, err = tx.Exec(
		`INSERT INTO tasks (id, title, status, role_boundary, project, priority)
		 VALUES (?, ?, 'TODO', ?, ?, ?)`,
		id, title, roleBoundary, project, priority,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert task: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO history (task_id, agent, action) VALUES (?, 'system', 'DISPATCH')`,
		id,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert history: %w", err)
	}

	if err := insertEvent(tx, "task.created", map[string]string{"task_id": id}); err != nil {
		return Task{}, fmt.Errorf("insert event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit dispatch: %w", err)
	}

	runHook(s.hooksDir, "task.created", map[string]string{"task_id": id})
	return s.View(ctx, id)
}

func (s *Service) ClaimNext(ctx context.Context, agent, role, project string) (Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	// Serializable isolation → write lock up front.
	// Required: two concurrent claimers must never get the same task.
	// Retry on SQLITE_BUSY to handle contention gracefully.
	var task Task
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return err
		}
		defer tx.Rollback()

		var row *sql.Row
		if project != "" {
			row = tx.QueryRow(
				`UPDATE tasks
				   SET status         = 'IN_PROGRESS',
				       assigned_agent = ?,
				       lease_until    = datetime('now', '+' || ? || ' minutes'),
				       updated_at     = CURRENT_TIMESTAMP
				 WHERE id = (
				   SELECT id FROM tasks
				    WHERE role_boundary = ?
				      AND project = ?
				      AND (status = 'TODO'
				           OR (status = 'IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP))
				    ORDER BY priority ASC, created_at ASC
				    LIMIT 1
				 )
				 RETURNING id, title, status, role_boundary, project, priority, assigned_agent, lease_until, created_at, updated_at`,
				agent, defaultLeaseMinutes, role, project,
			)
		} else {
			row = tx.QueryRow(
				`UPDATE tasks
				   SET status         = 'IN_PROGRESS',
				       assigned_agent = ?,
				       lease_until    = datetime('now', '+' || ? || ' minutes'),
				       updated_at     = CURRENT_TIMESTAMP
				 WHERE id = (
				   SELECT id FROM tasks
				    WHERE role_boundary = ?
				      AND (status = 'TODO'
				           OR (status = 'IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP))
				    ORDER BY priority ASC, created_at ASC
				    LIMIT 1
				 )
				 RETURNING id, title, status, role_boundary, project, priority, assigned_agent, lease_until, created_at, updated_at`,
				agent, defaultLeaseMinutes, role,
			)
		}

		t, err := scanTask(row)
		if err != nil {
			if err == sql.ErrNoRows {
				// No work available — empty result.
				return nil
			}
			return fmt.Errorf("claim update: %w", err)
		}

		_, err = tx.Exec(
			`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'CLAIM')`,
			t.ID, agent,
		)
		if err != nil {
			return fmt.Errorf("insert claim history for task %s agent %s: %w", t.ID, agent, err)
		}

		if err := insertEvent(tx, "task.claimed", map[string]string{"task_id": t.ID, "agent": agent}); err != nil {
			return fmt.Errorf("insert event: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return err
		}

		task = t
		return nil
	})
	if err != nil {
		return Task{}, fmt.Errorf("claim after retries: %w", err)
	}
	if task.ID != "" {
		runHook(s.hooksDir, "task.claimed", map[string]string{"task_id": task.ID, "agent": agent})
	}
	return task, nil
}

// Uses a single UPDATE with WHERE guards so the check+update is atomic —
// the write lock is acquired at first write, and SQLite serializes at commit time.
func (s *Service) Complete(ctx context.Context, id, agent string, toReview bool) (Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	var task Task
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		newStatus := StatusDone
		action := "COMPLETE"
		if toReview {
			newStatus = StatusInReview
			action = "REVIEW"
		}

		res, err := tx.Exec(
			`UPDATE tasks
			    SET status = ?, assigned_agent = NULL, lease_until = NULL,
			        updated_at = CURRENT_TIMESTAMP
			  WHERE id = ?
			    AND assigned_agent = ?
			    AND status IN ('IN_PROGRESS', 'IN_REVIEW')`,
			string(newStatus), id, agent,
		)
		if err != nil {
			return fmt.Errorf("complete update: %w", err)
		}

		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("complete rows affected: %w", err)
		}
		if n == 0 {
			// Determine why: does the task exist?
			var exists bool
			tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
			if !exists {
				return ErrNotFound
			}
			// Existing task — must be wrong agent or wrong state.
			// Fetch actual assigned_agent for helpful error.
			var actualAgent sql.NullString
			tx.QueryRow(`SELECT assigned_agent FROM tasks WHERE id = ?`, id).Scan(&actualAgent)
			if actualAgent.Valid {
				return &ExitError{Code: 2, Message: fmt.Sprintf("task not assigned to this agent (assigned to: %s)", actualAgent.String)}
			}
			return ErrNotAssigned
		}

		_, err = tx.Exec(
			`INSERT INTO history (task_id, agent, action) VALUES (?, ?, ?)`,
			id, agent, action,
		)
		if err != nil {
			return fmt.Errorf("insert complete history for task %s agent %s: %w", id, agent, err)
		}

		if !toReview {
			if err := insertEvent(tx, "task.completed", map[string]string{"task_id": id, "agent": agent}); err != nil {
				return fmt.Errorf("insert event: %w", err)
			}
		}

		if err := tx.Commit(); err != nil {
			return err
		}

		task, err = s.View(ctx, id)
		return err
	})
	if err == nil && !toReview {
		runHook(s.hooksDir, "task.completed", map[string]string{"task_id": id, "agent": agent})
	}
	return task, err
}

func (s *Service) LogProgress(ctx context.Context, id, agent, content string, noteType string) (Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	if len(content) > maxNoteLength {
		return Task{}, &ExitError{Code: 2, Message: fmt.Sprintf("note too long (max %d)", maxNoteLength)}
	}

	var task Task
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		// Verify ownership via UPDATE condition — atomic with the lease renewal.
		res, err := tx.Exec(
			`UPDATE tasks
			    SET lease_until = datetime('now', '+' || ? || ' minutes'),
			        updated_at = CURRENT_TIMESTAMP
			  WHERE id = ? AND assigned_agent = ?`,
			defaultLeaseMinutes, id, agent,
		)
		if err != nil {
			return fmt.Errorf("renew lease: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("log rows affected: %w", err)
		}
		if n == 0 {
			var exists bool
			tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
			if !exists {
				return ErrNotFound
			}
			return ErrNotAssigned
		}

		var nt *string
		if noteType != "" {
			nt = &noteType
		}
		_, err = tx.Exec(
			`INSERT INTO notes (task_id, author, note_type, content) VALUES (?, ?, ?, ?)`,
			id, agent, nt, content,
		)
		if err != nil {
			return fmt.Errorf("insert note: %w", err)
		}

		_, err = tx.Exec(
			`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'PROGRESS')`,
			id, agent,
		)
		if err != nil {
			return fmt.Errorf("insert progress history for task %s agent %s: %w", id, agent, err)
		}

		if err := tx.Commit(); err != nil {
			return err
		}

		task, err = s.View(ctx, id)
		return err
	})
	return task, err
}

func (s *Service) Block(ctx context.Context, id, agent, reason string) (Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	if len(reason) > maxReasonLength {
		return Task{}, &ExitError{Code: 2, Message: fmt.Sprintf("reason too long (max %d)", maxReasonLength)}
	}

	var task Task
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		res, err := tx.Exec(
			`UPDATE tasks
			    SET status = 'BLOCKED', assigned_agent = NULL, lease_until = NULL,
			        updated_at = CURRENT_TIMESTAMP
			  WHERE id = ?
			    AND assigned_agent = ?
			    AND status IN ('IN_PROGRESS', 'IN_REVIEW')`,
			id, agent,
		)
		if err != nil {
			return fmt.Errorf("block update: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("block rows affected: %w", err)
		}
		if n == 0 {
			var exists bool
			tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
			if !exists {
				return ErrNotFound
			}
			return ErrNotAssigned
		}

		_, err = tx.Exec(
			`INSERT INTO notes (task_id, author, note_type, content) VALUES (?, ?, 'BLOCKED', ?)`,
			id, agent, reason,
		)
		if err != nil {
			return fmt.Errorf("insert block note: %w", err)
		}

		_, err = tx.Exec(
			`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'BLOCK')`,
			id, agent,
		)
		if err != nil {
			return fmt.Errorf("insert block history for task %s agent %s: %w", id, agent, err)
		}

		if err := insertEvent(tx, "task.blocked", map[string]string{"task_id": id, "agent": agent}); err != nil {
			return fmt.Errorf("insert event: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return err
		}

		task, err = s.View(ctx, id)
		return err
	})
	if err == nil {
		runHook(s.hooksDir, "task.blocked", map[string]string{"task_id": id, "agent": agent})
	}
	return task, err
}

// Any reviewer can approve — no lease ownership check needed.
func (s *Service) ReviewApprove(ctx context.Context, id, agent string) (Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, fmt.Errorf("review begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE tasks
		    SET status = 'DONE', assigned_agent = NULL, lease_until = NULL,
		        updated_at = CURRENT_TIMESTAMP
		  WHERE id = ? AND status = 'IN_REVIEW'`,
		id,
	)
	if err != nil {
		return Task{}, fmt.Errorf("review update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Task{}, fmt.Errorf("review rows affected: %w", err)
	}
	if n == 0 {
		var exists bool
		tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
		if !exists {
			return Task{}, ErrNotFound
		}
		return Task{}, ErrInvalidState
	}

	_, err = tx.Exec(
		`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'REVIEW')`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert review history for task %s agent %s: %w", id, agent, err)
	}

	if err := insertEvent(tx, "review.approved", map[string]string{"task_id": id, "agent": agent}); err != nil {
		return Task{}, fmt.Errorf("insert event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit review: %w", err)
	}

	runHook(s.hooksDir, "review.approved", map[string]string{"task_id": id, "agent": agent})
	return s.View(ctx, id)
}

// Any reviewer can reject — no lease ownership check needed.
func (s *Service) ReviewReject(ctx context.Context, id, agent, reason string) (Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	if len(reason) > maxReasonLength {
		return Task{}, &ExitError{Code: 2, Message: fmt.Sprintf("reason too long (max %d)", maxReasonLength)}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, fmt.Errorf("reject begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE tasks
		    SET status = 'TODO', assigned_agent = NULL, lease_until = NULL,
		        updated_at = CURRENT_TIMESTAMP
		  WHERE id = ? AND status = 'IN_REVIEW'`,
		id,
	)
	if err != nil {
		return Task{}, fmt.Errorf("reject update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Task{}, fmt.Errorf("reject rows affected: %w", err)
	}
	if n == 0 {
		var exists bool
		tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
		if !exists {
			return Task{}, ErrNotFound
		}
		return Task{}, ErrInvalidState
	}

	_, err = tx.Exec(
		`INSERT INTO notes (task_id, author, note_type, content) VALUES (?, ?, 'REJECTED', ?)`,
		id, agent, reason,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert reject note: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'REVIEW')`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert reject history for task %s agent %s: %w", id, agent, err)
	}

	if err := insertEvent(tx, "review.rejected", map[string]string{"task_id": id, "agent": agent}); err != nil {
		return Task{}, fmt.Errorf("insert event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit reject: %w", err)
	}

	runHook(s.hooksDir, "review.rejected", map[string]string{"task_id": id, "agent": agent})
	return s.View(ctx, id)
}

func (s *Service) BatchUpdatePriority(ids []string, priority int) (int, error) {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("batch priority begin tx: %w", err)
	}
	defer tx.Rollback()

	updated := 0
	for _, id := range ids {
		res, err := tx.Exec(
			`UPDATE tasks SET priority = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			priority, id,
		)
		if err != nil {
			return 0, fmt.Errorf("update task %s: %w", id, err)
		}
		n, _ := res.RowsAffected()
		updated += int(n)
	}

	return updated, tx.Commit()
}

func (s *Service) BatchUpdateProject(ids []string, project string) (int, error) {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("batch project begin tx: %w", err)
	}
	defer tx.Rollback()

	updated := 0
	for _, id := range ids {
		res, err := tx.Exec(
			`UPDATE tasks SET project = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			project, id,
		)
		if err != nil {
			return 0, fmt.Errorf("update task %s: %w", id, err)
		}
		n, _ := res.RowsAffected()
		updated += int(n)
	}

	return updated, tx.Commit()
}
