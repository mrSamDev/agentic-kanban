package main

import (
	"strings"

	"agent-kanban/internal/task"

	"github.com/spf13/cobra"
)

var validNoteTypes = map[string]bool{
	"PROGRESS": true,
	"ERROR":    true,
	"DECISION": true,
	"":         true,
}

func nonEmpty(s string, label string) error {
	if strings.TrimSpace(s) == "" {
		return &task.ExitError{Code: 2, Message: label + " cannot be empty"}
	}
	return nil
}

func taskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage tasks",
	}
	cmd.AddCommand(
		dispatchCmd(),
		claimCmd(),
		claimNextCmd(),
		viewCmd(),
		completeCmd(),
		extendLeaseCmd(),
		logProgressCmd(),
		blockCmd(),
		searchCmd(),
		approveCmd(),
		rejectCmd(),
		statsCmd(),
		batchCmd(),
	)
	return cmd
}
