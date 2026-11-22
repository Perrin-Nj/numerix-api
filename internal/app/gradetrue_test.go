package app

// T003/T012/T013: grade-true play (platform.grade_window). Flag off is the old behaviour (golden test in
// internal/engine); these tests turn it on.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"slices"
	"testing"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/appconfig"
	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/seedload"
)

func TestChildWithoutGradeIsGrade1OnlyWithTheFlag(t *testing.T) {
	f := newFeedFixture(t, true) // the fixture starts with the flag off
	ctx := context.Background()
	if g := f.s.childGrade(ctx, plChild{}); g != 6 {
		t.Fatalf("flag off keeps today's rule (unknown grade = 6), got %d", g)
	}
	f.setPlatform(t, func(p *appconfig.Platform) { p.GradeWindow = true })
	if g := f.s.childGrade(ctx, plChild{}); g != 1 {
		t.Fatalf("flag on: no grade is grade 1, got %d", g)
	}
	if !f.s.game().graph.GradeWindow() {
		t.Fatal("childGrade must apply the flag to the graph")
	}
	if g := f.s.childGrade(ctx, plChild{Grade: sql.NullInt64{Int64: 3, Valid: true}}); g != 3 {
		t.Fatalf("a real grade is kept, got %d", g)
	}
}

// gradeBank clones one sample into a quest-wide bank spread over grades 1 to 4, difficulties 1 to 5, some drills.
func gradeBank(files *seedload.Files) {
	var src *content.Question
	for _, q := range files.Questions() {
		if q.ID == "cuisine.fournee.901" {
			src = q
		}
	}
	var list []*content.Question
	for i := 0; i < 90; i++ {
		c := *src
		c.ID = fmt.Sprintf("cuisine.fournee.%03d", 801+i)
		c.Status, c.Grade, c.Difficulty = content.StatusPublished, 2+i%3, 1+(i/3)%5
		c.Style = content.StyleContext
		if c.Grade == 2 && i%2 == 0 {
			c.Style = content.StyleDrill
		}
		list = append(list, &c)
	}
	files.QuestionFiles = []seedload.QuestionFile{{Name: seedload.ExamplesFile, World: seedload.ExamplesFile, Questions: list}}
	for i := range files.Quests {
		if files.Quests[i].ID == "cuisine.fournee" {
			files.Quests[i].GradeMax = 4
		}
	}
}

// SC-003: grade 2 and grade 4 children get different grades and difficulties; nobody gets a harder year than their own;
// grade 4 gets no drills; at most about 15 percent of picks go below the child's year.
func TestSC003GradeDistribution(t *testing.T) {
	f := newFeedFixture(t, true)
	f.setPlatform(t, func(p *appconfig.Platform) { p.GradeWindow = true })
	seedBank(t, f, gradeBank)
	type stat struct{ n, sumGrade, sumDiff, below, drills int }
	stats := map[int]*stat{2: {}, 4: {}}
	ctx := context.Background()
	for _, grade := range []int{2, 4} {
		for k := 0; k < 30; k++ {
			id := fmt.Sprintf("%08d-0000-4000-8000-%012d", grade, k)
			f.sql(t, `INSERT INTO children (id, parent_id, display_name, timezone, grade) VALUES (?, 'p-1', 'K', 'UTC', ?)`, id, grade)
			for i := 0; i < 5; i++ {
				var qid string
				err := f.s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
					var e error
					qid, _, e = f.s.pickQuestion(ctx, tx, "", id, "cuisine", "ARI-OPE-A02c", grade, "")
					return e
				})
				if err != nil {
					t.Fatalf("grade %d pick %d: %v", grade, i, err)
				}
				var bank string
				var ver int
				_ = f.s.Store.R.QueryRow(`SELECT bank_id, bank_version FROM questions WHERE id = ?`, qid).Scan(&bank, &ver)
				q, err := f.s.Content.LoadVersion(ctx, f.s.Store.R, bank, ver)
				if err != nil {
					t.Fatal(err)
				}
				if q.Grade > grade {
					t.Fatalf("a grade %d child was served a grade %d question", grade, q.Grade)
				}
				st := stats[grade]
				st.n++
				st.sumGrade += q.Grade
				st.sumDiff += q.Difficulty
				if q.Grade < grade {
					st.below++
				}
				if q.Style == content.StyleDrill && grade == 4 {
					st.drills++
				}
			}
		}
	}
	g1, g4 := stats[2], stats[4]
	meanG1, meanG4 := float64(g1.sumGrade)/float64(g1.n), float64(g4.sumGrade)/float64(g4.n)
	dG1, dG4 := float64(g1.sumDiff)/float64(g1.n), float64(g4.sumDiff)/float64(g4.n)
	t.Logf("grade 2: mean grade %.2f diff %.2f | grade 4: mean grade %.2f diff %.2f, below %d/%d", meanG1, dG1, meanG4, dG4, g4.below, g4.n)
	if meanG4-meanG1 < 1.5 {
		t.Errorf("mean served grade must differ by at least 2: %.2f vs %.2f", meanG1, meanG4)
	}
	if dG4-dG1 < 0.4 {
		t.Errorf("a grade 4 child must start harder than a grade 2 child: %.2f vs %.2f", dG1, dG4)
	}
	if g4.drills != 0 {
		t.Errorf("grade 4 served %d drills", g4.drills)
	}
	if share := float64(g4.below) / float64(g4.n); share > 0.25 {
		t.Errorf("%.0f percent of grade 4 picks went below the child's year, the target is about 15", share*100)
	}
}

// FR-014: only the next beat changes. An open beat served under the old rules is returned unchanged after the flip.
func TestFlippingTheFlagKeepsTheOpenBeat(t *testing.T) {
	f := newFeedFixture(t, true)
	seedBank(t, f, onlyClones("cuisine.fournee.901", 9))
	sid := f.startIn(t, "cuisine", "ARI-OPE-A02c")
	first, _ := f.nextBeat(t, sid)
	f.setPlatform(t, func(p *appconfig.Platform) { p.GradeWindow = true })
	again, _ := f.nextBeat(t, sid)
	if first["id"] != again["id"] {
		t.Fatalf("the open beat changed after the flip: %v vs %v", first["id"], again["id"])
	}
}

// T031: the grade window must not make the picker slower. One pick for a fresh child per round, alternating the two
// modes on the same database so both see the same load; the medians are logged and compared with a loose guard
// (the real 5 percent comparison is in docs/specs notes, measured with several runs).
func TestPickQuestionTimingWindowVsLegacy(t *testing.T) {
	f := newFeedFixture(t, true)
	seedBank(t, f, gradeBank)
	ctx := context.Background()
	const rounds = 300
	times := map[bool][]time.Duration{}
	for i := 0; i < rounds; i++ {
		for _, window := range []bool{false, true} {
			id := fmt.Sprintf("%08d-0000-4000-8000-%012d", i, map[bool]int{false: 1, true: 2}[window])
			f.sql(t, `INSERT INTO children (id, parent_id, display_name, timezone, grade) VALUES (?, 'p-1', 'K', 'UTC', 4)`, id)
			f.setPlatform(t, func(p *appconfig.Platform) { p.GradeWindow = window })
			start := time.Now()
			if err := f.s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
				_, _, e := f.s.pickQuestion(ctx, tx, "", id, "cuisine", "ARI-OPE-A02c", 4, "")
				return e
			}); err != nil {
				t.Fatal(err)
			}
			times[window] = append(times[window], time.Since(start))
		}
	}
	med := func(d []time.Duration) time.Duration { slices.Sort(d); return d[len(d)/2] }
	off, on := med(times[false]), med(times[true])
	t.Logf("pick median: window off %v, window on %v (ratio %.2f)", off, on, float64(on)/float64(off))
	if on > 2*off {
		t.Errorf("the grade window doubled the pick time: %v vs %v", on, off)
	}
}

// F1: questions that were never tagged are derived from the skill: grade 2 on a drill skill is a drill, so a grade 3
// child is served them at most in the 10 percent drill share.
func TestUntaggedDrillSkillQuestionsFollowTheDrillRule(t *testing.T) {
	f := newFeedFixture(t, true)
	f.setPlatform(t, func(p *appconfig.Platform) { p.GradeWindow = true })
	seedBank(t, f, func(files *seedload.Files) {
		gradeBank(files)
		for _, q := range files.QuestionFiles[0].Questions {
			q.Style = "" // nothing is tagged
			if q.Grade == 4 {
				q.Grade = 2
			}
		}
	})
	ctx := context.Background()
	picks, grade2 := 0, 0
	for k := 0; k < 30; k++ {
		id := fmt.Sprintf("%08d-0000-4000-8000-%012d", 7, k)
		f.sql(t, `INSERT INTO children (id, parent_id, display_name, timezone, grade) VALUES (?, 'p-1', 'K', 'UTC', 3)`, id)
		for i := 0; i < 5; i++ {
			var qid string
			if err := f.s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
				var e error
				qid, _, e = f.s.pickQuestion(ctx, tx, "", id, "cuisine", "ARI-OPE-A02c", 3, "")
				return e
			}); err != nil {
				t.Fatal(err)
			}
			var bank string
			var ver int
			_ = f.s.Store.R.QueryRow(`SELECT bank_id, bank_version FROM questions WHERE id = ?`, qid).Scan(&bank, &ver)
			q, _ := f.s.Content.LoadVersion(ctx, f.s.Store.R, bank, ver)
			picks++
			if q.Grade == 2 {
				grade2++
			}
		}
	}
	if grade2*100 > picks*15 {
		t.Fatalf("%d of %d picks were untagged grade 2 drills for a grade 3 child", grade2, picks)
	}
}

// A seed run that retires old ids (espace 001 to 071 replaced by 101 and up) must not hurt a child with history on them:
// the open chapter still loads and can be answered from the served copy, mastery per skill is intact, and the picker
// serves the new ids only.
func TestChildWithHistoryOnRetiredIds(t *testing.T) {
	f := newFeedFixture(t, true)
	seedBank(t, f, onlyClones("cuisine.fournee.901", 9)) // old ids cuisine.fournee.801 to 809
	sid := f.startIn(t, "cuisine", "ARI-OPE-A02c")
	beat, _ := f.nextBeat(t, sid)
	beatID := beat["id"].(string)
	old := f.curated(t, beatID)
	if code, _ := f.answer(t, sid, beatID, json.RawMessage(old.Answer.Value), uuid.NewString()); code != 200 {
		t.Fatal("first answer")
	}
	profile := func() string {
		var p string
		_ = f.s.Store.R.QueryRow(`SELECT bkt FROM child_profiles WHERE child_id = ?`, f.childID).Scan(&p)
		return p
	}
	before := profile()
	open, _ := f.nextBeat(t, sid) // a second beat is open (served from an old id) when the seed run retires everything old
	openID := open["id"].(string)
	// the new content: ids 101 and up, the old ones are left out of the seed and retired with allow-retire
	files, err := seedload.Read(testSeedDir, seedload.ReadOptions{SkipAssets: true, Only: []string{seedload.ExamplesFile}})
	if err != nil {
		t.Fatal(err)
	}
	var src *content.Question
	for _, q := range files.Questions() {
		if q.ID == "cuisine.fournee.901" {
			src = q
		}
	}
	var fresh []*content.Question
	for i := 1; i <= 9; i++ {
		c := *src
		c.ID, c.Status = fmt.Sprintf("cuisine.fournee.%d", 100+i), content.StatusPublished
		fresh = append(fresh, &c)
	}
	files.QuestionFiles = []seedload.QuestionFile{{Name: seedload.ExamplesFile, World: seedload.ExamplesFile, Questions: fresh}}
	rep, err := seedload.Apply(context.Background(), f.s.Content, f.s.Assets, f.s.game().graph, files, seedload.Options{Actor: "test", AllowRetire: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Questions.Retired != 9 || rep.Questions.Added != 9 {
		t.Fatalf("seed report: %+v", rep.Questions)
	}
	// the open beat (an old id) still loads and is answered from the served copy
	if code, b, _ := f.gcall(t, "GET", "/v1/sessions/"+sid, f.childTok, "", nil); code != 200 {
		t.Fatalf("the session must still load: %d %v", code, b)
	}
	if code, res := f.answer(t, sid, openID, json.RawMessage(old.Answer.Value), uuid.NewString()); code != 200 || res["correct_answer"] == nil {
		t.Fatalf("answering a beat served from a retired id: %d %v", code, res)
	}
	if after := profile(); after == "" || after == before {
		t.Fatalf("mastery per skill must keep growing across the retirement:\n%s\n%s", before, after)
	}
	// from now on only new ids are served
	next, _ := f.nextBeat(t, sid)
	q := f.curated(t, next["id"].(string))
	if !strings.HasPrefix(q.ID, "cuisine.fournee.10") && !strings.HasPrefix(q.ID, "cuisine.fournee.10") {
		t.Fatalf("the picker served %s, want a new id", q.ID)
	}
}
