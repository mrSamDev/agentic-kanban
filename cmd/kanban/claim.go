package main

import (
	"agent-kanban/internal/task"

	"github.com/spf13/cobra"
)

func claimCmd() *cobra.Command {
	var agent string
	var transfer bool
	var toAgent string

	cmd := &cobra.Command{
		Use:   "claim <id>",
		Short: "Claim a specific task by ID",
		Long: `Claim a specific task by ID (instead of taking the next available).

The task must be in TODO state and have no unmet dependencies.
Useful for subagents that know exactly which task they're working on.

Transfer a claimed task to another agent (hierarchical delegation):

  kanban task claim TASK-5 --agent samdev --transfer --to pi-worker

The transferring agent (--agent) must be the current assigned_agent.
The receiving agent (--to) gets a fresh 15-minute lease.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			if transfer {
				if toAgent == "" {
					return &task.ExitError{Code: 2, Message: "--to is required when --transfer is set"}
				}
				t, err := s.TransferClaim(cmd.Context(), args[0], agent, toAgent)
				if err != nil {
					return err
				}
				writeJSON(t)
				return nil
			}

			t, err := s.ClaimByID(cmd.Context(), args[0], agent)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().BoolVar(&transfer, "transfer", false, "transfer claim to another agent")
	cmd.Flags().StringVar(&toAgent, "to", "", "target agent for transfer")
	_ = cmd.MarkFlagRequired("agent")
	return cmd
}