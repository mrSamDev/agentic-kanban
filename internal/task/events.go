package task

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

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

	// Auto-cleanup: delete expired events in the same transaction.
	// Uses the index on (ttl_seconds, created_at) for efficiency.
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
