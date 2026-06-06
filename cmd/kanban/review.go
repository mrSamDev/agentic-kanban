package main

import (
	"context"

	"github.com/spf13/cobra"
)

func approveCmd() *cobra.Command {
	var agent string

	cmd := &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve a task in review, mark as done",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			t, err := s.ReviewApprove(context.Background(), args[0], agent)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.MarkFlagRequired("agent")
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

			t, err := s.ReviewReject(context.Background(), args[0], agent, reason)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "rejection reason (required)")
	cmd.MarkFlagRequired("agent")
	cmd.MarkFlagRequired("reason")
	return cmd
}
