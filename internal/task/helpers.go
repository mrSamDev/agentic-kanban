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

var ErrSelfReview = &ExitError{Code: 2, Message: "cannot review your own task \u2014 another agent must approve"}

type Service struct {
	db          *sql.DB
	timeout     time.Duration
	maxRetries  int
	retryBaseMs int
	hooksDir    string
}

func NewService(db *sql.DB, timeout time.Duration, hooksDir string) *Service {
	return &Service{
		db:          db,
		timeout:     timeout,
		maxRetries:  3,
		retryBaseMs: 100,
		hooksDir:    hooksDir,
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
const (
	defaultLeaseMinutes = 15
	maxCandidateFetch   = 100
)

// Length limits prevent CLI overflow and keep task metadata concise.
const (
	maxTitleLength   = 500
	maxNoteLength    = 10000
	maxReasonLength  = 1000
	defaultViewLimit = 20
)

// Prefix "TASK-" for human-readable IDs in logs and CLI output.
// Caller must already hold a write transaction.
func nextID(tx *sql.Tx) (string, error) {
	// task_seq is seeded from MAX(id) at DB open time (see storage.Open).
	// No reconcile needed here — just increment the counter inside the active
	// serializable transaction so concurrent dispatchers never collide.
	var id int
	err := tx.QueryRow(
		"UPDATE task_seq SET next_id = next_id + 1 WHERE id = 1 RETURNING next_id",
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("next id: %w", err)
	}
	return fmt.Sprintf("TASK-%d", id), nil
}

// parseLeaseTime handles both RFC3339 (JSON) and SQLite's default datetime format.
func parseLeaseTime(s string) (*time.Time, error) {
	// SQLite datetime format is the 99% case; RFC3339 is the rare case from JSON serialization.
	parsed, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, s)
	}
	if err != nil {
		return nil, fmt.Errorf("parse lease time %q: %w", s, err)
	}
	return &parsed, nil
}

func scanTask(scanner interface {
	Scan(dest ...any) error
}) (Task, error) {
	var t Task
	var assigned, lease, project, dependsOn, claimedBy sql.NullString
	var createdAt, updatedAt time.Time
	err := scanner.Scan(
		&t.ID, &t.Title, (*string)(&t.Status),
		&t.RoleBoundary, &project, &t.Priority,
		&assigned, &lease,
		&createdAt, &updatedAt,
		&dependsOn, &claimedBy,
	)
	if err != nil {
		return t, err
	}
	t.Project = project.String
	t.AssignedAgent = NullableStringFromDB(assigned)
	t.DependsOn = NullableStringFromDB(dependsOn)
	t.ClaimedBy = NullableStringFromDB(claimedBy)
	t.CreatedAt = createdAt
	t.UpdatedAt = updatedAt
	if lease.Valid {
		if lt, err := parseLeaseTime(lease.String); err != nil {
			return t, fmt.Errorf("scan task %s lease: %w", t.ID, err)
		} else {
			t.LeaseUntil = lt
		}
	}
	return t, nil
}

// alreadyDone checks whether a task is already at the target status.
// Used by BatchComplete to detect tasks completed by another writer between retries.
func alreadyDone(tx *sql.Tx, id, status string) bool {
	var current string
	if err := tx.QueryRow(`SELECT status FROM tasks WHERE id = ?`, id).Scan(&current); err != nil {
		return false
	}
	return current == status
}

// reRead fetches a full Task by ID inside an existing transaction.
func reRead(tx *sql.Tx, id string) (Task, error) {
	row := tx.QueryRow(
		`SELECT id, title, status, role_boundary, project, priority,
		        assigned_agent, lease_until, created_at, updated_at, depends_on, claimed_by
		   FROM tasks WHERE id = ?`, id,
	)
	return scanTask(row)
}
