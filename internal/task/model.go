package task

import (
	"database/sql"
	"encoding/json"
	"time"
)

// TaskStatus represents the lifecycle states.
type TaskStatus string

const (
	StatusTODO       TaskStatus = "TODO"
	StatusInProgress TaskStatus = "IN_PROGRESS"
	StatusBlocked    TaskStatus = "BLOCKED"
	StatusInReview   TaskStatus = "IN_REVIEW"
	StatusDone       TaskStatus = "DONE"
)

// Task maps to the tasks table.
type Task struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Status        TaskStatus `json:"status"`
	RoleBoundary  string     `json:"role_boundary"`
	Project       string     `json:"project"`
	Priority      int        `json:"priority"`
	AssignedAgent *string    `json:"assigned_agent"` // nullable
	LeaseUntil    *time.Time `json:"lease_until"`    // nullable
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DependsOn     *string    `json:"depends_on"`     // comma-separated dependency IDs, nullable
	ClaimedBy     *string    `json:"claimed_by"`     // immutable snapshot of claiming agent, nullable
}

// Note maps to the notes table.
type Note struct {
	ID        int64     `json:"id"`
	TaskID    string    `json:"task_id"`
	Author    string    `json:"author"`
	NoteType  *string   `json:"note_type"` // nullable
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// HistoryEntry maps to the history table.
type HistoryEntry struct {
	ID        int64     `json:"id"`
	TaskID    string    `json:"task_id"`
	Agent     string    `json:"agent"`
	Action    string    `json:"action"`
	CreatedAt time.Time `json:"created_at"`
}

// TaskDetail is the full view returned by View: task + notes + history.
type TaskDetail struct {
	Task    Task           `json:"task"`
	Notes   []Note         `json:"notes"`
	History []HistoryEntry `json:"history"`
}

// Event maps to the events table.
type Event struct {
	ID        int64           `json:"id"`
	CreatedAt time.Time       `json:"created_at"`
	EventType string          `json:"event"`
	Payload   json.RawMessage `json:"payload"`
}

// NullableTimeFromDB converts a *sql.NullTime (from database/sql) to *time.Time.
func NullableTimeFromDB(nt sql.NullTime) *time.Time {
	if nt.Valid {
		return &nt.Time
	}
	return nil
}

// NullableStringFromDB converts a *sql.NullString to *string.
func NullableStringFromDB(ns sql.NullString) *string {
	if ns.Valid {
		return &ns.String
	}
	return nil
}
