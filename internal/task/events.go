package task

import (
	"database/sql"
	"encoding/json"
	"log"
)

// insertEvent writes an event to the events table.
// Events are best-effort observability — marshal or insert failures are logged
// but do not abort the calling transaction.
func insertEvent(tx *sql.Tx, eventType string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("event marshal error (event=%s): %v", eventType, err)
		return
	}
	_, err = tx.Exec(
		`INSERT INTO events (event_type, payload) VALUES (?, ?)`,
		eventType, string(b),
	)
	if err != nil {
		log.Printf("event insert error (event=%s): %v", eventType, err)
	}
}
