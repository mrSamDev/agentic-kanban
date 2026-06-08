package main

import (
	"agent-kanban/internal/task"

	"github.com/spf13/cobra"
)

func completeCmd() *cobra.Command {
	var agent string
	var toReview bool

	cmd := &cobra.Command{
		Use:   "complete <id>",
		Short: "Mark a task as done (or submit for review)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			t, err := s.Complete(cmd.Context(), args[0], agent, toReview)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().BoolVar(&toReview, "review", false, "submit for review instead of completing")
	cmd.MarkFlagRequired("agent")
	return cmd
}

func logProgressCmd() *cobra.Command {
	var agent, note, noteType string

	cmd := &cobra.Command{
		Use:   "log-progress <id>",
		Short: "Log progress and renew lease",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			t, err := s.LogProgress(cmd.Context(), args[0], agent, note, noteType)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().StringVar(&note, "note", "", "progress note (required)")
	cmd.Flags().StringVar(&noteType, "type", "", "note type (PROGRESS|ERROR|DECISION)")
	cmd.MarkFlagRequired("agent")
	cmd.MarkFlagRequired("note")
	cmd.PreRunE = func(_ *cobra.Command, _ []string) error {
		if noteType != "" && !validNoteTypes[noteType] {
			return &task.ExitError{Code: 2, Message: "note type must be PROGRESS, ERROR, or DECISION"}
		}
		return nil
	}
	return cmd
}

func extendLeaseCmd() *cobra.Command {
	var agent string
	var minutes int

	cmd := &cobra.Command{
		Use:   "extend-lease <id>",
		Short: "Extend the lease on a claimed task",
		Long: `Extend the lease on a claimed task (heartbeat, no state transition).
Defaults to 15 minutes if --minutes is omitted.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			t, err := s.ExtendLease(cmd.Context(), args[0], agent, minutes)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().IntVar(&minutes, "minutes", 0, "lease duration in minutes (default: 15)")
	cmd.MarkFlagRequired("agent")
	return cmd
}

func blockCmd() *cobra.Command {
	var agent, reason string

	cmd := &cobra.Command{
		Use:   "block <id>",
		Short: "Block a task with a reason",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			t, err := s.Block(cmd.Context(), args[0], agent, reason)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "block reason (required)")
	cmd.MarkFlagRequired("agent")
	cmd.MarkFlagRequired("reason")
	return cmd
}
