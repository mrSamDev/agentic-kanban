package main

import (
	"context"
	"fmt"
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

			updated, err := s.BatchUpdatePriority(context.Background(), idList, priority)
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

			updated, err := s.BatchUpdateProject(context.Background(), idList, project)
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
