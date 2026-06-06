package task

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
	var assigned, lease sql.NullString
	var createdAt, updatedAt time.Time
	err := scanner.Scan(
		&t.ID, &t.Title, (*string)(&t.Status),
		&t.RoleBoundary, &t.Priority,
		&assigned, &lease,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return t, err
	}
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

// --- Command implementations ---

// Dispatch inserts a new TODO task and returns it.
func (s *Service) Dispatch(title, roleBoundary string, priority int) (Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Task{}, fmt.Errorf("dispatch begin tx: %w", err)
	}
	defer tx.Rollback()

	id, err := nextID(tx)
	if err != nil {
		return Task{}, err
	}

	_, err = tx.Exec(
		`INSERT INTO tasks (id, title, status, role_boundary, priority)
		 VALUES (?, ?, 'TODO', ?, ?)`,
		id, title, roleBoundary, priority,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert task: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO history (task_id, agent, action) VALUES (?, 'system', 'DISPATCH')`,
		id,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit dispatch: %w", err)
	}

	return s.View(id)
}

// ClaimNext atomically claims the highest-priority available task for a role.
// Returns empty Task with nil error when no work is available (exit 0, JSON {}).
func (s *Service) ClaimNext(agent, role string) (Task, error) {
	// Serializable isolation → BEGIN IMMEDIATE → write lock up front.
	// Required: two concurrent claimers must never get the same task.
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Task{}, fmt.Errorf("claim begin immediate: %w", err)
	}
	defer tx.Rollback()

	row := tx.QueryRow(
		`UPDATE tasks
		   SET status         = 'IN_PROGRESS',
		       assigned_agent = ?,
		       lease_until    = datetime('now', '+15 minutes'),
		       updated_at     = CURRENT_TIMESTAMP
		 WHERE id = (
		   SELECT id FROM tasks
		    WHERE (role_boundary = ? AND (status = 'TODO'
		           OR (status = 'IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP)))
		       OR (status = 'IN_REVIEW' AND ? = 'reviewer')
		    ORDER BY
		      CASE
		        WHEN status = 'IN_REVIEW' THEN 0  -- reviewer tasks first
		        ELSE 1
		      END,
		      priority ASC, created_at ASC
		    LIMIT 1
		 )
		 RETURNING *`,
		agent, role, role,
	)

	t, err := scanTask(row)
	if err != nil {
		if err == sql.ErrNoRows {
			// No work available — empty result.
			tx.Rollback()
			return Task{}, nil
		}
		return Task{}, fmt.Errorf("claim update: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'CLAIM')`,
		t.ID, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert claim history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit claim: %w", err)
	}

	return t, nil
}

// View returns the full task detail (task + notes + history) for the given ID.
func (s *Service) View(id string) (Task, error) {
	row := s.db.QueryRow(
		`SELECT id, title, status, role_boundary, priority,
		        assigned_agent, lease_until, created_at, updated_at
		   FROM tasks WHERE id = ?`, id,
	)
	t, err := scanTask(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return Task{}, ErrNotFound
		}
		return Task{}, fmt.Errorf("view task: %w", err)
	}
	return t, nil
}

// ViewDetail returns the full task detail including notes and history.
func (s *Service) ViewDetail(id string) (TaskDetail, error) {
	t, err := s.View(id)
	if err != nil {
		return TaskDetail{}, err
	}

	notes, err := s.listNotes(id)
	if err != nil {
		return TaskDetail{}, err
	}

	history, err := s.listHistory(id)
	if err != nil {
		return TaskDetail{}, err
	}

	return TaskDetail{Task: t, Notes: notes, History: history}, nil
}

func (s *Service) listNotes(taskID string) ([]Note, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, author, note_type, content, created_at
		   FROM notes WHERE task_id = ? ORDER BY id`, taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var n Note
		var noteType sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&n.ID, &n.TaskID, &n.Author, &noteType, &n.Content, &createdAt); err != nil {
			return nil, err
		}
		n.NoteType = NullableStringFromDB(noteType)
		n.CreatedAt = createdAt
		notes = append(notes, n)
	}
	if notes == nil {
		notes = []Note{}
	}
	return notes, rows.Err()
}

func (s *Service) listHistory(taskID string) ([]HistoryEntry, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, agent, action, created_at
		   FROM history WHERE task_id = ? ORDER BY id`, taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []HistoryEntry
	for rows.Next() {
		var h HistoryEntry
		var createdAt time.Time
		if err := rows.Scan(&h.ID, &h.TaskID, &h.Agent, &h.Action, &createdAt); err != nil {
			return nil, err
		}
		h.CreatedAt = createdAt
		history = append(history, h)
	}
	if history == nil {
		history = []HistoryEntry{}
	}
	return history, rows.Err()
}

// Complete transitions a task to DONE (or IN_REVIEW) and clears the lease.
// Uses a single UPDATE with WHERE guards so the check+update is atomic without
// BEGIN IMMEDIATE — the write lock is acquired at first write, and SQLite
// serializes at commit time.
func (s *Service) Complete(id, agent string, toReview bool) (Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Task{}, fmt.Errorf("complete begin tx: %w", err)
	}
	defer tx.Rollback()

	newStatus := StatusDone
	action := "COMPLETE"
	if toReview {
		newStatus = StatusInReview
		action = "REVIEW"
	}

	res, err := tx.Exec(
		`UPDATE tasks
		    SET status = ?, assigned_agent = NULL, lease_until = NULL,
		        updated_at = CURRENT_TIMESTAMP
		  WHERE id = ?
		    AND assigned_agent = ?
		    AND status IN ('IN_PROGRESS', 'IN_REVIEW')`,
		string(newStatus), id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("complete update: %w", err)
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		// Determine why: does the task exist?
		var exists bool
		tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
		if !exists {
			return Task{}, ErrNotFound
		}
		// Existing task — must be wrong agent or wrong state.
		return Task{}, ErrNotAssigned
	}

	_, err = tx.Exec(
		`INSERT INTO history (task_id, agent, action) VALUES (?, ?, ?)`,
		id, agent, action,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert complete history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit complete: %w", err)
	}

	return s.View(id)
}

// LogProgress appends a note and renews the lease (heartbeat).
func (s *Service) LogProgress(id, agent, content string, noteType string) (Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Task{}, fmt.Errorf("log begin tx: %w", err)
	}
	defer tx.Rollback()

	// Verify ownership via UPDATE condition — atomic with the lease renewal.
	res, err := tx.Exec(
		`UPDATE tasks
		    SET lease_until = datetime('now', '+15 minutes'),
		        updated_at = CURRENT_TIMESTAMP
		  WHERE id = ? AND assigned_agent = ?`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("renew lease: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists bool
		tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
		if !exists {
			return Task{}, ErrNotFound
		}
		return Task{}, ErrNotAssigned
	}

	var nt *string
	if noteType != "" {
		nt = &noteType
	}
	_, err = tx.Exec(
		`INSERT INTO notes (task_id, author, note_type, content) VALUES (?, ?, ?, ?)`,
		id, agent, nt, content,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert note: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'PROGRESS')`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert progress history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit log: %w", err)
	}

	return s.View(id)
}

// Block transitions a task to BLOCKED, clears the lease, and records the reason.
func (s *Service) Block(id, agent, reason string) (Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Task{}, fmt.Errorf("block begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE tasks
		    SET status = 'BLOCKED', assigned_agent = NULL, lease_until = NULL,
		        updated_at = CURRENT_TIMESTAMP
		  WHERE id = ?
		    AND assigned_agent = ?
		    AND status IN ('IN_PROGRESS', 'IN_REVIEW')`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("block update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists bool
		tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
		if !exists {
			return Task{}, ErrNotFound
		}
		return Task{}, ErrNotAssigned
	}

	_, err = tx.Exec(
		`INSERT INTO notes (task_id, author, note_type, content) VALUES (?, ?, 'BLOCKED', ?)`,
		id, agent, reason,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert block note: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'BLOCK')`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert block history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit block: %w", err)
	}

	return s.View(id)
}

// SearchParams defines optional filters for Search.
type SearchParams struct {
	Status  TaskStatus
	Role    string
	Agent   string
	Limit   int
	Offset  int
}

// Search returns tasks matching the given filters.
func (s *Service) Search(params SearchParams) ([]Task, error) {
	var conditions []string
	var args []any

	if params.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, string(params.Status))
	}
	if params.Role != "" {
		conditions = append(conditions, "role_boundary = ?")
		args = append(args, params.Role)
	}
	if params.Agent != "" {
		conditions = append(conditions, "assigned_agent = ?")
		args = append(args, params.Agent)
	}

	query := "SELECT id, title, status, role_boundary, priority, assigned_agent, lease_until, created_at, updated_at FROM tasks"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY priority ASC, created_at ASC"

	if params.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", params.Limit)
	}
	if params.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", params.Offset)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if tasks == nil {
		tasks = []Task{}
	}
	return tasks, rows.Err()
}

// ReviewApprove transitions an IN_REVIEW or IN_PROGRESS (claimed by reviewer) task to DONE.
func (s *Service) ReviewApprove(id, agent string) (Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Task{}, fmt.Errorf("review begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE tasks
		    SET status = 'DONE', assigned_agent = NULL, lease_until = NULL,
		        updated_at = CURRENT_TIMESTAMP
		  WHERE id = ? AND (status = 'IN_REVIEW' OR (status = 'IN_PROGRESS' AND assigned_agent = ?))`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("review update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists bool
		tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
		if !exists {
			return Task{}, ErrNotFound
		}
		return Task{}, ErrInvalidState
	}

	_, err = tx.Exec(
		`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'REVIEW')`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert review history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit review: %w", err)
	}

	return s.View(id)
}

// ReviewReject sends an IN_REVIEW task back to TODO, clearing the lease.
func (s *Service) ReviewReject(id, agent, reason string) (Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Task{}, fmt.Errorf("reject begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE tasks
		    SET status = 'TODO', assigned_agent = NULL, lease_until = NULL,
		        updated_at = CURRENT_TIMESTAMP
		  WHERE id = ? AND (status = 'IN_REVIEW' OR (status = 'IN_PROGRESS' AND assigned_agent = ?))`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("reject update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists bool
		tx.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, id).Scan(&exists)
		if !exists {
			return Task{}, ErrNotFound
		}
		return Task{}, ErrInvalidState
	}

	_, err = tx.Exec(
		`INSERT INTO notes (task_id, author, note_type, content) VALUES (?, ?, 'REJECTED', ?)`,
		id, agent, reason,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert reject note: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO history (task_id, agent, action) VALUES (?, ?, 'REVIEW')`,
		id, agent,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert reject history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("commit reject: %w", err)
	}

	return s.View(id)
}