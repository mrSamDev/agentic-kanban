package task

import (
	"context"
	"database/sql"
	"fmt"
)

// Uses Serializable isolation so two concurrent claimers never get the same task.
// Also reclaims tasks where the previous agent's lease expired.
func (s *Service) ClaimNext(ctx context.Context, agent, role, project string) (Task, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
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

		if err := insertEvent(tx, "task.claimed", map[string]string{
			"task_id": t.ID,
			"agent":   agent,
			"title":   t.Title,
			"project": t.Project,
			"priority": fmt.Sprintf("%d", t.Priority),
			"role_boundary": t.RoleBoundary,
		}); err != nil {
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
		runHook(s.hooksDir, "task.claimed", map[string]string{
			"task_id": task.ID,
			"agent":   agent,
			"title":   task.Title,
			"project": task.Project,
			"priority": fmt.Sprintf("%d", task.Priority),
			"role_boundary": task.RoleBoundary,
		})
	}
	return task, nil
}
