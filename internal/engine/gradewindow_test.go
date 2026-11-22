package engine

import (
	"testing"
	"time"
)

// bankWorld loads a graph where "echecs" is a written-questions world with one quest per skill given.
func bankWorld(t *testing.T, window bool, entries ...BankEntry) *Graph {
	t.Helper()
	gr := mustGraph(t)
	gr.SetBank(entries)
	gr.SetBankOnly("echecs")
	gr.SetGradeWindow(window)
	return gr
}

// A grade 1 child must not be offered a quest whose written questions are all grade 4 (T003, FR-010).
func TestGradeWindowClosesQuestWithOnlyHigherGrades(t *testing.T) {
	e := BankEntry{World: "echecs", Skill: "GEO-A01", Published: 12, ByGrade: map[int]int{4: 12}}
	other := BankEntry{World: "echecs", Skill: "ARI-OPE-A07a", Published: 12, ByGrade: map[int]int{1: 12}} // keeps grade 1 usable in the world
	if off := bankWorld(t, false, e, other); !off.PlayableFor("echecs", "GEO-A01", 1) {
		t.Fatal("flag off keeps today's behaviour: bank-only quests open for every year")
	}
	on := bankWorld(t, true, e, other)
	if on.PlayableFor("echecs", "GEO-A01", 1) {
		t.Error("flag on: grade 1 child must not play a grade 4 only quest")
	}
	if !on.PlayableFor("echecs", "GEO-A01", 4) {
		t.Error("flag on: grade 4 child plays it")
	}
}

// The window is [grade-1, grade]: a grade 4 child counts grade 3 and 4 questions, not grade 2.
func TestGradeWindowCountsOnlyTheTwoYears(t *testing.T) {
	e := BankEntry{World: "echecs", Skill: "GEO-A01", Published: 16, ByGrade: map[int]int{2: 8, 5: 8}}
	gr := bankWorld(t, true, e, BankEntry{World: "echecs", Skill: "ARI-OPE-A07a", Published: 12, ByGrade: map[int]int{4: 12}})
	if gr.PlayableFor("echecs", "GEO-A01", 4) {
		t.Error("grade 4 window is 3..4: grade 2 and grade 5 questions do not count")
	}
	if !gr.PlayableFor("echecs", "GEO-A01", 3) { // window 2..3
		t.Error("grade 3 window is 2..3 and holds 8 questions")
	}
}

// A new grade 4 child gets the skill whose introduction year is closest to grade 4, not the earliest year (FR-010).
func TestPlannerClosestYearForNewSkill(t *testing.T) {
	now := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	for _, window := range []bool{false, true} {
		gr := mustGraph(t)
		gr.SetGradeWindow(window)
		sel, err := Choose(gr, NewProfile("kid", gr), "nature", 4, NewRNG(1), now)
		if err != nil {
			t.Fatal(err)
		}
		s, _ := gr.Get(sel.SkillID)
		if window && s.YearIntro < 3 {
			t.Errorf("flag on: grade 4 child got %s introduced in year %d", s.ID, s.YearIntro)
		}
		if !window && s.YearIntro > 2 {
			t.Errorf("flag off must keep the earliest year first, got year %d", s.YearIntro)
		}
	}
}

// A child with no history in a bank-only world and grade 1 must never be sent to a skill with no grade 1 or 0 question.
func TestPlannerBankOnlyDoesNotBypassYear(t *testing.T) {
	now := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	gr := bankWorld(t, true,
		BankEntry{World: "echecs", Skill: "GEO-A01", Published: 12, ByGrade: map[int]int{4: 12}},
		BankEntry{World: "echecs", Skill: "ARI-OPE-A07a", Published: 12, ByGrade: map[int]int{1: 12}})
	sel, err := Choose(gr, NewProfile("kid", gr), "echecs", 1, NewRNG(1), now)
	if err != nil || sel.SkillID != "ARI-OPE-A07a" {
		t.Fatalf("selection = %+v, %v", sel, err)
	}
}

func TestGradePriorTable(t *testing.T) {
	gr := mustGraph(t)
	intro := func(id string) int { s, _ := gr.Get(id); return s.YearIntro }
	id := "ARI-OPE-A02a"
	y := intro(id)
	for _, c := range []struct {
		grade int
		want  float64
	}{{y, 0.25}, {y - 1, 0.25}, {y + 1, 0.40}, {y + 2, 0.55}, {y + 5, 0.6}} {
		if got := gr.GradePrior(id, c.grade, 0.25, 0.15); got < c.want-1e-9 || got > c.want+1e-9 {
			t.Errorf("grade %d (intro %d): %.2f, want %.2f", c.grade, y, got, c.want)
		}
	}
	if got := gr.GradePrior("unknown", 4, 0.25, 0.15); got != 0.25 {
		t.Errorf("unknown skill must give the base, got %v", got)
	}
}

// A world with nothing in the window uses the lower years, never a higher one: a grade 1 child is not offered a quest
// that only has grade 4 questions (the world is then unplayable at grade 1, which is reported as a data gap).
func TestGradeWindowFallsBackToLowerYearsNeverHigher(t *testing.T) {
	gr := bankWorld(t, true, BankEntry{World: "echecs", Skill: "GEO-A01", Published: 12, ByGrade: map[int]int{4: 12}})
	if gr.WindowFor("echecs", 1) {
		t.Fatal("no quest has grade 0 or 1 questions: the window must not apply")
	}
	if gr.PlayableFor("echecs", "GEO-A01", 1) {
		t.Error("grade 4 content must never be offered to a grade 1 child")
	}
	low := bankWorld(t, true, BankEntry{World: "echecs", Skill: "GEO-A01", Published: 12, ByGrade: map[int]int{1: 12}})
	if low.WindowFor("echecs", 4) || !low.PlayableFor("echecs", "GEO-A01", 4) {
		t.Error("a grade 4 child with only grade 1 content falls back to the lower year")
	}
	if !gr.WindowFor("echecs", 4) {
		t.Error("a grade 4 child has content in the window")
	}
}
