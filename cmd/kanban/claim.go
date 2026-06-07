package main

import (
	"github.com/spf13/cobra"
)

func claimCmd() *cobra.Command {
	var agent string

	cmd := &cobra.Command{
		Use:   "claim <id>",
		Short: "Claim a specific task by ID",
		Long: `Claim a specific task by ID (instead of taking the next available).
The task must be in TODO state and have no unmet dependencies.
Useful for subagents that know exactly which task they're working on.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			t, err := s.ClaimByID(cmd.Context(), args[0], agent)
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