package contentdb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/Perrin-Nj/numerix-api/internal/content"
)

// Candidate is a published bank question a child has not been served yet.
type Candidate struct {
	ID         string
	Version    int
	Type       string
	Grade      int
	Difficulty int
}

// Candidates lists the published questions of one quest (world + skill) that suit the child's grade and that
// the child has never been served, in ANY version (the no-repeat rule works on the bank id).
func (r *Repo) Candidates(ctx context.Context, q Querier, childID, world, skill string, grade int) ([]Candidate, error) {
	rows, err := q.QueryContext(ctx, `SELECT b.id, b.version, b.type, b.grade, b.difficulty FROM bank_questions b
		WHERE b.world_id = ? AND b.skill_id = ? AND b.status = 'published' AND b.grade <= ?
		  AND NOT EXISTS (SELECT 1 FROM questions sq JOIN child_questions cq ON cq.question_id = sq.id
		                  WHERE sq.bank_id = b.id AND cq.child_id = ?)
		ORDER BY b.id`, world, skill, grade, childID)
	if err != nil {
		return nil, fmt.Errorf("list candidates: %w", err)
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.ID, &c.Version, &c.Type, &c.Grade, &c.Difficulty); err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// TargetDifficulty maps a mastery estimate (0 to 1) to a difficulty 1 to 5: a child who knows the skill
// better gets harder questions.
func TargetDifficulty(mastery float64) int {
	return min(5, max(1, 1+int(mastery*4+0.5)))
}

// tieBreak is a stable pseudo random number for (child, question): the same child always gets the same
// order, different children get different orders, and nothing depends on RANDOM().
func tieBreak(childID, questionID string) uint64 {
	sum := sha256.Sum256([]byte(childID + "|" + questionID))
	return binary.BigEndian.Uint64(sum[:8])
}

// Rank picks the best candidate: another type than the previous beat first, then the difficulty closest to
// the target, then the grade closest to the child's, then the stable hash. ok is false for an empty list.
func Rank(cands []Candidate, childID, avoidType string, target, childGrade int) (Candidate, bool) {
	if len(cands) == 0 {
		return Candidate{}, false
	}
	abs := func(n int) int {
		if n < 0 {
			return -n
		}
		return n
	}
	best := slices.MinFunc(cands, func(a, b Candidate) int {
		keys := func(c Candidate) [3]int {
			same := 0
			if avoidType != "" && c.Type == avoidType {
				same = 1
			}
			return [3]int{same, abs(c.Difficulty - target), abs(c.Grade - childGrade)}
		}
		ka, kb := keys(a), keys(b)
		for i := range ka {
			if ka[i] != kb[i] {
				return ka[i] - kb[i]
			}
		}
		ha, hb := tieBreak(childID, a.ID), tieBreak(childID, b.ID)
		switch {
		case ha < hb:
			return -1
		case ha > hb:
			return 1
		}
		return 0
	})
	return best, true
}

// LoadVersion returns the parsed question for a bank id and version, from the cache or the database.
// The returned value is shared with the cache: do not modify it.
func (r *Repo) LoadVersion(ctx context.Context, q Querier, id string, version int) (*content.Question, error) {
	if cached, ok := r.Cache.Get(id, version); ok {
		return cached, nil
	}
	var body string
	var cur int
	// the current row first (the common case), the history table for an older version
	err := q.QueryRowContext(ctx, `SELECT content_json, version FROM bank_questions WHERE id = ?`, id).Scan(&body, &cur)
	if err == nil && cur != version {
		err = q.QueryRowContext(ctx, `SELECT content_json FROM question_versions WHERE question_id = ? AND version = ?`, id, version).Scan(&body)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load question %s v%d: %w", id, version, err)
	}
	var parsed content.Question
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, fmt.Errorf("decode question %s v%d: %w", id, version, err)
	}
	r.Cache.Put(id, version, &parsed)
	return &parsed, nil
}
