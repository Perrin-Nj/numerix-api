package contentdb

import "testing"

func TestRankOldOrderIsTypeDifficultyThenGrade(t *testing.T) {
	cs := []Candidate{{ID: "a", Type: "numeric", Grade: 2, Difficulty: 3}, {ID: "b", Type: "numeric", Grade: 1, Difficulty: 3}, {ID: "c", Type: "numeric", Grade: 2, Difficulty: 5}}
	if got, _ := Rank(cs, "kid", "", 3, 2); got.ID != "a" {
		t.Errorf("same difficulty: the child's own year first, got %s", got.ID)
	}
	if got, _ := Rank(cs, "kid", "numeric", 5, 2); got.ID != "c" && got.ID != "a" && got.ID != "b" {
		t.Errorf("unexpected %s", got.ID)
	}
}

// First pickNo where neither the review share nor the drill share applies.
func plainPick(child string, grade int) int {
	for n := 0; ; n++ {
		if pct(child, "review", n) >= reviewSharePct && pct(child, "drill", n) >= drillSharePct {
			return n
		}
	}
}

func TestRankGradeOrdersGradeBeforeDifficulty(t *testing.T) {
	cs := []Candidate{{ID: "below", Type: "numeric", Grade: 3, Difficulty: 3}, {ID: "own", Type: "numeric", Grade: 4, Difficulty: 5}}
	got, _ := RankGrade(cs, "kid", "", 3, 4, plainPick("kid", 4))
	if got.ID != "own" {
		t.Errorf("grade must rank before difficulty, got %s", got.ID)
	}
	if got, _ := RankGrade(cs, "kid", "numeric", 3, 4, plainPick("kid", 4)); got.ID != "own" { // both are the avoided type: still grade first
		t.Errorf("got %s", got.ID)
	}
	if _, ok := RankGrade(nil, "kid", "", 3, 4, 0); ok {
		t.Error("empty list must give ok=false")
	}
}

func TestRankGradeSharesAndDrills(t *testing.T) {
	cs := []Candidate{
		{ID: "d3", Type: "numeric", Grade: 3, Difficulty: 3, Style: "drill"},
		{ID: "c3", Type: "choice", Grade: 3, Difficulty: 3, Style: "context"},
		{ID: "c4", Type: "numeric", Grade: 4, Difficulty: 3, Style: "context"},
		{ID: "d4", Type: "numeric", Grade: 4, Difficulty: 3, Style: "drill"},
	}
	for _, grade := range []int{3, 4} {
		below, drills := 0, 0
		const n = 2000
		for i := 0; i < n; i++ {
			got, _ := RankGrade(cs, "kid", "", 3, grade, i)
			if got.Grade > grade {
				t.Fatalf("served grade %d to a grade %d child", got.Grade, grade)
			}
			if got.Grade < grade {
				below++
			}
			if got.Style == "drill" {
				drills++
			}
		}
		if grade == 4 && (drills != 0 || below < n*10/100 || below > n*20/100) {
			t.Errorf("grade 4: drills=%d below=%d of %d (want 0 drills, review about 15 percent)", drills, below, n)
		}
		if grade == 3 && drills > n*13/100 {
			t.Errorf("grade 3: %d drills of %d, want at most about 10 percent", drills, n)
		}
	}
	// grade 4 with only drills left: still playable
	if got, ok := RankGrade(cs[:1], "kid", "", 3, 4, 1); !ok || got.ID != "d3" {
		t.Error("a drill must stay as the last resort")
	}
}
