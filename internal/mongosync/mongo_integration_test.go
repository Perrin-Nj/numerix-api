package mongosync

// Integration test against a REAL MongoDB. Skipped unless MONGODB_URI_TEST is set, for example an Atlas M0
// string or mongodb://localhost:27017. It uses a throw-away database that it drops at the end.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoSinkIntegration(t *testing.T) {
	uri := os.Getenv("MONGODB_URI_TEST")
	if uri == "" {
		t.Skip("MONGODB_URI_TEST not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	m, err := NewMongo(ctx, uri, fmt.Sprintf("numerix_test_%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = m.db.Drop(context.Background())
		_ = m.Close(context.Background())
	})
	n := func(coll string, filter bson.D) int64 {
		c, err := m.db.Collection(coll).CountDocuments(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	op := Op{Kind: KindBests, Key: "p1", Doc: map[string]any{"longest_streak_days": 3}}
	for i := 0; i < 2; i++ { // replay: idempotent
		if err := m.Bulk(ctx, []Op{op}); err != nil {
			t.Fatal(err)
		}
	}
	if got := n("personal_bests", bson.D{{Key: "key", Value: "p1"}}); got != 1 {
		t.Fatalf("replay left %d documents", got)
	}
	op.Doc = map[string]any{"longest_streak_days": 4}
	if err := m.Upsert(ctx, op); err != nil {
		t.Fatal(err)
	}
	if got := n("personal_bests", bson.D{{Key: "longest_streak_days", Value: 4}}); got != 1 {
		t.Fatalf("replace failed, %d docs with the new value", got)
	}
	// unique index exists
	if _, err := m.db.Collection("progress_stats").InsertMany(ctx, []any{bson.D{{Key: "key", Value: "x"}}, bson.D{{Key: "key", Value: "x"}}}); err == nil {
		t.Fatal("unique index on key missing")
	}
	// family erase removes every week
	for _, w := range []string{"2026-01-05", "2026-01-12"} {
		if err := m.Upsert(ctx, Op{Kind: KindFamily, Key: "f1|" + w, Doc: map[string]any{"family": "f1"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Upsert(ctx, Op{Kind: KindFamily, Key: "f1", Delete: true, DeleteBy: "family"}); err != nil {
		t.Fatal(err)
	}
	if got := n("family_board", bson.D{}); got != 0 {
		t.Fatalf("family erase left %d docs", got)
	}
	if err := m.Upsert(ctx, Op{Kind: KindBests, Key: "p1", Delete: true}); err != nil || n("personal_bests", bson.D{}) != 0 {
		t.Fatalf("delete failed: %v", err)
	}
	if err := m.Upsert(ctx, Op{Kind: "nope", Key: "x"}); err == nil {
		t.Fatal("unknown kind must be an error")
	}
}
