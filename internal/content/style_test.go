package content

import (
	"fmt"
	"slices"
	"testing"
)

// T1: style is drill or context; drills belong to grades 1 and 2, with at most 10 percent of a quest at grade 3.
func TestStyleRuleT1(t *testing.T) {
	q := base()
	q.Style = "quiz"
	if !slices.Contains(Codes(testEnv().Validate(q)), "style_value") {
		t.Fatal("an unknown style must fail")
	}
	q = base()
	q.Style = "drill"
	for grade, wantErr := range map[int]bool{1: false, 2: false, 3: false, 4: true} {
		q.Grade = grade
		got := slices.Contains(Codes(testEnv().Validate(q)), "style_drill_grade")
		if got != wantErr {
			t.Errorf("drill at grade %d: error=%v, want %v", grade, got, wantErr)
		}
	}
	q.Style = ""
	q.Grade = 4
	if slices.Contains(Codes(testEnv().Validate(q)), "style_drill_grade") {
		t.Error("a missing style counts as context")
	}
}

func TestStyleRuleT1ShareAtGrade3(t *testing.T) {
	mk := func(n, drills int) []*Question {
		var qs []*Question
		for i := 0; i < n; i++ {
			c := base()
			c.ID = fmt.Sprintf("cuisine.fournee.%03d", i+1)
			c.Grade = 3
			c.Prompt.FR += fmt.Sprintf(" Variante %d.", i)
			c.Answer.Calc = fmt.Sprintf("%d+%d", 8+i, 7-i%3)
			if i < drills {
				c.Style = "drill"
			}
			qs = append(qs, c)
		}
		return qs
	}
	if slices.Contains(Codes(testEnv().ValidateSet(mk(20, 2))), "style_drill_share") {
		t.Error("2 of 20 is 10 percent: allowed")
	}
	if !slices.Contains(Codes(testEnv().ValidateSet(mk(20, 3))), "style_drill_share") {
		t.Error("3 of 20 grade 3 questions as drills must fail")
	}
}

// Q1: 20 published questions per grade in a published world, a warning until the gate flag turns it into an error.
func TestGradeQuotaQ1(t *testing.T) {
	var qs []*Question
	for i := 0; i < 19; i++ {
		c := base()
		c.ID = fmt.Sprintf("cuisine.fournee.%03d", i+1)
		c.Grade, c.Status = 1, StatusPublished
		qs = append(qs, c)
	}
	quests := []Quest{{ID: "cuisine.fournee", World: "cuisine", GradeMin: 1, GradeMax: 1, TargetCount: 12}}
	is := testEnv().GradeQuota("cuisine", qs, quests, false)
	if len(is) != 1 || is[0].Code != "world_grade_quota" || !is[0].Warning {
		t.Fatalf("19 of 20 must warn once: %v", is)
	}
	if is = testEnv().GradeQuota("cuisine", qs, quests, true); !HasErrors(is) {
		t.Fatal("the gate flag makes it an error")
	}
	c := base()
	c.ID, c.Grade, c.Status = "cuisine.fournee.020", 1, StatusPublished
	if is = testEnv().GradeQuota("cuisine", append(qs, c), quests, true); len(is) != 0 {
		t.Fatalf("20 questions pass: %v", is)
	}
}

// F1: operations drill skills must carry an explicit style in the three worlds; the picker derives one when absent.
func TestDerivedStyle(t *testing.T) {
	for _, c := range []struct {
		skill string
		grade int
		given string
		want  string
	}{
		{"ARI-OPE-A02a", 1, "", StyleDrill}, {"ARI-OPE-A02c", 2, "", StyleDrill},
		{"ARI-OPE-A02a", 3, "", StyleContext}, {"ARI-OPE-A05", 4, "", StyleContext},
		{"ARI-OPE-A06a", 2, "", StyleContext}, {"GEO-A01", 1, "", StyleContext},
		{"ARI-OPE-A02a", 1, StyleContext, StyleContext}, {"ARI-OPE-A02a", 3, StyleDrill, StyleDrill},
	} {
		if got := DerivedStyle(c.skill, c.grade, c.given); got != c.want {
			t.Errorf("%s g%d %q: %s, want %s", c.skill, c.grade, c.given, got, c.want)
		}
	}
}

func TestStyleRequiredInTheThreeWorlds(t *testing.T) {
	q := base()
	q.ID, q.Skill, q.Grade = "echecs.valeurs.001", "ARI-OPE-A04a", 2
	e := testEnv()
	e.Skill = func(string) (SkillInfo, bool) { return SkillInfo{YearIntro: 1}, true }
	e.Quest = func(string) (QuestInfo, bool) {
		return QuestInfo{World: "echecs", Skill: "ARI-OPE-A04a", GradeMin: 1, GradeMax: 4, TargetCount: 12}, true
	}
	q.Quest = "echecs.valeurs"
	find := func(is []Issue) *Issue {
		for i := range is {
			if is[i].Code == "style_required" {
				return &is[i]
			}
		}
		return nil
	}
	if i := find(e.Validate(q)); i == nil || !i.Warning {
		t.Fatalf("a missing style on a drill skill is a warning by default: %v", i)
	}
	e.StrictStyle = true
	if i := find(e.Validate(q)); i == nil || i.Warning {
		t.Fatalf("an error with the strict flag: %v", i)
	}
	q.Style = StyleDrill
	if find(e.Validate(q)) != nil {
		t.Fatal("an explicit style passes")
	}
	q.Grade, q.Style = 3, StyleDrill // drill at grade 3 on a drill skill in these worlds is refused
	if !slices.Contains(Codes(e.Validate(q)), "style_drill_grade3") {
		t.Fatal("drill at grade 3 must fail in the three worlds")
	}
	q.Style = StyleContext
	if find(e.Validate(q)) != nil || slices.Contains(Codes(e.Validate(q)), "style_drill_grade3") {
		t.Fatal("context at grade 3 passes")
	}
	q.ID, q.Quest, q.Style = "cuisine.fournee.001", "cuisine.fournee", ""
	if find(e.Validate(q)) != nil {
		t.Fatal("other worlds are not covered")
	}
}
