package main

import (
	"fmt"
	"strings"
	"time"

	"agent-kanban/internal/task"

	"github.com/spf13/cobra"
)

func pruneCmd() *cobra.Command {
	var before string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Prune old events, history, and notes",
		Long: `Delete records older than a specified age.

After pruning, run VACUUM to reclaim disk space:
  sqlite3 .kanban/kanban.db "VACUUM"

Examples:
  kanban prune --before 7d          # delete everything older than 7 days
  kanban prune --before 30d         # delete everything older than 30 days
  kanban prune --before 2024-06-01  # delete before a specific date
  kanban prune --before 30d --dry-run  # preview without deleting
`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			beforeTime, err := parseBefore(before)
			if err != nil {
				return err
			}

			result, err := s.Prune(cmd.Context(), beforeTime, dryRun)
			if err != nil {
				return err
			}
			writeJSON(result)
			return nil
		},
	}
	cmd.Flags().StringVar(&before, "before", "30d", "delete records older than this (7d, 30d, or YYYY-MM-DD)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be deleted without deleting")
	return cmd
}

func parseBefore(s string) (time.Time, error) {
	s = strings.ToLower(s)
	if len(s) > 0 && s[len(s)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil && days > 0 {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, &task.ExitError{Code: 2, Message: fmt.Sprintf("invalid --before format: %q (use 7d, 30d, or YYYY-MM-DD)", s)}
}
