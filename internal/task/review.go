package task

import (
	"context"
	"database/sql"
	"fmt"
	"os"
)

// Any reviewer can approve — no lease ownership check needed.
func (s *Service) ReviewApprove(ctx context.Context, id, agent string) (Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	var task Task
	var payload EventPayload
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("review begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		// Check self-review gate (configurable via KANBAN_ALLOW_SELF_REVIEW env var)
		if os.Getenv("KANBAN_ALLOW_SELF_REVIEW") != "true" {
			if err := checkSelfReview(tx, id, agent); err != nil {
				return err
			}
		}

		res, err := tx.Exec(
			`UPDATE tasks
			    SET status = 'DONE', assigned_agent = NULL, lease_until = NULL,
			        updated_at = CURRENT_TIMESTAMP
			  WHERE id = ? AND status = 'IN_REVIEW'`,
			id,
		)
		if err != nil {
			return fmt.Errorf("review update: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("review rows affected: %w", err)
		}
		if n == 0 {
			var exists bool
			err := tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("check task: %w", err)
			}
			return ErrInvalidState
		}

		_, err = tx.Exec(
			`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'REVIEW')`,
			id, agent,
		)
		if err != nil {
			return fmt.Errorf("insert review history for task %s agent %s: %w", id, agent, err)
		}

		payload = eventPayload(tx, id, EventPayload{Agent: agent})
		if err := insertEvent(tx, "review.approved", payload); err != nil {
			return fmt.Errorf("insert event: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return err
		}

		task, err = s.View(ctx, id)
		return err
	})
	if err == nil {
		runHook(s.hooksDir, "review.approved", payload)
	}
	return task, err
}

// Any reviewer can reject — no lease ownership check needed.
func (s *Service) ReviewReject(ctx context.Context, id, agent, reason string) (Task, error) {
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
			return fmt.Errorf("reject begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		// Check self-review gate (configurable via KANBAN_ALLOW_SELF_REVIEW env var)
		if os.Getenv("KANBAN_ALLOW_SELF_REVIEW") != "true" {
			if err := checkSelfReview(tx, id, agent); err != nil {
				return err
			}
		}

		res, err := tx.Exec(
			`UPDATE tasks
			    SET status = 'TODO', assigned_agent = NULL, lease_until = NULL,
			        updated_at = CURRENT_TIMESTAMP
			  WHERE id = ? AND status = 'IN_REVIEW'`,
			id,
		)
		if err != nil {
			return fmt.Errorf("reject update: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("reject rows affected: %w", err)
		}
		if n == 0 {
			var exists bool
			err := tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("check task: %w", err)
			}
			return ErrInvalidState
		}

		_, err = tx.Exec(
			`INSERT INTO notes (task_id, author, note_type, content) VALUES (?, ?, 'REJECTED', ?)`,
			id, agent, reason,
		)
		if err != nil {
			return fmt.Errorf("insert reject note: %w", err)
		}

		_, err = tx.Exec(
			`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'REVIEW')`,
			id, agent,
		)
		if err != nil {
			return fmt.Errorf("insert reject history for task %s agent %s: %w", id, agent, err)
		}

		extra := EventPayload{Agent: agent, Reason: reason}
		payload = eventPayload(tx, id, extra)
		if err := insertEvent(tx, "review.rejected", payload); err != nil {
			return fmt.Errorf("insert event: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return err
		}

		task, err = s.View(ctx, id)
		return err
	})
	if err == nil {
		runHook(s.hooksDir, "review.rejected", payload)
	}
	return task, err
}

// checkSelfReview verifies the reviewing agent is not the same agent who claimed the task.
// Uses the claimed_by column (immutable snapshot set on first claim, preserved across lease
// reclamations) rather than the prunable history table.
// Returns nil (allows review) when there is no claiming agent (task created directly in IN_REVIEW).
// Disabled by setting KANBAN_ALLOW_SELF_REVIEW=true.
func checkSelfReview(tx *sql.Tx, id, agent string) error {
	var claimedBy sql.NullString
	err := tx.QueryRow(
		`SELECT claimed_by FROM tasks WHERE id = ?`,
		id,
	).Scan(&claimedBy)

	if err == sql.ErrNoRows {
		return nil // task doesn't exist, caller will handle
	}
	if err != nil {
		return fmt.Errorf("check self-review: %w", err)
	}
	if claimedBy.Valid && claimedBy.String == agent {
		return ErrSelfReview
	}
	return nil
}
