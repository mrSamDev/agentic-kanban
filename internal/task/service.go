package task

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
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

	// Prevent impossible dependency graphs: detect cycles before committing.
	// Walk the transitive closure of the new task's dependencies. If any path
	// revisits a node already on the current recursion stack, the new task would
	// inherit a cycle. O(depth) instead of O(total-tasks).
	if dependsOn != nil && *dependsOn != "" {
		onStack := map[string]bool{}
		var walk func(current string) (bool, error)
		walk = func(current string) (bool, error) {
			if onStack[current] {
				return true, nil // back edge found
			}
			onStack[current] = true
			var raw sql.NullString
			err := tx.QueryRow(`SELECT depends_on FROM tasks WHERE id = ?`, current).Scan(&raw)
			if err == sql.ErrNoRows {
				delete(onStack, current)
				return false, nil
			}
			if err != nil {
				delete(onStack, current)
				return false, fmt.Errorf("cycle check look up %s: %w", current, err)
			}
			if raw.Valid && raw.String != "" {
				for _, d := range strings.Split(raw.String, ",") {
					d = strings.TrimSpace(d)
					if d == "" {
						continue
					}
					cycle, err := walk(d)
					if err != nil {
						delete(onStack, current)
						return false, err
					}
					if cycle {
						delete(onStack, current)
						return true, nil
					}
				}
			}
			delete(onStack, current)
			return false, nil
		}

		for _, d := range strings.Split(*dependsOn, ",") {
			d = strings.TrimSpace(d)
			if d == "" {
				continue
			}
			cycle, err := walk(d)
			if err != nil {
				return Task{}, err
			}
			if cycle {
				return Task{}, fmt.Errorf("cycle detected: adding %s would create a dependency cycle", id)
			}
		}
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
			var actualAgent sql.NullString
			err := tx.QueryRow(`SELECT assigned_agent FROM tasks WHERE id = ?`, id).Scan(&actualAgent)
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("check task: %w", err)
			}
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
			var actualAgent sql.NullString
			err := tx.QueryRow(`SELECT assigned_agent FROM tasks WHERE id = ?`, id).Scan(&actualAgent)
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("check task: %w", err)
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
			var actualAgent sql.NullString
			err := tx.QueryRow(`SELECT assigned_agent FROM tasks WHERE id = ?`, id).Scan(&actualAgent)
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("check task: %w", err)
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

// BatchComplete completes multiple tasks in one serializable transaction.
// Processes all IDs; returns completed tasks + per-ID errors for partial failures.
func (s *Service) BatchComplete(ctx context.Context, ids []string, agent string, toReview bool) ([]Task, []error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	newStatus := StatusDone
	action := "COMPLETE"
	eventType := "task.completed"
	if toReview {
		newStatus = StatusInReview
		action = "REVIEW"
		eventType = "task.submitted_for_review"
	}

	var completed []Task
	var errs []error

	err := s.retryOnBusy(func() error {
		// Reset on retry to avoid duplicate entries
		completed = nil
		errs = nil

		tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return fmt.Errorf("batch complete begin tx: %w", err)
		}
		defer tx.Rollback()

		for _, id := range ids {
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
				errs = append(errs, fmt.Errorf("complete %s: %w", id, err))
				continue
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				if alreadyDone(tx, id, string(newStatus)) {
					t, err := reRead(tx, id)
					if err != nil {
						errs = append(errs, err)
					} else {
						completed = append(completed, t)
					}
				} else {
					errs = append(errs, fmt.Errorf("%s: not assigned to %s", id, agent))
				}
				continue
			}

			if _, err := tx.Exec(
				`INSERT INTO history (task_id, agent, action) VALUES (?, ?, ?)`,
				id, agent, action,
			); err != nil {
				errs = append(errs, fmt.Errorf("%s history: %w", id, err))
				continue
			}

			payload := eventPayload(tx, id, EventPayload{Agent: agent})
			if err := insertEvent(tx, eventType, payload); err != nil {
				errs = append(errs, fmt.Errorf("%s event: %w", id, err))
				continue
			}

			// Re-read task
			row := tx.QueryRow(
				`SELECT id, title, status, role_boundary, project, priority,
				        assigned_agent, lease_until, created_at, updated_at, depends_on, claimed_by
				   FROM tasks WHERE id = ?`, id,
			)
			t, scanErr := scanTask(row)
			if scanErr != nil {
				errs = append(errs, fmt.Errorf("%s re-read: %w", id, scanErr))
				continue
			}
			completed = append(completed, t)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("batch complete commit: %w", err)
		}
		return nil
	})
	if err != nil {
		return completed, append(errs, fmt.Errorf("tx: %w", err))
	}

	// Fire hooks outside tx
	for _, t := range completed {
		payload := EventPayload{
			TaskID:       t.ID,
			Agent:        agent,
			Title:        t.Title,
			Project:      t.Project,
			Priority:     fmt.Sprintf("%d", t.Priority),
			RoleBoundary: t.RoleBoundary,
		}
		runHook(s.hooksDir, eventType, payload)
	}
	return completed, errs
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
			var actualAgent sql.NullString
			err := tx.QueryRow(`SELECT assigned_agent FROM tasks WHERE id = ?`, id).Scan(&actualAgent)
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("check task: %w", err)
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



// ApproveAll batch-approves all IN_REVIEW tasks for a given project (or all projects if empty).
func (s *Service) ApproveAll(ctx context.Context, agent, project string) ([]Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	var approved []Task
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return fmt.Errorf("approve all begin tx: %w", err)
		}
		defer tx.Rollback()

		query := `SELECT id, title, status, role_boundary, project, priority,
		        assigned_agent, lease_until, created_at, updated_at, depends_on, claimed_by
		   FROM tasks WHERE status = 'IN_REVIEW'`
		var args []any
		if project != "" {
			query += " AND project = ?"
			args = append(args, project)
		}
		query += " ORDER BY priority ASC"

		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("approve all query: %w", err)
		}
		defer rows.Close()

		var tasks []Task
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				return fmt.Errorf("approve all scan: %w", err)
			}
			tasks = append(tasks, t)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("approve all rows: %w", err)
		}
		rows.Close()

		for _, t := range tasks {
			if os.Getenv("KANBAN_ALLOW_SELF_REVIEW") != "true" {
				if err := checkSelfReview(tx, t.ID, agent); err != nil {
					return fmt.Errorf("%s: %w", t.ID, err)
				}
			}

			res, err := tx.Exec(
				`UPDATE tasks
				    SET status = 'DONE', assigned_agent = NULL, lease_until = NULL,
				        updated_at = CURRENT_TIMESTAMP
				  WHERE id = ? AND status = 'IN_REVIEW'`,
				t.ID,
			)
			if err != nil {
				return fmt.Errorf("approve all update %s: %w", t.ID, err)
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				continue
			}

			if _, err := tx.Exec(
				`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'REVIEW')`,
				t.ID, agent,
			); err != nil {
				return fmt.Errorf("approve all history %s: %w", t.ID, err)
			}

			payload := eventPayload(tx, t.ID, EventPayload{Agent: agent})
			if err := insertEvent(tx, "review.approved", payload); err != nil {
				return fmt.Errorf("approve all event %s: %w", t.ID, err)
			}

			approved = append(approved, t)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("approve all commit: %w", err)
		}
		return nil
	})
	if err != nil {
		return approved, err
	}

	for _, t := range approved {
		payload := EventPayload{
			TaskID:       t.ID,
			Agent:        agent,
			Title:        t.Title,
			Project:      t.Project,
			Priority:     fmt.Sprintf("%d", t.Priority),
			RoleBoundary: t.RoleBoundary,
		}
		runHook(s.hooksDir, "review.approved", payload)
	}
	return approved, nil
}
