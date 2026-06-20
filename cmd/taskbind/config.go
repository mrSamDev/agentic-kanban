package main

import (
	"context"
	"os"
	"path/filepath"
)

type Config struct {
	DBPath string
	Debug  bool
}

type ctxKey struct{}

var configKey = ctxKey{}

func resolveConfig(dbPath string, debug bool) Config {
	if dbPath == ".taskbind/taskbind.db" {
		if env := os.Getenv("TASKBIND_DB"); env != "" {
			dbPath = env
		} else if found := findProjectRoot(); found != "" {
			dbPath = found
		}
	}
	return Config{DBPath: dbPath, Debug: debug}
}

// findProjectRoot walks up from CWD looking for a .taskbind directory.
// Returns the path to taskbind.db inside the first .taskbind/ found, or empty string.
func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".taskbind")); err == nil {
			return filepath.Join(dir, ".taskbind", "taskbind.db")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func contextWithConfig(ctx context.Context, cfg Config) context.Context {
	return context.WithValue(ctx, configKey, cfg)
}

func ConfigFromContext(ctx context.Context) Config {
	return ctx.Value(configKey).(Config)
}
