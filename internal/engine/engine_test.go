package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

// mustGraph loads the embedded curriculum or fails the test.
func mustGraph(t *testing.T) *Graph {
	t.Helper()
	gr, err := LoadGraph()
	if err != nil {
		t.Fatal(err)
	}
	return gr
}

// TestGraphLoads checks the curriculum size and which skills have a generator.
func TestGraphLoads(t *testing.T) {
	gr := mustGraph(t)
	if n := len(gr.All()); n != 75 {
		t.Fatalf("want 75 skills, got %d", n)
	}
	want := []string{"ARI-OPE-A02a", "ARI-OPE-A02c", "ARI-OPE-A03a", "ARI-OPE-A05", "ARI-OPE-A06a", "ARI-OPE-A06c", "ARI-OPE-A07a", "ARI-SOP-A03a"}
	got := gr.Implemented()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("implemented = %v, want %v", got, want)
	}
}

// TestGeneratorProperties runs every implemented skill 720 times: the leak check is always empty,
// the answer is among 4 distinct choices, there are at least 3 distinct distractors, and grading is consistent.
func TestGeneratorProperties(t *testing.T) {
	gr := mustGraph(t)
	for _, id := range gr.Implemented() {
		t.Run(id, func(t *testing.T) {
			g := NewRNG(42)
			for i := 0; i < 720; i++ {
				p, err := gr.Generate(id, g, nil)
				if err != nil {
					t.Fatalf("run %d: %v", i, err)
				}
				if leaks := p.LeakDetected(); len(leaks) != 0 {
					t.Fatalf("run %d leak %v in %q", i, leaks, p.EnonceMath)
				}
				if len(p.Choix) != 4 {
					t.Fatalf("run %d: %d choices", i, len(p.Choix))
				}
				seen := map[string]bool{}
				hasAnswer := false
				for _, c := range p.Choix {
					if seen[key(c)] {
						t.Fatalf("run %d: duplicate choice %v in %v", i, c, p.Choix)
					}
					seen[key(c)] = true
					hasAnswer = hasAnswer || Equivalent(c, p.Reponse)
				}
				if !hasAnswer {
					t.Fatalf("run %d: answer %v not in %v", i, p.Reponse, p.Choix)
				}
				if len(p.Distracteurs) < 3 {
					t.Fatalf("run %d: %d distractors", i, len(p.Distracteurs))
				}
				for _, ds := range p.Distracteurs {
					if Equivalent(ds.Valeur, p.Reponse) {
						t.Fatalf("run %d: distractor equals answer", i)
					}
					if r := p.Evaluate(ds.Valeur); r.Type != ErreurTypique {
						t.Fatalf("run %d: distractor %v graded %v", i, ds.Valeur, r.Type)
					}
				}
				if r := p.Evaluate(p.Reponse); r.Type != Correct {
					t.Fatalf("run %d: answer graded %v", i, r.Type)
				}
				if p.Shape == "" || p.EnonceMath == "" || p.CompetenceID != id {
					t.Fatalf("run %d: missing shape/statement/id", i)
				}
			}
		})
	}
}

// TestDeterministic checks the same seed gives the same problem.
func TestDeterministic(t *testing.T) {
	gr := mustGraph(t)
	a, _ := gr.Generate("ARI-OPE-A07a", NewRNG(7), nil)
	b, _ := gr.Generate("ARI-OPE-A07a", NewRNG(7), nil)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatal("same seed gave different problems")
	}
}

// TestShapeRotation: consecutive problems never share a shape, and fresh problems differ.
func TestShapeRotation(t *testing.T) {
	gr := mustGraph(t)
	for _, id := range gr.Implemented() {
		g := NewRNG(1)
		prev := ""
		for i := 0; i < 40; i++ {
			p, err := gr.GenerateFresh(id, g, prev, nil)
			if err != nil {
				t.Fatalf("%s beat %d: %v", id, i, err)
			}
			if i > 0 && p.Shape == prev {
				t.Fatalf("%s beat %d repeated shape %q", id, i, prev)
			}
			prev = p.Shape
		}
	}
}

// TestNotImplemented mimics Python: a skill without generator raises, prerequisites of such skills are ignored.
func TestNotImplemented(t *testing.T) {
	gr := mustGraph(t)
	if _, err := gr.Generate("ARI-SEN-A01a", NewRNG(1), nil); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("want ErrNotImplemented, got %v", err)
	}
	if _, err := gr.Generate("NOPE", NewRNG(1), nil); !errors.Is(err, ErrUnknownSkill) {
		t.Fatalf("want ErrUnknownSkill, got %v", err)
	}
	if !gr.PrereqsMet("ARI-OPE-A02a", map[string]bool{}, true) {
		t.Fatal("A02a prerequisites are all unimplemented: must be met when ignoring them")
	}
	if gr.PrereqsMet("ARI-OPE-A03a", map[string]bool{}, true) {
		t.Fatal("A03a needs A02a (implemented): must not be met")
	}
	if !gr.PrereqsMet("ARI-OPE-A03a", map[string]bool{"ARI-OPE-A02a": true}, true) {
		t.Fatal("A03a must be met once A02a is mastered")
	}
}

// TestEquivalent is a table test of answer comparison.
func TestEquivalent(t *testing.T) {
	tests := []struct {
		name string
		a, b Value
		want bool
	}{
		{"int equal", 42, 42, true},
		{"int vs float", 42, 42.0, true},
		{"int vs text", 42, " 42 ", true},
		{"different", 42, 43, false},
		{"pair equal", []int{3, 1}, []int{3, 1}, true},
		{"pair from json", []any{3.0, 1.0}, []int{3, 1}, true},
		{"pair different", []int{3, 1}, []int{3, 2}, false},
		{"pair vs int", []int{3, 1}, 3, false},
		{"text case", "Le Total", "le total", true},
		{"text different", "a", "b", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Equivalent(tc.a, tc.b); got != tc.want {
				t.Fatalf("Equivalent(%v,%v)=%v", tc.a, tc.b, got)
			}
		})
	}
}

// TestEvaluateOutcomes covers the three outcomes.
func TestEvaluateOutcomes(t *testing.T) {
	p := Problem{CompetenceID: "X", Reponse: 12, Distracteurs: []Distractor{{7, "addition_facteurs", "Additionne"}}}
	tests := []struct {
		given Value
		want  Outcome
		code  string
	}{{12, Correct, ""}, {7, ErreurTypique, "addition_facteurs"}, {99, ErreurAleatoire, ""}}
	for _, tc := range tests {
		r := p.Evaluate(tc.given)
		if r.Type != tc.want || r.CodeErreur != tc.code {
			t.Fatalf("given %v: %+v", tc.given, r)
		}
	}
}

// TestLeakCheckDetectsLeak proves the check works: an answer left in the scene context is reported.
func TestLeakCheckDetectsLeak(t *testing.T) {
	p := Problem{EnonceMath: "6 × 7 = ?", Reponse: 42, ClesReponse: []string{}, ContexteNarratif: map[string]any{"resultat": 42, "terme_1": 6}}
	if leaks := p.LeakDetected(); len(leaks) != 1 || leaks[0] != "resultat" {
		t.Fatalf("leaks = %v", leaks)
	}
	p.ClesReponse = []string{"resultat"}
	if leaks := p.LeakDetected(); len(leaks) != 0 {
		t.Fatalf("hidden key still leaks: %v", leaks)
	}
	// a value that is also written in the statement is not a leak: "4 + 0 = ?"
	q := Problem{EnonceMath: "4 + 0 = ?", Reponse: 4, ContexteNarratif: map[string]any{"terme_1": 4}}
	if leaks := q.LeakDetected(); len(leaks) != 0 {
		t.Fatalf("visible value flagged: %v", leaks)
	}
}

// TestPayloads checks the two LLM payloads: the scene hides the answer, phase 1 denouement hides it too.
func TestPayloads(t *testing.T) {
	gr := mustGraph(t)
	p, _ := gr.Generate("ARI-OPE-A06a", NewRNG(3), nil)
	scene := p.ForLLMScene()["contexte_narratif"].(map[string]any)
	if _, has := scene["resultat"]; has {
		t.Fatal("scene payload contains the result")
	}
	res := p.Evaluate(p.Reponse)
	if _, has := p.ForLLMDenouement(res, true)["contexte_narratif"].(map[string]any)["resultat"]; has {
		t.Fatal("hidden denouement payload contains the result")
	}
	if _, has := p.ForLLMDenouement(res, false)["contexte_narratif"].(map[string]any)["resultat"]; !has {
		t.Fatal("full denouement payload should carry the result (Python parity)")
	}
}

// TestJSONRoundTrip: a stored problem still grades after decoding (numbers come back as float64).
func TestJSONRoundTrip(t *testing.T) {
	gr := mustGraph(t)
	for _, id := range gr.Implemented() {
		p, _ := gr.Generate(id, NewRNG(9), nil)
		raw, _ := json.Marshal(p)
		var back Problem
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}
		back.Normalize()
		if r := back.Evaluate(p.Reponse); r.Type != Correct {
			t.Fatalf("%s: round trip lost the answer: %+v", id, r)
		}
		if len(back.LeakDetected()) != 0 {
			t.Fatalf("%s: leak after round trip", id)
		}
	}
}

// TestBKT covers learning, forgetting, typical errors and persistence.
func TestBKT(t *testing.T) {
	gr := mustGraph(t)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	prof := NewProfile("kid", gr)
	ok := Result{Type: Correct}
	var last float64
	for i := 0; i < 8; i++ {
		last = prof.Observe("ARI-OPE-A02a", ok, 1000, 4000, t0.Add(time.Duration(i)*time.Minute))
	}
	if last < MasteryThreshold {
		t.Fatalf("8 quick successes should master a fact skill, got %.2f", last)
	}
	if e := prof.States["ARI-OPE-A02a"]; e.Reviews != 0 {
		t.Fatalf("successes within minutes must not count as spaced reviews, got %d", e.Reviews)
	}
	// forgetting: 3 days later the mastery has fallen below the threshold and the skill is due
	later := t0.Add(72 * time.Hour)
	if prof.Mastery("ARI-OPE-A02a", later) >= MasteryThreshold {
		t.Fatal("mastery must decay")
	}
	if due := prof.ToReview(later); len(due) != 1 || due[0].SkillID != "ARI-OPE-A02a" {
		t.Fatalf("due = %v", due)
	}
	// a spaced success lengthens the half-life
	before := prof.States["ARI-OPE-A02a"].HalfLife
	prof.Observe("ARI-OPE-A02a", ok, 0, 0, later)
	if prof.States["ARI-OPE-A02a"].HalfLife <= before {
		t.Fatal("spaced success must lengthen the half-life")
	}
	// typical error: recorded as active misconception, cleared by the next success
	prof.Observe("ARI-OPE-A07a", Result{Type: ErreurTypique, CodeErreur: "reste_ignore", DescriptionErreur: "Ignore le reste"}, 0, 0, later)
	if m := prof.ActiveMisconceptions(); len(m["reste_ignore"]) != 1 {
		t.Fatalf("misconceptions = %v", m)
	}
	// persistence round trip
	raw, err := prof.JSON()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseProfile("kid", raw, gr)
	if err != nil {
		t.Fatal(err)
	}
	if back.Mastery("ARI-OPE-A02a", later) != prof.Mastery("ARI-OPE-A02a", later) || back.Descs["reste_ignore"] == "" {
		t.Fatal("profile did not survive JSON")
	}
	prof.Observe("ARI-OPE-A07a", ok, 0, 0, later.Add(time.Minute))
	if m := prof.ActiveMisconceptions(); len(m) != 0 {
		t.Fatalf("success must clear misconceptions: %v", m)
	}
	// the profile of an unknown child starts empty
	if p, err := ParseProfile("x", "{}", gr); err != nil || len(p.States) != 0 {
		t.Fatal("empty profile expected")
	}
}

// TestPlanner covers the four reasons in priority order.
func TestPlanner(t *testing.T) {
	gr := mustGraph(t)
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	g := NewRNG(5)
	prof := NewProfile("kid", gr)

	sel, err := Choose(gr, prof, "nature", 4, g, now)
	if err != nil || sel.Reason != "nouvelle" || sel.SkillID != "ARI-OPE-A02a" {
		t.Fatalf("fresh child: %+v %v", sel, err)
	}
	prof.Observe("ARI-OPE-A02a", Result{Type: ErreurAleatoire}, 0, 0, now)
	if sel, _ = Choose(gr, prof, "nature", 4, g, now); sel.Reason != "consolidation" || sel.SkillID != "ARI-OPE-A02a" {
		t.Fatalf("in progress: %+v", sel)
	}
	for i := 0; i < 10; i++ {
		prof.Observe("ARI-OPE-A02a", Result{Type: Correct}, 0, 0, now.Add(time.Duration(i)*time.Second))
	}
	if sel, _ = Choose(gr, prof, "nature", 4, g, now.Add(time.Minute)); sel.Reason != "nouvelle" || sel.SkillID == "ARI-OPE-A02a" {
		t.Fatalf("after mastery: %+v", sel)
	}
	if sel, _ = Choose(gr, prof, "nature", 4, g, now.Add(10*24*time.Hour)); sel.Reason != "revision" || sel.SkillID != "ARI-OPE-A02a" {
		t.Fatalf("later: %+v", sel)
	}
	if _, err := Choose(gr, NewProfile("k", gr), "nature", 0, g, now); !errors.Is(err, ErrNoSkill) {
		t.Fatalf("grade 0 should find nothing, got %v", err)
	}
}

// PlayableFor counts only the bank questions a child of that school year can be served (question grade <= child grade).
func TestPlayableForCountsOnlyQuestionsUpToTheChildGrade(t *testing.T) {
	gr, err := LoadGraph()
	if err != nil {
		t.Fatal(err)
	}
	const skill = "ARI-SEN-A08" // a skill without generator: only the bank makes it playable
	if gr.HasGenerator(skill) {
		t.Skip("skill has a generator now; pick a bank-only skill")
	}
	gr.SetBank([]BankEntry{{World: "jeux", Skill: skill, Published: 10, ByGrade: map[int]int{1: 3, 3: 7}}})
	cases := []struct {
		grade int
		want  int
		play  bool
	}{{1, 3, false}, {2, 3, false}, {3, 10, true}, {6, 10, true}}
	for _, c := range cases {
		if got := gr.BankFor("jeux", skill, c.grade); got != c.want {
			t.Errorf("grade %d: BankFor = %d, want %d", c.grade, got, c.want)
		}
		if got := gr.PlayableFor("jeux", skill, c.grade); got != c.play {
			t.Errorf("grade %d: PlayableFor = %v, want %v", c.grade, got, c.play)
		}
	}
}
