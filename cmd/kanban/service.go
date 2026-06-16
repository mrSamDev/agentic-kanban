package main

import (
	"path/filepath"
	"time"

	"agent-kanban/internal/storage"
	"agent-kanban/internal/task"
)

func openService(cfg Config) (*task.Service, func(), error) {
	db, err := storage.Open(cfg.DBPath, cfg.Debug)
	if err != nil {
		return nil, nil, err
	}
	hooksDir := filepath.Join(filepath.Dir(cfg.DBPath), "hooks")
	runner := task.NewHookRunner()
	s := task.NewService(db.DB, db.Reader(), 0, hooksDir, runner)
	return s, func() {
		// Wait for .d/ hook goroutines before closing DB.
		// wg.Wait() returns instantly when no hooks fired (counter=0).
		// 35s exceeds execHook's 30s context timeout.
		runner.Wait(35 * time.Second)
		_ = db.Close()
	}, nil
}
