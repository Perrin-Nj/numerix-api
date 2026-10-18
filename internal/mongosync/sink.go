// Package mongosync copies a few aggregates from SQLite to MongoDB through a transactional outbox.
//
// Go for Spring people:
//   - SQLite stays the source of truth. Mongo only receives personal bests, the family board and anonymised
//     progress stats. Nothing here is ever read back to serve a request.
//   - The outbox pattern: the game code inserts a small row in the SAME transaction as its own change
//     (Spring: an @Transactional method that also saves an "event" entity). A background goroutine
//     (Spring: a @Scheduled bean) later sends them. At-least-once delivery, idempotent upserts.
//   - An interface is satisfied implicitly: any type with the right methods is a Sink, no "implements".
package mongosync

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// Event kinds, one per Mongo collection.
const (
	KindBests  = "personal_bests"
	KindFamily = "family_board"
	KindStats  = "progress_stats"
)

// MinChildren is the k-anonymity floor: a stats bucket with fewer children is never sent.
const MinChildren = 5

// Op is one idempotent write for the sink: replace the document with this Key, or delete it.
type Op struct {
	Kind   string         // one of the Kind constants (selects the collection)
	Key    string         // unique key inside the collection (a pseudonym, never a real id)
	Doc    map[string]any // the document body; the sink adds "key" itself
	Delete bool           // true: remove the document instead of writing it
	// DeleteBy, with Delete, removes EVERY document whose field DeleteBy equals Key (used to erase a whole
	// family, all weeks). Empty = delete the one document whose "key" equals Key.
	DeleteBy string
}

// Sink is where Ops go (Spring: a repository interface). Defined here, where it is used.
type Sink interface {
	Upsert(ctx context.Context, op Op) error
	Bulk(ctx context.Context, ops []Op) error // many ops in one round trip; replaying the same ops is safe
	Close(ctx context.Context) error
}

// Disabled is the sink used when MONGODB_URI is not set: it accepts and forgets everything.
type Disabled struct{}

func (Disabled) Upsert(context.Context, Op) error { return nil }
func (Disabled) Bulk(context.Context, []Op) error { return nil }
func (Disabled) Close(context.Context) error      { return nil }

// ErrSkip is returned by a Resolver when an event needs no write (for example the family has no children).
var ErrSkip = errors.New("mongosync: nothing to send")

// Pseudonymizer turns a real id into a stable pseudonym that cannot be reversed without the secret.
type Pseudonymizer struct{ secret []byte }

// NewPseudonymizer keeps the server secret (MONGO_PSEUDO_SECRET).
func NewPseudonymizer(secret string) *Pseudonymizer { return &Pseudonymizer{secret: []byte(secret)} }

// ID returns HMAC-SHA256(secret, domain + ":" + id) in hex. The domain keeps a child pseudonym and a family
// pseudonym different even if two ids were ever equal. Same input and secret always give the same output.
func (p *Pseudonymizer) ID(domain, id string) string {
	m := hmac.New(sha256.New, p.secret) // HMAC = keyed hash: without the key nobody can recompute or test guesses
	m.Write([]byte(domain + ":" + id))
	return hex.EncodeToString(m.Sum(nil))
}
