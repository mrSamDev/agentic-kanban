package task

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// EventPayload carries typed event fields so hooks get self-sufficient data
// without re-parsing strings. Zero values are omitted from JSON.
type EventPayload struct {
	TaskID       string `json:"task_id"`
	Title        string `json:"title,omitempty"`
	Project      string `json:"project,omitempty"`
	Priority     string `json:"priority,omitempty"`
	Agent        string `json:"agent,omitempty"`
	NoteType     string `json:"note_type,omitempty"`
	Reason       string `json:"reason,omitempty"`
	RoleBoundary string `json:"role_boundary,omitempty"`
}

// eventPayload loads task metadata from DB and merges extra fields.
func eventPayload(tx *sql.Tx, taskID string, extra EventPayload) EventPayload {
	p := EventPayload{TaskID: taskID}
	var title, project string
	var priority int
	if err := tx.QueryRow(
		`SELECT title, project, priority FROM tasks WHERE id = ?`, taskID,
	).Scan(&title, &project, &priority); err == nil {
		p.Title = title
		p.Project = project
		p.Priority = fmt.Sprintf("%d", priority)
	}
	p.Agent = extra.Agent
	p.NoteType = extra.NoteType
	p.Reason = extra.Reason
	p.RoleBoundary = extra.RoleBoundary
	return p
}

// taskMeta holds the stable fields of a task needed for event payloads.
type taskMeta struct {
	Title    string
	Project  string
	Priority int
}

// loadTaskMetas fetches metadata for all given task IDs in a single query.
// Missing IDs are silently omitted from the returned map.
func loadTaskMetas(tx *sql.Tx, ids []string) (map[string]taskMeta, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("SELECT id, title, project, priority FROM tasks WHERE id IN (%s)", joinStrings(placeholders, ","))
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("load task metas: %w", err)
	}
	defer rows.Close()

	metas := make(map[string]taskMeta, len(ids))
	for rows.Next() {
		var id, title, project string
		var priority int
		if err := rows.Scan(&id, &title, &project, &priority); err != nil {
			return nil, fmt.Errorf("scan task meta: %w", err)
		}
		metas[id] = taskMeta{Title: title, Project: project, Priority: priority}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task metas: %w", err)
	}
	return metas, nil
}

// buildPayload constructs a payload from pre-loaded metadata. Extra fields
// may override meta values (e.g. batch update changes project/priority).
func buildPayload(metas map[string]taskMeta, id string, extra EventPayload) EventPayload {
	p := EventPayload{TaskID: id}
	if m, ok := metas[id]; ok {
		p.Title = m.Title
		p.Project = m.Project
		p.Priority = fmt.Sprintf("%d", m.Priority)
	}
	if extra.Project != "" {
		p.Project = extra.Project
	}
	if extra.Priority != "" {
		p.Priority = extra.Priority
	}
	p.Agent = extra.Agent
	p.NoteType = extra.NoteType
	p.Reason = extra.Reason
	p.RoleBoundary = extra.RoleBoundary
	return p
}

func insertEvent(tx *sql.Tx, eventType string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event %s payload: %w", eventType, err)
	}
	_, err = tx.Exec(
		`INSERT INTO events (event_type, payload) VALUES (?, ?)`,
		eventType, string(b),
	)
	if err != nil {
		return fmt.Errorf("insert event %s: %w", eventType, err)
	}

	_, err = tx.Exec(
		`DELETE FROM events
		  WHERE ttl_seconds IS NOT NULL
		    AND created_at < datetime('now', '-' || ttl_seconds || ' seconds')`,
	)
	if err != nil {
		return fmt.Errorf("cleanup expired events: %w", err)
	}

	return nil
}
