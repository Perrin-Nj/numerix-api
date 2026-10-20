package app

// Performance guard (R3): the hot queries must use an index, and a benchmark times the answer path.
//
// Go for Spring people: a func named BenchmarkXxx(b *testing.B) is run by `go test -bench`; b.N is the loop
// count the framework picks (like a JMH benchmark). It does not run in the default test run.

import (
	"context"
	"strings"
	"testing"
)

// TestHotQueriesUseIndex fails when a hot query falls back to a full table scan.
func TestHotQueriesUseIndex(t *testing.T) {
	_, s := newTestServer(t)
	queries := []string{
		`SELECT question_id FROM beats WHERE id = 'x' AND session_id = 'y'`,
		`SELECT idempotency_key, json FROM answers WHERE beat_id = 'x' LIMIT 1`,
		`SELECT COUNT(*) FROM answers WHERE session_id = 'y' AND outcome = 'correct'`,
		`SELECT COALESCE(outcome,'') FROM answers WHERE session_id = 'y' ORDER BY created_at, rowid`,
		`SELECT b.id FROM beats b WHERE b.session_id = 'y' AND NOT EXISTS (SELECT 1 FROM answers a WHERE a.beat_id = b.id) ORDER BY b.idx DESC LIMIT 1`,
		`SELECT a.created_at FROM answers a JOIN sessions s ON s.id = a.session_id WHERE s.child_id = 'c' AND a.created_at >= 'a'`,
		`SELECT json FROM child_settings WHERE child_id = 'c'`,
	}
	for _, q := range queries {
		rows, err := s.Store.R.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(detail, "SCAN ") && !strings.Contains(detail, "USING") { // a bare SCAN reads the whole table
				t.Errorf("full scan in %q: %s", q, detail)
			}
		}
		rows.Close()
	}
}

// BenchmarkSceneKey times the new per-answer work (family lookup plus key build).
func BenchmarkSceneKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = sceneKeyFor("ARI-OPE-A06a", "erreur_typique", "MIS-X")
	}
}
