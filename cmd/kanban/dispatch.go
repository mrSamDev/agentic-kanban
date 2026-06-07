package main

import (
	"github.com/spf13/cobra"
)

func dispatchCmd() *cobra.Command {
	var title, role, project, dependsOn string
	var priority int

	cmd := &cobra.Command{
		Use:   "dispatch",
		Short: "Create a new task",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			var depPtr *string
			if dependsOn != "" {
				depPtr = &dependsOn
			}

			t, err := s.Dispatch(cmd.Context(), title, role, project, priority, depPtr)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "task title (required)")
	cmd.Flags().StringVar(&role, "role", "", "role boundary (required)")
	cmd.Flags().StringVar(&project, "project", "", "project/scope label (default: default)")
	cmd.Flags().IntVar(&priority, "priority", 100, "priority (lower = more urgent)")
	cmd.Flags().StringVar(&dependsOn, "depends-on", "", "comma-separated dependency task IDs")
	cmd.MarkFlagRequired("title")
	cmd.MarkFlagRequired("role")
	cmd.PreRunE = func(_ *cobra.Command, _ []string) error {
		if err := nonEmpty(title, "title"); err != nil {
			return err
		}
		if err := nonEmpty(role, "role"); err != nil {
			return err
		}
		return nil
	}
	return cmd
}

func claimNextCmd() *cobra.Command {
	var agent, role, project string

	cmd := &cobra.Command{
		Use:   "claim-next",
		Short: "Claim the next available task by role",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			t, err := s.ClaimNext(cmd.Context(), agent, role, project)
			if err != nil {
				return err
			}
			if t.ID == "" {
				writeJSON(struct{}{})
				return nil
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().StringVar(&role, "role", "", "role (required)")
	cmd.Flags().StringVar(&project, "project", "", "filter by project/scope")
	cmd.MarkFlagRequired("agent")
	cmd.MarkFlagRequired("role")
	return cmd
}
