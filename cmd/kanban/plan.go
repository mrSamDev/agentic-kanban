package main

import (
	"fmt"
	"os"

	"agent-kanban/internal/storage"
	"agent-kanban/internal/task"

	"github.com/spf13/cobra"
)

func planCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Plan management commands",
	}
	cmd.AddCommand(lintCmd())
	return cmd
}

func lintCmd() *cobra.Command {
	var asJSON bool
	var project string

	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Check the board for structural problems (unknown deps, cycles, missing roles)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			db, err := storage.Open(cfg.DBPath, cfg.Debug)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			issues, err := task.LintPlan(cmd.Context(), db.Reader(), project)
			if err != nil {
				return err
			}

			if asJSON {
				writeJSON(issues)
				return exitCodeForIssues(issues)
			}

			printLintIssues(issues)
			return exitCodeForIssues(issues)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	cmd.Flags().StringVar(&project, "project", "", "limit to a specific project/scope")
	return cmd
}

func printLintIssues(issues []task.LintIssue) {
	if len(issues) == 0 {
		fmt.Println("No issues found.")
		return
	}

	errors, warns := 0, 0
	for _, iss := range issues {
		sev := "WARN"
		if iss.Severity == "error" {
			sev = "ERROR"
			errors++
		} else {
			warns++
		}
		_, _ = fmt.Fprintf(os.Stdout, "%-5s %-10s %s\n", sev, iss.TaskID, iss.Message)
	}

	fmt.Printf("\n%d issue(s) found (%d error(s), %d warning(s))\n", len(issues), errors, warns)
}

// exitCodeForIssues returns a non-nil ExitError (code 1) when any errors are present.
// Warnings alone exit 0.
func exitCodeForIssues(issues []task.LintIssue) error {
	for _, iss := range issues {
		if iss.Severity == "error" {
			return &task.ExitError{Code: 1, Message: ""}
		}
	}
	return nil
}
