package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"agent-kanban/internal/bootstrap"
	"agent-kanban/internal/storage"
	"agent-kanban/internal/task"

	"github.com/spf13/cobra"
)

var dbPath string
var debug bool

var version = "0.1.4"

var validNoteTypes = map[string]bool{
	"PROGRESS": true,
	"ERROR":    true,
	"DECISION": true,
	"":         true, // optional
}

func nonEmpty(s string, label string) error {
	if strings.TrimSpace(s) == "" {
		return &task.ExitError{Code: 2, Message: label + " cannot be empty"}
	}
	return nil
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "kanban",
		Short: "Agent coordination engine — shared task state for cooperating agents",
	}

	rootCmd.PersistentFlags().StringVar(&dbPath, "db", ".kanban/kanban.db", "path to SQLite database (or $KANBAN_DB to override)")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug logging")

	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true

	rootCmd.AddCommand(taskCmd())
	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(initCmd())

	if err := rootCmd.Execute(); err != nil {
		// ExitError carries exit code 2; generic errors exit 1.
		if exitErr, ok := err.(*task.ExitError); ok {
			writeStderr(exitErr.Message)
			os.Exit(exitErr.Code)
		}
		writeStderr(err.Error())
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println("kanban " + version)
		},
	}
}

func taskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage tasks",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			// Resolve db path from env if flag is default.
			if dbPath == ".kanban/kanban.db" {
				if env := os.Getenv("KANBAN_DB"); env != "" {
					dbPath = env
				}
			}
			return nil
		},
	}
	cmd.AddCommand(
		dispatchCmd(),
		claimNextCmd(),
		viewCmd(),
		completeCmd(),
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

func openService() (*task.Service, func(), error) {
	db, err := storage.Open(dbPath, debug)
	if err != nil {
		return nil, nil, err
	}
	s := task.NewService(db.DB, 0)
	return s, func() { db.Close() }, nil
}

func writeJSON(v any) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		writeStderr(fmt.Sprintf("marshal error: %v", err))
		os.Exit(1)
	}
	fmt.Println(string(out))
}

func writeStderr(msg string) {
	// JSON is the primary format; fall back to plain text on encode error
	// (os.Stderr.Write failure is deliberately ignored — nothing useful to do).
	if err := json.NewEncoder(os.Stderr).Encode(map[string]string{"error": msg}); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, msg)
	}
}

// --- init command ---

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
		RunE: func(_ *cobra.Command, _ []string) error {
			return bootstrap.Init(bootstrap.InitOptions{
				Dir:      dir,
				DBPath:   dbPath,
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

// --- subcommands ---

func dispatchCmd() *cobra.Command {
	var title, role, project string
	var priority int

	cmd := &cobra.Command{
		Use:   "dispatch",
		Short: "Create a new task",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, close, err := openService()
			if err != nil {
				return err
			}
			defer close()

			t, err := s.Dispatch(context.Background(), title, role, project, priority)
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
		RunE: func(_ *cobra.Command, _ []string) error {
			s, close, err := openService()
			if err != nil {
				return err
			}
			defer close()

			t, err := s.ClaimNext(context.Background(), agent, role, project)
			if err != nil {
				return err
			}
			// Empty task (no work) → output {}.
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

func viewCmd() *cobra.Command {
	var noteLimit, historyLimit int

	cmd := &cobra.Command{
		Use:   "view <id>",
		Short: "View task details with notes and history",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, close, err := openService()
			if err != nil {
				return err
			}
			defer close()

			detail, err := s.ViewDetail(context.Background(), args[0], noteLimit, historyLimit)
			if err != nil {
				return err
			}
			writeJSON(detail)
			return nil
		},
	}
	cmd.Flags().IntVar(&noteLimit, "notes", 0, "max notes to return (0 = all)")
	cmd.Flags().IntVar(&historyLimit, "history", 0, "max history entries to return (0 = all)")
	return cmd
}

func completeCmd() *cobra.Command {
	var agent string
	var toReview bool

	cmd := &cobra.Command{
		Use:   "complete <id>",
		Short: "Mark a task as done (or submit for review)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, close, err := openService()
			if err != nil {
				return err
			}
			defer close()

			t, err := s.Complete(context.Background(), args[0], agent, toReview)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().BoolVar(&toReview, "review", false, "submit for review instead of completing")
	cmd.MarkFlagRequired("agent")
	return cmd
}

func logProgressCmd() *cobra.Command {
	var agent, note, noteType string

	cmd := &cobra.Command{
		Use:   "log-progress <id>",
		Short: "Log progress and renew lease",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, close, err := openService()
			if err != nil {
				return err
			}
			defer close()

			t, err := s.LogProgress(context.Background(), args[0], agent, note, noteType)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().StringVar(&note, "note", "", "progress note (required)")
	cmd.Flags().StringVar(&noteType, "type", "", "note type (PROGRESS|ERROR|DECISION)")
	cmd.MarkFlagRequired("agent")
	cmd.MarkFlagRequired("note")
	cmd.PreRunE = func(_ *cobra.Command, _ []string) error {
		if noteType != "" && !validNoteTypes[noteType] {
			return &task.ExitError{Code: 2, Message: "note type must be PROGRESS, ERROR, or DECISION"}
		}
		return nil
	}
	return cmd
}

func blockCmd() *cobra.Command {
	var agent, reason string

	cmd := &cobra.Command{
		Use:   "block <id>",
		Short: "Block a task with a reason",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, close, err := openService()
			if err != nil {
				return err
			}
			defer close()

			t, err := s.Block(context.Background(), args[0], agent, reason)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "block reason (required)")
	cmd.MarkFlagRequired("agent")
	cmd.MarkFlagRequired("reason")
	return cmd
}

func searchCmd() *cobra.Command {
	var status, role, agent, project string
	var limit int

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search tasks by filters",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, close, err := openService()
			if err != nil {
				return err
			}
			defer close()

			params := task.SearchParams{
				Status:  task.TaskStatus(status),
				Role:    role,
				Agent:   agent,
				Project: project,
				Limit:   limit,
			}

			tasks, err := s.Search(context.Background(), params)
			if err != nil {
				return err
			}
			writeJSON(tasks)
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "filter by status")
	cmd.Flags().StringVar(&role, "role", "", "filter by role boundary")
	cmd.Flags().StringVar(&agent, "agent", "", "filter by assigned agent")
	cmd.Flags().StringVar(&project, "project", "", "filter by project/scope")
	cmd.Flags().IntVar(&limit, "limit", 0, "max results")
	return cmd
}

func approveCmd() *cobra.Command {
	var agent string

	cmd := &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve a task in review, mark as done",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, close, err := openService()
			if err != nil {
				return err
			}
			defer close()

			t, err := s.ReviewApprove(context.Background(), args[0], agent)
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

func rejectCmd() *cobra.Command {
	var agent, reason string

	cmd := &cobra.Command{
		Use:   "reject <id>",
		Short: "Reject a task in review, send back to TODO",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, close, err := openService()
			if err != nil {
				return err
			}
			defer close()

			t, err := s.ReviewReject(context.Background(), args[0], agent, reason)
			if err != nil {
				return err
			}
			writeJSON(t)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "rejection reason (required)")
	cmd.MarkFlagRequired("agent")
	cmd.MarkFlagRequired("reason")
	return cmd
}

func statsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show aggregate task statistics",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, close, err := openService()
			if err != nil {
				return err
			}
			defer close()

			stats, err := s.Stats(context.Background())
			if err != nil {
				return err
			}
			writeJSON(stats)
			return nil
		},
	}
	return cmd
}

func batchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Batch operations on multiple tasks",
	}
	cmd.AddCommand(
		batchPriorityCmd(),
		batchProjectCmd(),
	)
	return cmd
}

func batchPriorityCmd() *cobra.Command {
	var ids string
	var priority int

	cmd := &cobra.Command{
		Use:   "set-priority",
		Short: "Set priority for multiple tasks",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, close, err := openService()
			if err != nil {
				return err
			}
			defer close()

			idList := strings.Split(ids, ",")
			for i := range idList {
				idList[i] = strings.TrimSpace(idList[i])
			}

			updated, err := s.BatchUpdatePriority(idList, priority)
			if err != nil {
				return err
			}
			fmt.Printf("Updated %d tasks to priority %d\n", updated, priority)
			return nil
		},
	}
	cmd.Flags().StringVar(&ids, "ids", "", "comma-separated task IDs (required)")
	cmd.Flags().IntVar(&priority, "priority", 100, "new priority (lower = more urgent)")
	cmd.MarkFlagRequired("ids")
	cmd.MarkFlagRequired("priority")
	return cmd
}

func batchProjectCmd() *cobra.Command {
	var ids string
	var project string

	cmd := &cobra.Command{
		Use:   "set-project",
		Short: "Set project label for multiple tasks",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, close, err := openService()
			if err != nil {
				return err
			}
			defer close()

			idList := strings.Split(ids, ",")
			for i := range idList {
				idList[i] = strings.TrimSpace(idList[i])
			}

			updated, err := s.BatchUpdateProject(idList, project)
			if err != nil {
				return err
			}
			fmt.Printf("Updated %d tasks to project '%s'\n", updated, project)
			return nil
		},
	}
	cmd.Flags().StringVar(&ids, "ids", "", "comma-separated task IDs (required)")
	cmd.Flags().StringVar(&project, "project", "", "project/scope label (required)")
	cmd.MarkFlagRequired("ids")
	cmd.MarkFlagRequired("project")
	return cmd
}
