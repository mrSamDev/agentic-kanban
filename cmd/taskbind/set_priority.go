package main

import (
	"github.com/spf13/cobra"
)

func setPriorityCmd() *cobra.Command {
	var id string
	var priority int

	cmd := &cobra.Command{
		Use:   "set-priority",
		Short: "Update a task's priority",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			t, err := s.SetPriority(cmd.Context(), id, priority)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task ID (required)")
	cmd.Flags().IntVar(&priority, "priority", 100, "new priority (0-999, lower = more urgent)")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}