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
	s := task.NewService(db.DB, 0)
	s.SetHooksDir(filepath.Join(filepath.Dir(cfg.DBPath), "hooks"))
	return s, func() { db.Close() }, nil
}
