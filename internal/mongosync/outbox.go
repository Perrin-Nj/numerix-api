package mongosync

import (
	"context"
	"database/sql"
	"fmt"
)

// Outbox writes "this key changed" rows. A nil *Outbox or a disabled one does nothing, so game code can call
// it without checking whether Mongo is configured (Spring: a null-object bean).
type Outbox struct{ enabled bool }

// NewOutbox builds the writer; enabled is false when MONGODB_URI is empty (no rows, no growth).
func NewOutbox(enabled bool) *Outbox { return &Outbox{enabled: enabled} }

// Enabled tells whether events are recorded.
func (o *Outbox) Enabled() bool { return o != nil && o.enabled }

// Enqueue inserts one row using the CALLER'S transaction tx. It never opens its own: if the caller rolls back,
// the row disappears with the game change; if it commits, both are saved. payload is JSON text ("{}" if empty).
func (o *Outbox) Enqueue(ctx context.Context, tx *sql.Tx, kind, key, payload string) error {
	if !o.Enabled() {
		return nil
	}
	if payload == "" {
		payload = "{}"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO outbox (kind, key, payload) VALUES (?, ?, ?)`, kind, key, payload); err != nil {
		return fmt.Errorf("enqueue %s event: %w", kind, err) // wrap with %w so callers can still use errors.Is
	}
	return nil
}
