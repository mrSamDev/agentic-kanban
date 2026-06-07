package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (s *Service) View(ctx context.Context, id string) (Task, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, status, role_boundary, project, priority,
		        assigned_agent, lease_until, created_at, updated_at, depends_on
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

func (s *Service) ViewDetail(ctx context.Context, id string, noteLimit, historyLimit int) (TaskDetail, error) {
	t, err := s.View(ctx, id)
	if err != nil {
		return TaskDetail{}, err
	}

	if noteLimit <= 0 {
		noteLimit = defaultViewLimit
	}
	if historyLimit <= 0 {
		historyLimit = defaultViewLimit
	}

	notes, err := s.listNotes(ctx, id, noteLimit)
	if err != nil {
		return TaskDetail{}, err
	}

	history, err := s.listHistory(ctx, id, historyLimit)
	if err != nil {
		return TaskDetail{}, err
	}

	return TaskDetail{Task: t, Notes: notes, History: history}, nil
}

func (s *Service) listNotes(ctx context.Context, taskID string, limit int) ([]Note, error) {
	query := `SELECT id, task_id, author, note_type, content, created_at
		   FROM notes WHERE task_id = ? ORDER BY id`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query, taskID)
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

func (s *Service) listHistory(ctx context.Context, taskID string, limit int) ([]HistoryEntry, error) {
	query := `SELECT id, task_id, agent, action, created_at
		   FROM history WHERE task_id = ? ORDER BY id`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query, taskID)
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

func (s *Service) MaxEventID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM events`).Scan(&id)
	return id, err
}

func (s *Service) PollEvents(ctx context.Context, afterID int64) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, created_at, event_type, payload FROM events WHERE id > ? ORDER BY id ASC`,
		afterID,
	)
	if err != nil {
		return nil, fmt.Errorf("poll events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var payloadStr string
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.EventType, &payloadStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Payload = json.RawMessage(payloadStr)
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Service) ListEvents(ctx context.Context, limit int) ([]Event, error) {
	q := `SELECT id, created_at, event_type, payload FROM events ORDER BY id ASC`
	var args []any
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var payloadStr string
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.EventType, &payloadStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Payload = json.RawMessage(payloadStr)
		events = append(events, e)
	}
	if events == nil {
		events = []Event{}
	}
	return events, rows.Err()
}

type SearchParams struct {
	Status  TaskStatus
	Role    string
	Agent   string
	Project string
	Limit   int
	Offset  int
}

func (s *Service) Search(ctx context.Context, params SearchParams) ([]Task, error) {
	if params.Status != "" && !ValidStatuses[params.Status] {
		return nil, &ExitError{Code: 2, Message: fmt.Sprintf("invalid status: %q", params.Status)}
	}
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

	query := "SELECT id, title, status, role_boundary, project, priority, assigned_agent, lease_until, created_at, updated_at, depends_on FROM tasks"
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

	rows, err := s.db.QueryContext(ctx, query, args...)
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

type TaskStats struct {
	ByStatus      map[string]int `json:"by_status"`
	ByRole        map[string]int `json:"by_role"`
	ExpiredLeases int            `json:"expired_leases"`
	TotalTasks    int            `json:"total_tasks"`
}

type BurndownStats struct {
	ByStatus    map[string]int `json:"by_status"`
	ByRole      map[string]int `json:"by_role"`
	Total       int            `json:"total"`
	DoneCount   int            `json:"done_count"`
	PercentDone float64        `json:"percent_done"`
}

func (s *Service) Burndown(ctx context.Context) (BurndownStats, error) {
	raw, err := s.Stats(ctx)
	if err != nil {
		return BurndownStats{}, err
	}
	done := raw.ByStatus["DONE"]
	pct := 0.0
	if raw.TotalTasks > 0 {
		pct = float64(done) / float64(raw.TotalTasks) * 100
	}
	return BurndownStats{
		ByStatus:    raw.ByStatus,
		ByRole:      raw.ByRole,
		Total:       raw.TotalTasks,
		DoneCount:   done,
		PercentDone: pct,
	}, nil
}

func (s *Service) Stats(ctx context.Context) (TaskStats, error) {
	stats := TaskStats{
		ByStatus:   make(map[string]int),
		ByRole:     make(map[string]int),
		TotalTasks: 0,
	}

	rows, err := s.db.QueryContext(ctx, "SELECT status, COUNT(*) FROM tasks GROUP BY status")
	if err != nil {
		return stats, fmt.Errorf("stats by status: %w", err)
	}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			rows.Close()
			return stats, fmt.Errorf("scan status count: %w", err)
		}
		stats.ByStatus[status] = count
		stats.TotalTasks += count
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return stats, fmt.Errorf("status count: %w", err)
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, "SELECT role_boundary, COUNT(*) FROM tasks GROUP BY role_boundary")
	if err != nil {
		return stats, fmt.Errorf("stats by role: %w", err)
	}
	for rows.Next() {
		var role string
		var count int
		if err := rows.Scan(&role, &count); err != nil {
			rows.Close()
			return stats, fmt.Errorf("scan role count: %w", err)
		}
		stats.ByRole[role] = count
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return stats, fmt.Errorf("role count: %w", err)
	}
	rows.Close()

	row := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tasks WHERE status = 'IN_PROGRESS' AND lease_until < CURRENT_TIMESTAMP",
	)
	if err := row.Scan(&stats.ExpiredLeases); err != nil {
		return stats, fmt.Errorf("stats expired leases: %w", err)
	}

	return stats, nil
}
