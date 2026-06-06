package task

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// eventPayload enriches events with title/project/priority so hooks have
// self-sufficient data without a follow-up DB lookup.
func eventPayload(tx *sql.Tx, taskID string, extra map[string]string) map[string]string {
	p := map[string]string{"task_id": taskID}
	var title, project string
	var priority int
	if err := tx.QueryRow(
		`SELECT title, project, priority FROM tasks WHERE id = ?`, taskID,
	).Scan(&title, &project, &priority); err == nil {
		p["title"] = title
		p["project"] = project
		p["priority"] = fmt.Sprintf("%d", priority)
	}
	for k, v := range extra {
		p[k] = v
	}
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
