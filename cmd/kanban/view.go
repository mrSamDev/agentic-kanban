package main

import (
	"fmt"
	"strings"

	"agent-kanban/internal/task"

	"github.com/spf13/cobra"
)

func viewCmd() *cobra.Command {
	var noteLimit, historyLimit int

	cmd := &cobra.Command{
		Use:   "view <id>",
		Short: "View task details with notes and history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			detail, err := s.ViewDetail(cmd.Context(), args[0], noteLimit, historyLimit)
			if err != nil {
				return err
			}
			writeJSON(detail)
			return nil
		},
	}
	cmd.Flags().IntVar(&noteLimit, "notes", 0, "max notes to return (0 = all)")
	cmd.Flags().IntVar(&historyLimit, "history", 0, "max history entries to return (0 = all)")
	return cmd
}

func searchCmd() *cobra.Command {
	var status, role, agent, project string
	var limit int

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search tasks by filters",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			params := task.SearchParams{
				Status:  task.TaskStatus(status),
				Role:    role,
				Agent:   agent,
				Project: project,
				Limit:   limit,
			}

			tasks, err := s.Search(cmd.Context(), params)
			if err != nil {
				return err
			}
			writeJSON(tasks)
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "filter by status")
	cmd.Flags().StringVar(&role, "role", "", "filter by role boundary")
	cmd.Flags().StringVar(&agent, "agent", "", "filter by assigned agent")
	cmd.Flags().StringVar(&project, "project", "", "filter by project/scope")
	cmd.Flags().IntVar(&limit, "limit", 0, "max results (0 = unlimited)")
	return cmd
}

func statsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show aggregate task statistics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			stats, err := s.Stats(cmd.Context())
			if err != nil {
				return err
			}
			writeJSON(stats)
			return nil
		},
	}
	return cmd
}

func statusCmd() *cobra.Command {
	var asJSON, burndown bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show board status and progress",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			stats, err := s.Burndown(cmd.Context())
			if err != nil {
				return err
			}

			if asJSON {
				writeJSON(stats)
				return nil
			}

			printStatusTable(stats, burndown)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&burndown, "burndown", false, "show progress bars per status")
	return cmd
}

var statusOrder = []string{"TODO", "IN_PROGRESS", "BLOCKED", "IN_REVIEW", "DONE"}

func printStatusTable(stats task.BurndownStats, burndown bool) {
	sep := strings.Repeat("─", 40)
	if burndown {
		fmt.Printf("%-16s %-7s %s\n", "Status", "Count", "Progress")
	} else {
		fmt.Printf("%-16s %s\n", "Status", "Count")
	}
	fmt.Println(sep)

	for _, status := range statusOrder {
		count := stats.ByStatus[status]
		if burndown && stats.Total > 0 {
			bars := int(float64(count) / float64(stats.Total) * 20)
			bar := strings.Repeat("█", bars)
			fmt.Printf("%-16s %-7d %s\n", status, count, bar)
		} else {
			fmt.Printf("%-16s %d\n", status, count)
		}
	}
	// Any statuses not in the fixed order
	for status, count := range stats.ByStatus {
		known := false
		for _, s := range statusOrder {
			if s == status {
				known = true
				break
			}
		}
		if !known {
			fmt.Printf("%-16s %d\n", status, count)
		}
	}

	fmt.Println(sep)
	fmt.Printf("Total: %d  Done: %d  %.0f%% complete\n\n", stats.Total, stats.DoneCount, stats.PercentDone)

	if len(stats.ByRole) > 0 {
		fmt.Println("By Role:")
		for role, count := range stats.ByRole {
			fmt.Printf("  %-12s %d\n", role, count)
		}
	}
}
