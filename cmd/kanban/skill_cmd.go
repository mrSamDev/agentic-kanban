package main

import (
	"fmt"

	"agent-kanban/internal/bootstrap"

	"github.com/spf13/cobra"
)

func skillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "List and view embedded skill definitions",
		Long:  `Inspect the built-in skill definitions embedded in the binary.`,
	}

	cmd.AddCommand(skillListCmd())
	cmd.AddCommand(skillViewCmd())

	return cmd
}

func skillListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all available skills",
		RunE: func(_ *cobra.Command, _ []string) error {
			skills := bootstrap.ListSkills()
			for _, s := range skills {
				fmt.Printf("%s\t%s\t%s\n", s.Role, s.Name, s.File)
			}
			return nil
		},
	}
}

func skillViewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "view <name>",
		Short: "View a skill definition by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			content, role, err := bootstrap.ReadSkill(args[0])
			if err != nil {
				return err
			}
			fmt.Printf("role: %s\n---\n%s", role, content)
			return nil
		},
	}
}