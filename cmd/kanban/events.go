package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"
)

func eventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "View event log",
	}
	cmd.AddCommand(eventsListCmd(), eventsTailCmd())
	return cmd
}

func eventsListCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all events",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			events, err := s.ListEvents(context.Background(), limit)
			if err != nil {
				return err
			}
			writeJSON(events)
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "max events to return (0 = all)")
	return cmd
}

func eventsTailCmd() *cobra.Command {
	var fromStart bool
	var interval time.Duration
	cmd := &cobra.Command{
		Use:   "tail",
		Short: "Stream events as they arrive (NDJSON)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := ConfigFromContext(cmd.Context())
			s, close, err := openService(cfg)
			if err != nil {
				return err
			}
			defer close()

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()

			var cursor int64
			if !fromStart {
				cursor, err = s.MaxEventID(ctx)
				if err != nil {
					return err
				}
			}

			enc := json.NewEncoder(os.Stdout)
			for {
				events, err := s.PollEvents(ctx, cursor)
				if err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return err
				}
				for _, e := range events {
					if err := enc.Encode(e); err != nil {
						return err
					}
				}
				if len(events) > 0 {
					cursor = events[len(events)-1].ID
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(interval):
				}
			}
		},
	}
	cmd.Flags().BoolVar(&fromStart, "from-start", false, "replay all past events before following")
	cmd.Flags().DurationVar(&interval, "interval", 500*time.Millisecond, "poll interval")
	return cmd
}
