package main

import (
	"strings"

	"github.com/spf13/cobra"
)

func batchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Batch operations on multiple tasks",
	}
	cmd.AddCommand(
		batchPriorityCmd(),
		batchProjectCmd(),
		batchClaimCmd(),
		batchCompleteCmd(),
	)
	return cmd
}

func batchPriorityCmd() *cobra.Command {
	var ids string
	var priority int

	cmd := &cobra.Command{
		Use:   "set-priority",
		Short: "Set priority for multiple tasks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			idList := strings.Split(ids, ",")
			for i := range idList {
				idList[i] = strings.TrimSpace(idList[i])
			}

			updated, err := s.BatchUpdatePriority(cmd.Context(), idList, priority)
			if err != nil {
				return err
			}
			writeJSON(map[string]any{
				"updated":  updated,
				"priority": priority,
			})
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			idList := strings.Split(ids, ",")
			for i := range idList {
				idList[i] = strings.TrimSpace(idList[i])
			}

			updated, err := s.BatchUpdateProject(cmd.Context(), idList, project)
			if err != nil {
				return err
			}
			writeJSON(map[string]any{
				"updated": updated,
				"project": project,
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&ids, "ids", "", "comma-separated task IDs (required)")
	cmd.Flags().StringVar(&project, "project", "", "project/scope label (required)")
	cmd.MarkFlagRequired("ids")
	cmd.MarkFlagRequired("project")
	return cmd
}

func batchClaimCmd() *cobra.Command {
	var agent, role, project string
	var count int
	var respectDeps bool

	cmd := &cobra.Command{
		Use:   "claim",
		Short: "Claim multiple tasks atomically",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			tasks, err := s.ClaimBatch(cmd.Context(), agent, role, project, count, respectDeps)
			if err != nil {
				return err
			}
			writeJSON(tasks)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().StringVar(&role, "role", "", "role (required)")
	cmd.Flags().StringVar(&project, "project", "", "filter by project/scope")
	cmd.Flags().IntVar(&count, "count", 1, "number of tasks to claim")
	cmd.Flags().BoolVar(&respectDeps, "respect-deps", true, "skip tasks with unmet dependencies")
	cmd.MarkFlagRequired("agent")
	cmd.MarkFlagRequired("role")
	return cmd
}

func batchCompleteCmd() *cobra.Command {
	var ids, agent string
	var toReview bool

	cmd := &cobra.Command{
		Use:   "complete",
		Short: "Complete multiple tasks in one transaction",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			idList := strings.Split(ids, ",")
			for i := range idList {
				idList[i] = strings.TrimSpace(idList[i])
			}

			completed, errs := s.BatchComplete(cmd.Context(), idList, agent, toReview)
			result := map[string]any{
				"completed": completed,
			}
			if len(errs) > 0 {
				errStrs := make([]string, len(errs))
				for i, e := range errs {
					errStrs[i] = e.Error()
				}
				result["errors"] = errStrs
			}
			writeJSON(result)
			return nil
		},
	}
	cmd.Flags().StringVar(&ids, "ids", "", "comma-separated task IDs (required)")
	cmd.Flags().StringVar(&agent, "agent", "", "agent name (required)")
	cmd.Flags().BoolVar(&toReview, "to-review", false, "submit for review instead of completing")
	cmd.MarkFlagRequired("ids")
	cmd.MarkFlagRequired("agent")
	return cmd
}
