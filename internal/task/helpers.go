package task

import (
	"database/sql"
	"fmt"
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
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// 15 min lease: enough for a typical autonomous step, short enough to auto-reclaim hung tasks.
const defaultLeaseMinutes = 15

// Length limits prevent CLI overflow and keep task metadata concise.
const (
	maxTitleLength  = 500
	maxNoteLength   = 10000
	maxReasonLength = 1000
)



// Prefix "TASK-" for human-readable IDs in logs and CLI output.
// Caller must already hold a write transaction.
func nextID(tx *sql.Tx) (string, error) {
	var max int
	err := tx.QueryRow(
		"SELECT COALESCE(MAX(CAST(SUBSTR(id,6) AS INTEGER)), 0) FROM tasks",
	).Scan(&max)
	if err != nil {
		return "", fmt.Errorf("next id: %w", err)
	}
	return fmt.Sprintf("TASK-%d", max+1), nil
}

// Time parsing handles both RFC3339 (JSON) and SQLite's default datetime format.
func scanTask(scanner interface {
	Scan(dest ...any) error
}) (Task, error) {
	var t Task
	var assigned, lease, project sql.NullString
	var createdAt, updatedAt time.Time
	err := scanner.Scan(
		&t.ID, &t.Title, (*string)(&t.Status),
		&t.RoleBoundary, &project, &t.Priority,
		&assigned, &lease,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return t, err
	}
	t.Project = project.String
	t.AssignedAgent = NullableStringFromDB(sql.NullString{String: assigned.String, Valid: assigned.Valid})
	if lease.Valid {
		parsed, err := time.Parse(time.RFC3339, lease.String)
		if err != nil {
			parsed, err = time.Parse("2006-01-02 15:04:05", lease.String)
		}
		if err == nil {
			t.LeaseUntil = &parsed
		}
	}
	t.CreatedAt = createdAt
	t.UpdatedAt = updatedAt
	return t, nil
}
