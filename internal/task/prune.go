package task

import (
	"context"
	"fmt"
	"time"
)

type PruneResult struct {
	EventsDeleted  int64 `json:"events_deleted"`
	HistoryDeleted int64 `json:"history_deleted"`
	NotesDeleted   int64 `json:"notes_deleted"`
	DryRun         bool  `json:"dry_run"`
}

func (s *Service) Prune(ctx context.Context, before time.Time, dryRun bool) (PruneResult, error) {
	var result PruneResult
	result.DryRun = dryRun
	var err error

	result.EventsDeleted, err = s.pruneTable(ctx, "events", before, dryRun)
	if err != nil {
		return result, err
	}

	result.HistoryDeleted, err = s.pruneTable(ctx, "history", before, dryRun)
	if err != nil {
		return result, err
	}

	result.NotesDeleted, err = s.pruneTable(ctx, "notes", before, dryRun)
	if err != nil {
		return result, err
	}

	return result, nil
}

// allowedPruneTables prevents SQL injection via table name interpolation.
var allowedPruneTables = map[string]bool{
	"events":  true,
	"history": true,
	"notes":   true,
}

func (s *Service) pruneTable(ctx context.Context, table string, before time.Time, dryRun bool) (int64, error) {
	if !allowedPruneTables[table] {
		return 0, fmt.Errorf("prune: invalid table %q", table)
	}

	var query string
	if dryRun {
		query = fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE created_at < ?", table)
	} else {
		query = fmt.Sprintf("DELETE FROM %s WHERE created_at < ?", table)
	}

	if dryRun {
		var count int64
		err := s.db.QueryRowContext(ctx, query, before).Scan(&count)
		if err != nil {
			return 0, fmt.Errorf("count %s: %w", table, err)
		}
		return count, nil
	}

	res, err := s.db.ExecContext(ctx, query, before)
	if err != nil {
		return 0, fmt.Errorf("prune %s: %w", table, err)
	}
	return res.RowsAffected()
}

