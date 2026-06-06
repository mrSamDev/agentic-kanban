package task

import (
	"database/sql"
	"fmt"
	"time"
)

// --- Error types with exit-code support ---

// ExitError carries an exit code distinct from generic errors (exit 1).
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string { return e.Message }

// ErrNotFound is returned when a task ID doesn't exist.
var ErrNotFound = &ExitError{Code: 2, Message: "task not found"}

// ErrInvalidState is returned for illegal status transitions.
var ErrInvalidState = &ExitError{Code: 2, Message: "invalid state transition"}

// ErrNotAssigned is returned when the agent doesn't own the lease.
var ErrNotAssigned = &ExitError{Code: 2, Message: "task not assigned to this agent"}

// Service holds the DB handle and exposes intent-based operations.
type Service struct {
	db *sql.DB
}

// NewService creates a Service backed by the given *sql.DB.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// --- Helpers ---

// nextID generates TASK-<next> inside a write transaction.
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

// scanTask scans a single Task row from a Scanner.
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
