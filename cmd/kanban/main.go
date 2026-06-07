package main

import (
	"fmt"
	"os"
	"sync"

	"agent-kanban/internal/task"

	"github.com/spf13/cobra"
)

var (
	version   = "0.1.10"
	checkOnce sync.Once
)

func main() {
	var dbPath string
	var debug bool

	rootCmd := &cobra.Command{
		Use:   "kanban",
		Short: "Agent coordination engine — shared task state for cooperating agents",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cfg := resolveConfig(dbPath, debug)
			cmd.SetContext(contextWithConfig(cmd.Context(), cfg))
			checkOnce.Do(autoVersionCheck)
			return nil
		},
	}

	rootCmd.PersistentFlags().StringVar(&dbPath, "db", ".kanban/kanban.db", "path to SQLite database (or $KANBAN_DB to override)")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug logging")

	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true

	rootCmd.AddCommand(taskCmd())
	rootCmd.AddCommand(skillCmd())
	rootCmd.AddCommand(eventsCmd())
	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(reInitCmd())
	rootCmd.AddCommand(upgradeCmd())
	rootCmd.AddCommand(pruneCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(planCmd())

	if err := rootCmd.Execute(); err != nil {
		if exitErr, ok := err.(*task.ExitError); ok {
			writeStderr(exitErr.Message)
			os.Exit(exitErr.Code)
		}
		writeStderr(err.Error())
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println("kanban " + version)
			if check {
				versionCheck()
			}
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "check for newer version")
	return cmd
}
