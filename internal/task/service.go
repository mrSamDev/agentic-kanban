package task

import (
	"context"
	"database/sql"
	"fmt"
)

// --- Command implementations ---

// Dispatch inserts a new TODO task and returns it.
func (s *Service) Dispatch(title, roleBoundary string, priority int) (Task, error) {
	tx, err := s.db.Begin()
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

	return s.View(id)
}

// ClaimNext atomically claims the highest-priority available task for a role.
// Returns empty Task with nil error when no work is available (exit 0, JSON {}).
func (s *Service) ClaimNext(agent, role string) (Task, error) {
	// Serializable isolation → BEGIN IMMEDIATE → write lock up front.
	// Required: two concurrent claimers must never get the same task.
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Task{}, fmt.Errorf("claim begin immediate: %w", err)
	}
	defer tx.Rollback()

	row := tx.QueryRow(
		`UPDATE tasks
		   SET status         = 'IN_PROGRESS',
		       assigned_agent = ?,
		       lease_until    = datetime('now', '+15 minutes'),
		       updated_at     = CURRENT_TIMESTAMP
		 WHERE id = (
		   SELECT id FROM tasks
		    WHERE (role_boundary = ? AND (status = 'TODO'
		           OR (status = 'IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP)))
		       OR (status = 'IN_REVIEW' AND ? = 'reviewer')
		    ORDER BY
		      CASE
		        WHEN status = 'IN_REVIEW' THEN 0  -- reviewer tasks first
		        ELSE 1
		      END,
		      priority ASC, created_at ASC
		    LIMIT 1
		 )
		 RETURNING *`,
		agent, role, role,
	)

	t, err := scanTask(row)
	if err != nil {
		if err == sql.ErrNoRows {
			// No work available — empty result.
			tx.Rollback()
			return Task{}, nil
		}
		return Task{}, fmt.Errorf("claim update: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'CLAIM')`,
		t.ID, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert claim history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit claim: %w", err)
	}

	return t, nil
}

// Complete transitions a task to DONE (or IN_REVIEW) and clears the lease.
// Uses a single UPDATE with WHERE guards so the check+update is atomic without
// BEGIN IMMEDIATE — the write lock is acquired at first write, and SQLite
// serializes at commit time.
func (s *Service) Complete(id, agent string, toReview bool) (Task, error) {
	tx, err := s.db.Begin()
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

	n, _ := res.RowsAffected()
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
		return Task{}, fmt.Errorf("insert complete history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit complete: %w", err)
	}

	return s.View(id)
}

// LogProgress appends a note and renews the lease (heartbeat).
func (s *Service) LogProgress(id, agent, content string, noteType string) (Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Task{}, fmt.Errorf("log begin tx: %w", err)
	}
	defer tx.Rollback()

	// Verify ownership via UPDATE condition — atomic with the lease renewal.
	res, err := tx.Exec(
		`UPDATE tasks
		    SET lease_until = datetime('now', '+15 minutes'),
		        updated_at = CURRENT_TIMESTAMP
		  WHERE id = ? AND assigned_agent = ?`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("renew lease: %w", err)
	}
	n, _ := res.RowsAffected()
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
		return Task{}, fmt.Errorf("insert progress history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit log: %w", err)
	}

	return s.View(id)
}

// Block transitions a task to BLOCKED, clears the lease, and records the reason.
func (s *Service) Block(id, agent, reason string) (Task, error) {
	tx, err := s.db.Begin()
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
	n, _ := res.RowsAffected()
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
		return Task{}, fmt.Errorf("insert block history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit block: %w", err)
	}

	return s.View(id)
}

// ReviewApprove transitions a claimed (IN_PROGRESS) task to DONE. Agent must hold the lease.
func (s *Service) ReviewApprove(id, agent string) (Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Task{}, fmt.Errorf("review begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE tasks
		    SET status = 'DONE', assigned_agent = NULL, lease_until = NULL,
		        updated_at = CURRENT_TIMESTAMP
		  WHERE id = ? AND status = 'IN_PROGRESS' AND assigned_agent = ?`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("review update: %w", err)
	}
	n, _ := res.RowsAffected()
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
		return Task{}, fmt.Errorf("insert review history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit review: %w", err)
	}

	return s.View(id)
}

// ReviewReject sends a claimed (IN_PROGRESS) task back to TODO, clearing the lease. Agent must hold the lease.
func (s *Service) ReviewReject(id, agent, reason string) (Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Task{}, fmt.Errorf("reject begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE tasks
		    SET status = 'TODO', assigned_agent = NULL, lease_until = NULL,
		        updated_at = CURRENT_TIMESTAMP
		  WHERE id = ? AND status = 'IN_PROGRESS' AND assigned_agent = ?`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("reject update: %w", err)
	}
	n, _ := res.RowsAffected()
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
		return Task{}, fmt.Errorf("insert reject history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit reject: %w", err)
	}

	return s.View(id)
}
