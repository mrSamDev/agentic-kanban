package task

import (
	"context"
	"fmt"
)

func (s *Service) BatchUpdatePriority(ctx context.Context, ids []string, priority int) (int, error) {
	if priority < 0 || priority > 999 {
		return 0, &ExitError{Code: 2, Message: "priority must be between 0 and 999"}
	}
	var updated int
	var payloads []EventPayload
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("batch priority begin tx: %w", err)
		}
		defer tx.Rollback()

		metas, err := loadTaskMetas(tx, ids)
		if err != nil {
			return fmt.Errorf("batch priority load metas: %w", err)
		}

		updated = 0
		payloads = nil
		for _, id := range ids {
			res, err := tx.Exec(
				`UPDATE tasks SET priority = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				priority, id,
			)
			if err != nil {
				return fmt.Errorf("update task %s: %w", id, err)
			}
			n, _ := res.RowsAffected()
			if n > 0 {
				updated++
				p := buildPayload(metas, id, EventPayload{Priority: fmt.Sprintf("%d", priority)})
				payloads = append(payloads, p)
				if err := insertEvent(tx, "task.priority_updated", p); err != nil {
					return fmt.Errorf("insert priority event for %s: %w", id, err)
				}
			}
		}
		return tx.Commit()
	})
	if err == nil {
		for _, p := range payloads {
			runHook(s.hooksDir, "task.priority_updated", p)
		}
	}
	return updated, err
}

func (s *Service) BatchUpdateProject(ctx context.Context, ids []string, project string) (int, error) {
	if project == "" {
		return 0, &ExitError{Code: 2, Message: "project cannot be empty"}
	}
	var updated int
	var payloads []EventPayload
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("batch project begin tx: %w", err)
		}
		defer tx.Rollback()

		metas, err := loadTaskMetas(tx, ids)
		if err != nil {
			return fmt.Errorf("batch project load metas: %w", err)
		}

		updated = 0
		payloads = nil
		for _, id := range ids {
			res, err := tx.Exec(
				`UPDATE tasks SET project = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				project, id,
			)
			if err != nil {
				return fmt.Errorf("update task %s: %w", id, err)
			}
			n, _ := res.RowsAffected()
			if n > 0 {
				updated++
				p := buildPayload(metas, id, EventPayload{Project: project})
				payloads = append(payloads, p)
				if err := insertEvent(tx, "task.project_updated", p); err != nil {
					return fmt.Errorf("insert project event for %s: %w", id, err)
				}
			}
		}
		return tx.Commit()
	})
	if err == nil {
		for _, p := range payloads {
			runHook(s.hooksDir, "task.project_updated", p)
		}
	}
	return updated, err
}