package main

import (
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

			project, _ := cmd.Flags().GetString("project")
			stats, err := s.Stats(cmd.Context(), project)
			if err != nil {
				return err
			}
			writeJSON(stats)
			return nil
		},
	}
	cmd.Flags().String("project", "", "filter by project")
	return cmd
}

func statusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show board status and progress (JSON)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			project, _ := cmd.Flags().GetString("project")
			stats, err := s.Burndown(cmd.Context(), project)
			if err != nil {
				return err
			}

			writeJSON(stats)
			return nil
		},
	}
	cmd.Flags().String("project", "", "filter by project")
	return cmd
}
