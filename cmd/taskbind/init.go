package main

import (
	"path/filepath"

	"taskbind/internal/bootstrap"

	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	var harness, plan, dir string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a project for agent coordination",
		Long: `Scaffold a project with taskbind database, skill files, and optional task dispatch from a plan file.

Creates .taskbind/taskbind.db, agent skill directories, and optionally parses
--plan to dispatch the first batch of tasks.

  taskbind init
  taskbind init --harness pi
  taskbind init --harness pi --plan plan.md
  taskbind init --harness claude --plan plan.md --dir ./my-project

Supported harnesses: pi (default), claude, generic.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = ConfigFromContext(cmd.Context()) // init is project-scoped; ignore TASKBIND_DB / findProjectRoot
			return bootstrap.Init(bootstrap.InitOptions{
				Dir:      dir,
				DBPath:   filepath.Join(dir, ".taskbind", "taskbind.db"),
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

The database is left untouched. Useful after upgrading taskbind to pick up
new or updated skills (e.g. setup-hooks).

  taskbind re-init
  taskbind re-init --harness pi
  taskbind re-init --harness claude --plan plan.md

Supported harnesses: pi, claude, generic. Prompts if omitted.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = ConfigFromContext(cmd.Context()) // re-init is project-scoped; ignore TASKBIND_DB / findProjectRoot
			return bootstrap.ReInit(bootstrap.InitOptions{
				Dir:      dir,
				DBPath:   filepath.Join(dir, ".taskbind", "taskbind.db"),
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
