package contentdb

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perrin-Nj/numerix-api/internal/store"
)

// T031: the grade-window candidate query must not scan the whole bank_questions table.
func TestCandidatesWindowUsesAnIndex(t *testing.T) {
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.R.Query(`EXPLAIN QUERY PLAN SELECT b.id, b.version, b.type, b.grade, b.difficulty, COALESCE(json_extract(b.content_json, '$.style'), '') FROM bank_questions b
		WHERE b.world_id = ? AND b.skill_id = ? AND b.status = 'published' AND b.grade BETWEEN ? AND ?
		  AND NOT EXISTS (SELECT 1 FROM questions sq JOIN child_questions cq ON cq.question_id = sq.id
		                  WHERE sq.bank_id = b.id AND cq.child_id = ?)
		ORDER BY b.id`, "w", "s", 1, 2, "c")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	t.Log(strings.Join(plan, "\n"))
	for _, d := range plan {
		if strings.HasPrefix(d, "SCAN b") || strings.HasPrefix(d, "SCAN bank_questions") {
			t.Fatalf("full scan of bank_questions: %v (add idx_bank_pick to the migration)", plan)
		}
	}
}
