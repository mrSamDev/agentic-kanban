package task

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// View returns the task without notes or history.
func (s *Service) View(id string) (Task, error) {
	row := s.db.QueryRow(
		`SELECT id, title, status, role_boundary, project, priority,
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

// SearchParams defines optional filters for Search.
type SearchParams struct {
	Status  TaskStatus
	Role    string
	Agent   string
	Project string
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
	if params.Project != "" {
		conditions = append(conditions, "project = ?")
		args = append(args, params.Project)
	}

	query := "SELECT id, title, status, role_boundary, project, priority, assigned_agent, lease_until, created_at, updated_at FROM tasks"
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
