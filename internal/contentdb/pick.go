package contentdb

import (
	"cmp"
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
	Style      string // drill | context ("" counts as context)
}

// Candidates lists the published questions of one quest (world + skill) that suit the child's grade and that
// the child has never been served, in ANY version (the no-repeat rule works on the bank id).
func (r *Repo) Candidates(ctx context.Context, q Querier, childID, world, skill string, grade int) ([]Candidate, error) {
	return r.CandidatesWindow(ctx, q, childID, world, skill, 0, grade)
}

// CandidatesWindow is Candidates for the school years lo..hi (grade-true play uses [grade-1, grade]).
func (r *Repo) CandidatesWindow(ctx context.Context, q Querier, childID, world, skill string, lo, hi int) ([]Candidate, error) {
	rows, err := q.QueryContext(ctx, `SELECT b.id, b.version, b.type, b.grade, b.difficulty, COALESCE(json_extract(b.content_json, '$.style'), '') FROM bank_questions b
		WHERE b.world_id = ? AND b.skill_id = ? AND b.status = 'published' AND b.grade BETWEEN ? AND ?
		  AND NOT EXISTS (SELECT 1 FROM questions sq JOIN child_questions cq ON cq.question_id = sq.id
		                  WHERE sq.bank_id = b.id AND cq.child_id = ?)
		ORDER BY b.id`, world, skill, lo, hi, childID)
	if err != nil {
		return nil, fmt.Errorf("list candidates: %w", err)
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.ID, &c.Version, &c.Type, &c.Grade, &c.Difficulty, &c.Style); err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		c.Style = content.DerivedStyle(skill, c.Grade, c.Style) // an untagged drill-skill question still follows the drill rule
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

// Shares of grade-true picking, as percentages decided by a hash of the child and the pick number, so a reload
// never changes a pick.
const (
	reviewSharePct = 15 // picks that revisit the school year before the child's
	drillSharePct  = 10 // grade 3 picks that may still be a drill
)

func pct(childID, label string, n int) int {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", childID, label, n)))
	return int(binary.BigEndian.Uint64(sum[:8]) % 100)
}

// RankGrade is the grade-true picker: from grade 3 on a drill is dropped (a grade 3 child still gets 10 percent of
// them), 15 percent of picks revisit the year before the child's when such questions exist, and the order is
// [same type as the last beat, grade below the child's, distance to the target difficulty, stable hash].
// pickNo is the number of questions this child was served before (any stable counter works).
func RankGrade(cands []Candidate, childID, avoidType string, target, childGrade, pickNo int) (Candidate, bool) {
	return rankGrade(cands, childID, avoidType, target, childGrade, pickNo, false)
}

// RankGradeStrict is RankGrade without the last resort: from grade 3 on, when only drills are left (and the drill
// share does not apply) it finds nothing, so the caller can look in lower years or re-serve a question instead.
func RankGradeStrict(cands []Candidate, childID, avoidType string, target, childGrade, pickNo int) (Candidate, bool) {
	return rankGrade(cands, childID, avoidType, target, childGrade, pickNo, true)
}

func rankGrade(cands []Candidate, childID, avoidType string, target, childGrade, pickNo int, strict bool) (Candidate, bool) {
	if above := slices.DeleteFunc(slices.Clone(cands), func(c Candidate) bool { return c.Grade > childGrade }); len(above) > 0 {
		cands = above // never serve a harder school year than the child's (the SQL window already holds this)
	}
	if childGrade >= 3 && !(childGrade == 3 && pct(childID, "drill", pickNo) < drillSharePct) {
		if ctx := slices.DeleteFunc(slices.Clone(cands), func(c Candidate) bool { return c.Style == "drill" }); len(ctx) > 0 {
			cands = ctx // only when nothing else exists does a drill stay (the content gate reports it)
		} else if strict {
			return Candidate{}, false
		}
	}
	if pct(childID, "review", pickNo) < reviewSharePct {
		if below := slices.DeleteFunc(slices.Clone(cands), func(c Candidate) bool { return c.Grade >= childGrade }); len(below) > 0 {
			cands = below
		}
	}
	abs := func(n int) int { return max(n, -n) }
	if len(cands) == 0 {
		return Candidate{}, false
	}
	best := slices.MinFunc(cands, func(a, b Candidate) int {
		keys := func(c Candidate) [3]int {
			same, below := 0, 0
			if avoidType != "" && c.Type == avoidType {
				same = 1
			}
			if c.Grade < childGrade {
				below = 1
			}
			return [3]int{same, below, abs(c.Difficulty - target)}
		}
		ka, kb := keys(a), keys(b)
		for i := range ka {
			if ka[i] != kb[i] {
				return ka[i] - kb[i]
			}
		}
		return cmp.Compare(tieBreak(childID, a.ID), tieBreak(childID, b.ID))
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
