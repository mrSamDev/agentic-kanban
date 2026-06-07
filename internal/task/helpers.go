package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"time"
)

type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string { return e.Message }

var ErrNotFound = &ExitError{Code: 2, Message: "task not found"}

var ErrInvalidState = &ExitError{Code: 2, Message: "invalid state transition"}

var ErrNotAssigned = &ExitError{Code: 2, Message: "task not assigned to this agent"}

type Service struct {
	db          *sql.DB
	timeout     time.Duration
	maxRetries  int
	retryBaseMs int
	hooksDir    string
}

func (s *Service) SetHooksDir(dir string) { s.hooksDir = dir }

func NewService(db *sql.DB, timeout time.Duration) *Service {
	return &Service{
		db:          db,
		timeout:     timeout,
		maxRetries:  3,
		retryBaseMs: 100,
	}
}

const defaultTimeout = 30 * time.Second

func (s *Service) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	t := s.timeout
	if t <= 0 {
		t = defaultTimeout
	}
	return context.WithTimeout(ctx, t)
}

// sqliteError matches modernc.org/sqlite.Error without importing the driver.
type sqliteError interface {
	Code() int
	error
}

func isSQLiteBusy(err error) bool {
	var se sqliteError
	if errors.As(err, &se) {
		return se.Code() == 5 || se.Code() == 6
	}
	return false
}

func (s *Service) retryOnBusy(fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < s.maxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		if !isSQLiteBusy(err) {
			return err
		}
		lastErr = err
		if attempt < s.maxRetries-1 {
			base := s.retryBaseMs * (1 << attempt)
			jitter := int(float64(base) * 0.3 * (rand.Float64()*2 - 1))
			sleep := base + jitter
			if sleep < 1 {
				sleep = 1
			}
			time.Sleep(time.Duration(sleep) * time.Millisecond)
		}
	}
	return lastErr
}

// 15 min lease: enough for a typical autonomous step, short enough to auto-reclaim hung tasks.
const defaultLeaseMinutes = 15

// Length limits prevent CLI overflow and keep task metadata concise.
const (
	maxTitleLength  = 500
	maxNoteLength   = 10000
	maxReasonLength = 1000
	defaultViewLimit = 20
)

// Prefix "TASK-" for human-readable IDs in logs and CLI output.
// Caller must already hold a write transaction.
func nextID(tx *sql.Tx) (string, error) {
	var id int
	err := tx.QueryRow(
		"UPDATE task_seq SET next_id = next_id + 1 RETURNING next_id",
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("next id: %w", err)
	}
	return fmt.Sprintf("TASK-%d", id), nil
}

// parseLeaseTime handles both RFC3339 (JSON) and SQLite's default datetime format.
func parseLeaseTime(s string) *time.Time {
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		parsed, err = time.Parse("2006-01-02 15:04:05", s)
	}
	if err == nil {
		return &parsed
	}
	return nil
}

func scanTask(scanner interface {
	Scan(dest ...any) error
}) (Task, error) {
	var t Task
	var assigned, lease, project, dependsOn sql.NullString
	var createdAt, updatedAt time.Time
	err := scanner.Scan(
		&t.ID, &t.Title, (*string)(&t.Status),
		&t.RoleBoundary, &project, &t.Priority,
		&assigned, &lease,
		&createdAt, &updatedAt,
		&dependsOn,
	)
	if err != nil {
		return t, err
	}
	t.Project = project.String
	t.AssignedAgent = NullableStringFromDB(assigned)
	t.DependsOn = NullableStringFromDB(dependsOn)
	t.CreatedAt = createdAt
	t.UpdatedAt = updatedAt
	if lease.Valid {
		t.LeaseUntil = parseLeaseTime(lease.String)
	}
	return t, nil
}
