package main

import (
	"agent-kanban/internal/bootstrap"

	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	var harness, plan, dir string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a project for agent coordination",
		Long: `Scaffold a project with kanban database, skill files, and optional task dispatch from a plan file.

Creates .kanban/kanban.db, agent skill directories, and optionally parses
--plan to dispatch the first batch of tasks.

  kanban init
  kanban init --harness pi
  kanban init --harness pi --plan plan.md
  kanban init --harness claude --plan plan.md --dir ./my-project

Supported harnesses: pi (default), claude, generic.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			return bootstrap.Init(bootstrap.InitOptions{
				Dir:      dir,
				DBPath:   cfg.DBPath,
				Harness:  bootstrap.Harness(harness),
				PlanPath: plan,
			})
		},
	}
	cmd.Flags().StringVar(&harness, "harness", "", "agent harness (pi, claude, generic; prompts if empty)")
	cmd.Flags().StringVar(&plan, "plan", "", "path to plan.md or plan.json for initial task dispatch")
	cmd.Flags().StringVar(&dir, "dir", ".", "project root directory")
	return cmd
}

func reInitCmd() *cobra.Command {
	var harness, plan, dir string

	cmd := &cobra.Command{
		Use:   "re-init",
		Short: "Re-scaffold agent files without touching the database",
		Long: `Re-write skill files, agent definitions, and extensions.

The database is left untouched. Useful after upgrading kanban to pick up
new or updated skills (e.g. setup-hooks).

  kanban re-init
  kanban re-init --harness pi
  kanban re-init --harness claude --plan plan.md

Supported harnesses: pi, claude, generic. Prompts if omitted.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			return bootstrap.ReInit(bootstrap.InitOptions{
				Dir:      dir,
				DBPath:   cfg.DBPath,
				Harness:  bootstrap.Harness(harness),
				PlanPath: plan,
			})
		},
	}
	cmd.Flags().StringVar(&harness, "harness", "", "agent harness (pi, claude, generic; prompts if empty)")
	cmd.Flags().StringVar(&plan, "plan", "", "path to plan.md or plan.json for task dispatch")
	cmd.Flags().StringVar(&dir, "dir", ".", "project root directory")
	return cmd
}
