package task

import (
	"context"
	"fmt"
)

// BatchUpdatePriority sets the priority for multiple tasks.
func (s *Service) BatchUpdatePriority(ctx context.Context, ids []string, priority int) (int, error) {
	var updated int
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("batch priority begin tx: %w", err)
		}
		defer tx.Rollback()

		updated = 0
		for _, id := range ids {
			res, err := tx.Exec(
				`UPDATE tasks SET priority = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				priority, id,
			)
			if err != nil {
				return fmt.Errorf("update task %s: %w", id, err)
			}
			n, _ := res.RowsAffected()
			updated += int(n)
		}

		return tx.Commit()
	})
	return updated, err
}

// BatchUpdateProject sets the project label for multiple tasks.
func (s *Service) BatchUpdateProject(ctx context.Context, ids []string, project string) (int, error) {
	var updated int
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("batch project begin tx: %w", err)
		}
		defer tx.Rollback()

		updated = 0
		for _, id := range ids {
			res, err := tx.Exec(
				`UPDATE tasks SET project = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				project, id,
			)
			if err != nil {
				return fmt.Errorf("update task %s: %w", id, err)
			}
			n, _ := res.RowsAffected()
			updated += int(n)
		}

		return tx.Commit()
	})
	return updated, err
}
