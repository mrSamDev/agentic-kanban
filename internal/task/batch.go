package task

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) batchUpdate(ctx context.Context, ids []string, column string, value any, eventType string, extra EventPayload) (int, error) {
	if column != "priority" && column != "project" {
		return 0, fmt.Errorf("invalid batch column: %q", column)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	var updated int
	var payloads []EventPayload
	err := s.retryOnBusy(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("batch %s begin tx: %w", column, err)
		}
		defer func() { _ = tx.Rollback() }()

		metas, err := preloadTaskMetas(tx, ids)
		if err != nil {
			return fmt.Errorf("batch %s load metas: %w", column, err)
		}

		// Single bulk UPDATE instead of N per-task UPDATEs
		placeholders := make([]string, len(ids))
		args := make([]any, 0, len(ids)+1)
		args = append(args, value)
		for i, id := range ids {
			placeholders[i] = "?"
			args = append(args, id)
		}
		res, err := tx.Exec(
			fmt.Sprintf(`UPDATE tasks SET %s = ?, updated_at = CURRENT_TIMESTAMP WHERE id IN (%s)`, column, strings.Join(placeholders, ",")),
			args...,
		)
		if err != nil {
			return fmt.Errorf("batch update %s: %w", column, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("batch rows affected: %w", err)
		}
		updated = int(n)

		payloads = make([]EventPayload, 0, len(metas))
		for _, id := range ids {
			m, ok := metas[id]
			if !ok {
				continue
			}
			p := extra
			p.TaskID = id
			p.Title = m.title
			p.Project = m.project
			p.Priority = fmt.Sprintf("%d", m.priority)
			if extra.Project != "" {
				p.Project = extra.Project
			}
			if extra.Priority != "" {
				p.Priority = extra.Priority
			}
			payloads = append(payloads, p)
			if err := insertEvent(tx, eventType, p); err != nil {
				return fmt.Errorf("insert event for %s: %w", id, err)
			}
		}
		return tx.Commit()
	})
	if err == nil {
		for _, p := range payloads {
			runHook(s.HookRunner, s.hooksDir, eventType, p)
		}
	}
	return updated, err
}

func (s *Service) BatchUpdatePriority(ctx context.Context, ids []string, priority int) (int, error) {
	if priority < 0 || priority > 999 {
		return 0, &ExitError{Code: 2, Message: "priority must be between 0 and 999"}
	}
	return s.batchUpdate(ctx, ids, "priority", priority, "task.priority_updated", EventPayload{Priority: fmt.Sprintf("%d", priority)})
}

func (s *Service) BatchUpdateProject(ctx context.Context, ids []string, project string) (int, error) {
	if project == "" {
		return 0, &ExitError{Code: 2, Message: "project cannot be empty"}
	}
	return s.batchUpdate(ctx, ids, "project", project, "task.project_updated", EventPayload{Project: project})
}