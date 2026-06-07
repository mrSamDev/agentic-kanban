package task

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Service) Dispatch(ctx context.Context, title, roleBoundary, project string, priority int, dependsOn *string) (Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	if len(title) > maxTitleLength {
		return Task{}, &ExitError{Code: 2, Message: fmt.Sprintf("title too long (max %d)", maxTitleLength)}
	}
	if priority < 0 || priority > 999 {
		return Task{}, &ExitError{Code: 2, Message: "priority must be between 0 and 999"}
	}
	if roleBoundary == "" {
		return Task{}, &ExitError{Code: 2, Message: "role cannot be empty"}
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
		`INSERT INTO tasks (id, title, status, role_boundary, project, priority, depends_on)
		 VALUES (?, ?, 'TODO', ?, ?, ?, ?)`,
		id, title, roleBoundary, project, priority, dependsOn,
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

	if err := insertEvent(tx, "task.created", eventPayload(tx, id, EventPayload{})); err != nil {
		return Task{}, fmt.Errorf("insert event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit dispatch: %w", err)
	}

	runHook(s.hooksDir, "task.created", EventPayload{
		TaskID:       id,
		Title:        title,
		Project:      project,
		Priority:     fmt.Sprintf("%d", priority),
		RoleBoundary: roleBoundary,
	})
	return s.View(ctx, id)
}

func (s *Service) Complete(ctx context.Context, id, agent string, toReview bool) (Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	var task Task
	var payload EventPayload
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
			var exists bool
			tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
			if !exists {
				return ErrNotFound
			}
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

		payload = eventPayload(tx, id, EventPayload{Agent: agent})
		if !toReview {
			if err := insertEvent(tx, "task.completed", payload); err != nil {
				return fmt.Errorf("insert event: %w", err)
			}
		} else {
			if err := insertEvent(tx, "task.submitted_for_review", payload); err != nil {
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
		runHook(s.hooksDir, "task.completed", payload)
	}
	if err == nil && toReview {
		runHook(s.hooksDir, "task.submitted_for_review", payload)
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
	var payload EventPayload
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

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

		extra := EventPayload{Agent: agent, NoteType: noteType}
		payload = eventPayload(tx, id, extra)
		if err := insertEvent(tx, "task.progress", payload); err != nil {
			return fmt.Errorf("insert event: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return err
		}

		task, err = s.View(ctx, id)
		return err
	})
	if err == nil {
		runHook(s.hooksDir, "task.progress", payload)
	}
	return task, err
}

func (s *Service) ExtendLease(ctx context.Context, id, agent string, minutes int) (Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	if minutes <= 0 {
		minutes = defaultLeaseMinutes
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
			    SET lease_until = datetime('now', '+' || ? || ' minutes'),
			        updated_at = CURRENT_TIMESTAMP
			  WHERE id = ? AND assigned_agent = ?`,
			minutes, id, agent,
		)
		if err != nil {
			return fmt.Errorf("extend lease: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("extend lease rows affected: %w", err)
		}
		if n == 0 {
			var exists bool
			tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
			if !exists {
				return ErrNotFound
			}
			return ErrNotAssigned
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
	var payload EventPayload
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

		extra := EventPayload{Agent: agent, Reason: reason}
		payload = eventPayload(tx, id, extra)
		if err := insertEvent(tx, "task.blocked", payload); err != nil {
			return fmt.Errorf("insert event: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return err
		}

		task, err = s.View(ctx, id)
		return err
	})
	if err == nil {
		runHook(s.hooksDir, "task.blocked", payload)
	}
	return task, err
}


