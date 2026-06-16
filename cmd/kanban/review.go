package main

import (
	"agent-kanban/internal/task"

	"github.com/spf13/cobra"
)

func approveCmd() *cobra.Command {
	var agent, project string
	var approveAll bool

	cmd := &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve a task in review, mark as done. Use --all to approve all IN_REVIEW tasks.",
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			if approveAll {
				tasks, errs := s.ApproveAll(cmd.Context(), agent, project)
				if errs != nil && len(errs) > 0 {
					// Log per-task errors but still report successfully approved tasks
					for _, e := range errs {
						writeStderr(e.Error())
					}
				}
				if len(tasks) == 0 {
					writeJSON(map[string]string{"message": "no tasks approved"})
					return nil
				}
				writeJSON(tasks)
				return nil
			}

			if len(args) != 1 {
				return &task.ExitError{Code: 2, Message: "requires a task ID when not using --all"}
			}

			t, err := s.ReviewApprove(cmd.Context(), args[0], agent)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().BoolVar(&approveAll, "all", false, "approve all IN_REVIEW tasks")
	cmd.Flags().StringVar(&project, "project", "", "limit --all to a specific project/scope")
	_ = cmd.MarkFlagRequired("agent")
	return cmd
}

func rejectCmd() *cobra.Command {
	var agent, reason string

	cmd := &cobra.Command{
		Use:   "reject <id>",
		Short: "Reject a task in review, send back to TODO",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			t, err := s.ReviewReject(cmd.Context(), args[0], agent, reason)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "rejection reason (required)")
	_ = cmd.MarkFlagRequired("agent")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}
