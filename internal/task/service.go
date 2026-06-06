package task

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// --- Command implementations ---

// Dispatch inserts a new TODO task and returns it.
func (s *Service) Dispatch(ctx context.Context, title, roleBoundary string, priority int) (Task, error) {
	if len(title) > maxTitleLength {
		return Task{}, &ExitError{Code: 2, Message: fmt.Sprintf("title too long (max %d)", maxTitleLength)}
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
		`INSERT INTO tasks (id, title, status, role_boundary, priority)
		 VALUES (?, ?, 'TODO', ?, ?)`,
		id, title, roleBoundary, priority,
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

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit dispatch: %w", err)
	}

	return s.View(ctx, id)
}

// ClaimNext atomically claims the highest-priority available task for a role.
// Returns empty Task with nil error when no work is available (exit 0, JSON {}).
func (s *Service) ClaimNext(ctx context.Context, agent, role string) (Task, error) {
	// Serializable isolation → write lock up front.
	// Required: two concurrent claimers must never get the same task.
	// Retry on SQLITE_BUSY to handle contention gracefully.
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			if strings.Contains(err.Error(), "database is locked") && attempt < 2 {
				// Exponential backoff: 100ms, 200ms, 400ms
				time.Sleep(time.Duration(100*(1<<attempt)) * time.Millisecond)
				lastErr = err
				continue
			}
			return Task{}, fmt.Errorf("claim begin: %w", err)
		}

		row := tx.QueryRow(
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
			 RETURNING id, title, status, role_boundary, priority, assigned_agent, lease_until, created_at, updated_at`,
			agent, defaultLeaseMinutes, role,
		)

		t, err := scanTask(row)
		if err != nil {
			if err == sql.ErrNoRows {
				// No work available — empty result.
				tx.Rollback()
				return Task{}, nil
			}
			tx.Rollback()
			return Task{}, fmt.Errorf("claim update: %w", err)
		}

		_, err = tx.Exec(
			`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'CLAIM')`,
			t.ID, agent,
		)
		if err != nil {
			tx.Rollback()
			return Task{}, fmt.Errorf("insert claim history for task %s agent %s: %w", t.ID, agent, err)
		}

		if err := tx.Commit(); err != nil {
			tx.Rollback()
			return Task{}, fmt.Errorf("commit claim: %w", err)
		}

		return t, nil
	}
	return Task{}, fmt.Errorf("claim after retries: %w", lastErr)
}

// Complete transitions a task to DONE (or IN_REVIEW) and clears the lease.
// Uses a single UPDATE with WHERE guards so the check+update is atomic —
// the write lock is acquired at first write, and SQLite serializes at commit time.
func (s *Service) Complete(ctx context.Context, id, agent string, toReview bool) (Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, fmt.Errorf("complete begin tx: %w", err)
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
		return Task{}, fmt.Errorf("complete update: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return Task{}, fmt.Errorf("complete rows affected: %w", err)
	}
	if n == 0 {
		// Determine why: does the task exist?
		var exists bool
		tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
		if !exists {
			return Task{}, ErrNotFound
		}
		// Existing task — must be wrong agent or wrong state.
		return Task{}, ErrNotAssigned
	}

	_, err = tx.Exec(
		`INSERT INTO history (task_id, agent, action) VALUES (?, ?, ?)`,
		id, agent, action,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert complete history for task %s agent %s: %w", id, agent, err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit complete: %w", err)
	}

	return s.View(ctx, id)
}

// LogProgress appends a note and renews the lease (heartbeat).
func (s *Service) LogProgress(ctx context.Context, id, agent, content string, noteType string) (Task, error) {
	if len(content) > maxNoteLength {
		return Task{}, &ExitError{Code: 2, Message: fmt.Sprintf("note too long (max %d)", maxNoteLength)}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, fmt.Errorf("log begin tx: %w", err)
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
		return Task{}, fmt.Errorf("renew lease: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Task{}, fmt.Errorf("log rows affected: %w", err)
	}
	if n == 0 {
		var exists bool
		tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
		if !exists {
			return Task{}, ErrNotFound
		}
		return Task{}, ErrNotAssigned
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
		return Task{}, fmt.Errorf("insert note: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'PROGRESS')`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert progress history for task %s agent %s: %w", id, agent, err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit log: %w", err)
	}

	return s.View(ctx, id)
}

// Block transitions a task to BLOCKED, clears the lease, and records the reason.
func (s *Service) Block(ctx context.Context, id, agent, reason string) (Task, error) {
	if len(reason) > maxReasonLength {
		return Task{}, &ExitError{Code: 2, Message: fmt.Sprintf("reason too long (max %d)", maxReasonLength)}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, fmt.Errorf("block begin tx: %w", err)
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
		return Task{}, fmt.Errorf("block update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Task{}, fmt.Errorf("block rows affected: %w", err)
	}
	if n == 0 {
		var exists bool
		tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
		if !exists {
			return Task{}, ErrNotFound
		}
		return Task{}, ErrNotAssigned
	}

	_, err = tx.Exec(
		`INSERT INTO notes (task_id, author, note_type, content) VALUES (?, ?, 'BLOCKED', ?)`,
		id, agent, reason,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert block note: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'BLOCK')`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert block history for task %s agent %s: %w", id, agent, err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit block: %w", err)
	}

	return s.View(ctx, id)
}

// ReviewApprove approves an IN_REVIEW task, marking it DONE.
// Any reviewer can approve — no lease ownership check needed.
func (s *Service) ReviewApprove(ctx context.Context, id, agent string) (Task, error) {
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

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit review: %w", err)
	}

	return s.View(ctx, id)
}

// ReviewReject sends an IN_REVIEW task back to TODO, clearing the lease.
// Any reviewer can reject — no lease ownership check needed.
func (s *Service) ReviewReject(ctx context.Context, id, agent, reason string) (Task, error) {
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

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit reject: %w", err)
	}

	return s.View(ctx, id)
}
