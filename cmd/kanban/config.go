package main

import (
	"context"
	"os"
)

type Config struct {
	DBPath string
	Debug  bool
}

type ctxKey struct{}

var configKey = ctxKey{}

func resolveConfig(dbPath string, debug bool) Config {
	if dbPath == ".kanban/kanban.db" {
		if env := os.Getenv("KANBAN_DB"); env != "" {
			dbPath = env
		}
	}
	return Config{DBPath: dbPath, Debug: debug}
}

func contextWithConfig(ctx context.Context, cfg Config) context.Context {
	return context.WithValue(ctx, configKey, cfg)
}

func ConfigFromContext(ctx context.Context) Config {
	return ctx.Value(configKey).(Config)
}
