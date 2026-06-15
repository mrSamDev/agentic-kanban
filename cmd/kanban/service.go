package main

import (
	"path/filepath"

	"agent-kanban/internal/storage"
	"agent-kanban/internal/task"
)

func openService(cfg Config) (*task.Service, func(), error) {
	db, err := storage.Open(cfg.DBPath, cfg.Debug)
	if err != nil {
		return nil, nil, err
	}
	hooksDir := filepath.Join(filepath.Dir(cfg.DBPath), "hooks")
	s := task.NewService(db.DB, db.Reader(), 0, hooksDir)
	return s, func() { _ = db.Close() }, nil
}
