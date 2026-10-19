package content

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// raw turns a Go literal into the JSON the API receives.
func raw(s string) json.RawMessage { return json.RawMessage(s) }

// testEnv knows one quest, one skill and a few misconception codes.
func testEnv() *Env {
	return &Env{
		Skill: func(id string) (SkillInfo, bool) { return SkillInfo{YearIntro: 1}, id == "ARI-OPE-A02c" },
		Quest: func(id string) (QuestInfo, bool) {
			return QuestInfo{World: "cuisine", Skill: "ARI-OPE-A02c", GradeMin: 1, GradeMax: 3, TargetCount: 12}, id == "cuisine.fournee"
		},
		Misconception: func(c string) bool { return strings.HasPrefix(c, "mis_") },
		Asset:         func(ref string) bool { return ref == "a_known" },
		Vocab:         func(f, v string) bool { return v != "unknown" },
		Denylist:      []string{"lego"},
		Names:         map[string]bool{"lea": true, "noah": true},
	}
}

// base returns a valid numeric question; tests change one thing at a time.
func base() *Question {
	return &Question{
		ID: "cuisine.fournee.001", Quest: "cuisine.fournee", Skill: "ARI-OPE-A02c", Type: TypeNumeric, Grade: 2, Difficulty: 2,
		Story:    LText{"Léa sort deux plaques du four.", "Lea takes two trays out of the oven."},
		Prompt:   LText{"Il y a 8 biscuits sur la première plaque et 7 sur la deuxième. Combien en tout ?", "There are 8 cookies on the first tray and 7 on the second. How many in all?"},
		MathText: "8 + 7 = ?",
		Answer:   Answer{Kind: KindNumeric, Value: raw("15"), Calc: "8+7"},
		CommonErrors: []CommonError{{Value: raw("14"), Misconception: "mis_off_by_one",
			Feedback: LText{"Presque ! Compte encore.", "Close! Count again."}}},
		Hint:    LText{"Pars de 8 et avance de 7 pas.", "Start at 8 and hop 7 steps."},
		Explain: LText{"8 + 7 = 8 + 2 + 5, donc 10 + 5.", "8 + 7 = 8 + 2 + 5, so 10 + 5."},
		Tags:    []string{"cookies", "add-within-20"},
		Status:  StatusReview, Author: "t", Version: 1, Source: "seed:test.json",
	}
}

func choiceQ() *Question {
	q := base()
	q.ID, q.Type = "cuisine.fournee.002", TypeChoice
	q.MathText, q.CommonErrors = "", nil
	q.Choices = []Choice{
		{ID: "a", Text: LText{"13", "13"}, Misconception: "mis_off_by_one"},
		{ID: "b", Text: LText{"15", "15"}},
		{ID: "c", Text: LText{"78", "78"}, Misconception: "mis_wrong_operation"},
		{ID: "d", Text: LText{"1", "1"}, Random: true},
	}
	q.Answer = Answer{Kind: KindChoice, Correct: "b", CalcChoice: map[string]string{"b": "8+7"}}
	return q
}

func orderQ() *Question {
	q := base()
	q.ID, q.Type, q.MathText, q.CommonErrors = "cuisine.fournee.003", TypeOrder, "", nil
	q.Items = &Items{List: []Item{
		{ID: "a", Text: LText{"3/4", "3/4"}, Key: "3/4"}, {ID: "b", Text: LText{"1/4", "1/4"}, Key: "1/4"}, {ID: "c", Text: LText{"1/2", "1/2"}, Key: "1/2"},
	}}
	q.Answer = Answer{Kind: KindOrder, Sequence: []string{"b", "c", "a"}, By: "asc", Misconception: "mis_order"}
	return q
}

func matchQ() *Question {
	q := base()
	q.ID, q.Type, q.MathText, q.CommonErrors = "cuisine.fournee.004", TypeMatch, "", nil
	q.Items = &Items{
		Left:  []Item{{ID: "l1", Text: LText{"2 + 2", "2 + 2"}}, {ID: "l2", Text: LText{"3 + 4", "3 + 4"}}},
		Right: []Item{{ID: "r1", Text: LText{"sept", "seven"}}, {ID: "r2", Text: LText{"quatre", "four"}}},
	}
	q.Answer = Answer{Kind: KindMatch, Pairs: [][2]string{{"l1", "r2"}, {"l2", "r1"}}}
	return q
}

func tfQ() *Question {
	q := base()
	q.ID, q.Type, q.MathText, q.CommonErrors = "cuisine.fournee.005", TypeTrueFalse, "", nil
	q.Answer = Answer{Kind: KindTrueFalse, Value: raw("false"), Misconception: "mis_off_by_one"}
	return q
}

func estimateQ() *Question {
	q := base()
	q.ID, q.Type, q.MathText, q.CommonErrors = "cuisine.fournee.006", TypeEstimate, "", nil
	q.Answer = Answer{Kind: KindRange, Min: "190", Max: "210", ExactForCheck: "198", Calc: "99*2"}
	return q
}

func TestBaseQuestionsAreValid(t *testing.T) {
	for _, q := range []*Question{base(), choiceQ(), orderQ(), matchQ(), tfQ(), estimateQ()} {
		t.Run(q.Type, func(t *testing.T) {
			if errs := Errors(testEnv().Validate(q)); len(errs) > 0 {
				t.Fatalf("unexpected errors: %v", errs)
			}
		})
	}
}

// TestRules: one failing fixture per rule of plan 3.5. Each mutation must produce its rule code.
func TestRules(t *testing.T) {
	cases := []struct {
		name string
		mk   func() *Question
		code string
	}{
		{"bad id", func() *Question { q := base(); q.ID = "Bad ID"; return q }, "id_format"},
		{"id prefix", func() *Question { q := base(); q.ID = "sport.fournee.001"; return q }, "id_prefix"},
		{"unknown quest", func() *Question { q := base(); q.Quest = "nope.x"; q.ID = "nope.x.001"; return q }, "quest_unknown"},
		{"skill differs from quest", func() *Question { q := base(); q.Skill = "ARI-OPE-A02a"; return q }, "skill_mismatch"},
		{"unknown skill", func() *Question { q := base(); q.Skill = "X-1"; return q }, "skill_unknown"},
		{"grade outside quest", func() *Question { q := base(); q.Grade = 4; return q }, "grade_range"},
		{"difficulty", func() *Question { q := base(); q.Difficulty = 9; return q }, "difficulty_value"},
		{"kind for type", func() *Question { q := tfQ(); q.Type = TypeNumeric; return q }, "kind_for_type"},
		{"visual missing", func() *Question { q := base(); q.Type = TypeVisual; return q }, "visual_missing"},
		{"missing en", func() *Question { q := base(); q.Prompt.EN = ""; return q }, "missing_en"},
		{"missing fr", func() *Question { q := base(); q.Story.FR = ""; return q }, "missing_fr"},
		{"fr equals en", func() *Question { q := base(); q.Hint.EN = q.Hint.FR; return q }, "fr_equals_en"},
		{"answer not in choices", func() *Question { q := choiceQ(); q.Answer.Correct = "z"; return q }, "answer_not_in_choices"},
		{"too few choices", func() *Question { q := choiceQ(); q.Choices = q.Choices[:2]; return q }, "choice_count"},
		{"duplicate distractor", func() *Question {
			q := choiceQ()
			q.Choices[0].Text = LText{"78", "78"}
			return q
		}, "choice_duplicate"},
		{"same numeric value", func() *Question {
			q := choiceQ()
			q.Choices[0].Text = LText{"15,0", "15.0"}
			return q
		}, "choice_same_value"},
		{"unlabelled distractor", func() *Question { q := choiceQ(); q.Choices[0].Misconception = ""; return q }, "distractor_unlabelled"},
		{"two random", func() *Question { q := choiceQ(); q.Choices[0].Misconception, q.Choices[0].Random = "", true; return q }, "too_many_random"},
		{"unknown misconception", func() *Question { q := choiceQ(); q.Choices[0].Misconception = "bogus"; return q }, "misconception_unknown"},
		{"common error equals truth", func() *Question { q := base(); q.CommonErrors[0].Value = raw("15"); return q }, "common_error_is_truth"},
		{"bad calc", func() *Question { q := base(); q.Answer.Calc = "8+"; return q }, "calc_invalid"},
		{"calc mismatch", func() *Question { q := base(); q.Answer.Calc = "8+8"; return q }, "calc_mismatch"},
		{"calc_choice mismatch", func() *Question { q := choiceQ(); q.Answer.CalcChoice["b"] = "8+8"; return q }, "calc_mismatch"},
		{"range too wide", func() *Question { q := estimateQ(); q.Answer.Min = "100"; q.Answer.Max = "300"; return q }, "range_too_wide"},
		{"range exact outside", func() *Question { q := estimateQ(); q.Answer.ExactForCheck = "400"; return q }, "range_exact_outside"},
		{"order not a permutation", func() *Question { q := orderQ(); q.Answer.Sequence = []string{"a", "b"}; return q }, "order_permutation"},
		{"order disagrees with keys", func() *Question { q := orderQ(); q.Answer.Sequence = []string{"a", "b", "c"}; return q }, "order_keys"},
		{"match incomplete", func() *Question { q := matchQ(); q.Answer.Pairs = q.Answer.Pairs[:1]; return q }, "match_incomplete"},
		{"match right twice", func() *Question { q := matchQ(); q.Answer.Pairs[1][1] = "r2"; return q }, "match_right_twice"},
		{"truefalse needs bool", func() *Question { q := tfQ(); q.Answer.Value = raw(`"maybe"`); return q }, "truefalse_value"},
		{"leak digits in story", func() *Question { q := base(); q.Story.FR = "Léa a 15 biscuits."; return q }, "answer_leak"},
		{"leak words fr", func() *Question { q := base(); q.Hint.FR = "Le total est quinze."; return q }, "answer_leak"},
		{"leak words en", func() *Question { q := base(); q.Prompt.EN = "The answer is fifteen cookies. How many?"; return q }, "answer_leak"},
		{"leak in math_text", func() *Question { q := base(); q.MathText = "8 + 7 = 15"; return q }, "answer_leak"},
		{"leak choice text", func() *Question {
			q := choiceQ()
			q.Choices[1].Text = LText{"un gâteau", "a cake"}
			q.Answer.CalcChoice = nil
			q.Story = LText{"Léa dessine un gâteau.", "Lea draws a cake."}
			return q
		}, "answer_leak"},
		{"timer key", func() *Question {
			q := base()
			q.Visual = &Visual{Kind: "bars", Data: map[string]any{"timer": 5}}
			return q
		}, "timer_key"},
		{"hurry word", func() *Question { q := base(); q.Hint.FR = "Dépêche-toi de répondre."; return q }, "pressure_word"},
		{"countdown word", func() *Question { q := base(); q.Hint.EN = "Quick! Countdown is on."; return q }, "pressure_word"},
		{"sentence too long", func() *Question {
			q := base()
			q.Hint.FR = "Pars de huit et avance de sept pas sur la droite numérique jusqu'au total final."
			return q
		}, "sentence_too_long"},
		{"story too long", func() *Question {
			q := base()
			q.Story.EN = strings.Repeat("The baker waves. ", 12)
			return q
		}, "too_long"},
		{"em dash", func() *Question { q := base(); q.Story.FR = "Léa sort — vite — les plaques."; return q }, "dash"},
		{"emoji", func() *Question { q := base(); q.Story.EN = "Lea bakes \U0001F36A today."; return q }, "emoji"},
		{"denylist", func() *Question { q := base(); q.Story.EN = "Lea builds with Lego."; return q }, "denylist"},
		{"too many media", func() *Question {
			q := base()
			q.Media = []Media{{Role: "scene", Asset: "a_known"}, {Role: "object", Asset: "a_known"}, {Role: "visual", Asset: "a_known"}, {Role: "scene", Asset: "a_known"}}
			return q
		}, "media_count"},
		{"unknown asset", func() *Question { q := base(); q.Media = []Media{{Role: "scene", Asset: "a_nope"}}; return q }, "media_asset"},
		{"criteria vocabulary", func() *Question {
			q := base()
			q.Media = []Media{{Role: "scene", Criteria: &Criteria{World: "cuisine", Topic: "unknown", Kind: "photo"}}}
			return q
		}, "media_vocab"},
		{"criteria incomplete", func() *Question {
			q := base()
			q.Media = []Media{{Role: "scene", Criteria: &Criteria{Mood: "calm"}}}
			return q
		}, "media_criteria"},
		{"ai draft published", func() *Question { q := base(); q.AIDrafted, q.Status = true, StatusPublished; return q }, "ai_unreviewed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Codes(Errors(testEnv().Validate(tc.mk()))); !slices.Contains(got, tc.code) {
				t.Fatalf("want error %q, got %v", tc.code, got)
			}
		})
	}
}

func TestAllowOperandFlagAndRetiredSeed(t *testing.T) {
	q := base()
	q.MathText = "7 + 8 = ?"
	q.Story.FR = "Léa a 15 biscuits."
	q.AllowOperandEqualsAnswer = true
	if HasErrors(testEnv().Validate(q)) {
		t.Fatalf("flagged question should pass: %v", testEnv().Validate(q))
	}
	r := base()
	r.Status = StatusRetired
	env := testEnv()
	env.SeedFile = true
	if !slices.Contains(Codes(env.Validate(r)), "status_retired") {
		t.Fatal("retired in a seed file must fail")
	}
}

func TestWarningsDoNotBlock(t *testing.T) {
	q := base()
	q.Tags = nil
	q.Answer.Calc = ""
	is := testEnv().Validate(q)
	if HasErrors(is) {
		t.Fatalf("warnings only expected, got %v", is)
	}
	if got := Codes(is); !slices.Contains(got, "few_tags") || !slices.Contains(got, "calc_missing") {
		t.Fatalf("expected few_tags and calc_missing, got %v", got)
	}
}

func TestValidateSet(t *testing.T) {
	a, b := base(), base() // same id
	if !slices.Contains(Codes(testEnv().ValidateSet([]*Question{a, b})), "id_duplicate") {
		t.Fatal("duplicate id not reported")
	}
	c := base()
	c.ID = "cuisine.fournee.009"
	c.Prompt.FR += "!"
	if !slices.Contains(Codes(testEnv().ValidateSet([]*Question{a, c})), "near_duplicate") {
		t.Fatal("near duplicate not reported")
	}
}

func TestWorldQuota(t *testing.T) {
	qs := []*Question{base()}
	quests := []Quest{{ID: "cuisine.fournee", World: "cuisine", TargetCount: 12}}
	is := testEnv().WorldQuota("cuisine", qs, quests)
	if HasErrors(is) {
		t.Fatalf("an unpublished world only warns: %v", is)
	}
	qs[0].Status = StatusPublished
	if !HasErrors(testEnv().WorldQuota("cuisine", qs, quests)) {
		t.Fatal("a published world below quota must fail")
	}
}

func TestEvalTable(t *testing.T) {
	cases := []struct {
		expr string
		want string
		err  bool
	}{
		{"8+7", "15", false},
		{"0.1+0.2", "0.3", false},
		{"1/3", "1/3", false},
		{"2*(3+4)", "14", false},
		{"-3+5", "2", false},
		{"min(4,2,9)", "2", false},
		{"max(4,2,9)", "9", false},
		{"abs(-7)", "7", false},
		{"round(2.5)", "3", false},
		{"round(-2.5)", "-3", false},
		{"gcd(12,18)", "6", false},
		{"pow(3,2)", "9", false},
		{"sum(1,2,3)", "6", false},
		{"divmod(23,7)", "[3, 2]", false},
		{"1/0", "", true},
		{"x+1", "", true},
		{"pow(2,50)", "", true},
		{"divmod(23,0)", "", true},
		{"divmod(23,7)+1", "", true},
		{"(1+2", "", true},
		{"1.2.3", "", true},
		{"", "", true},
		{"2 3", "", true},
		{strings.Repeat("(", 30) + "1" + strings.Repeat(")", 30), "", true},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			v, err := Eval(tc.expr)
			if (err != nil) != tc.err {
				t.Fatalf("err = %v, want error %v", err, tc.err)
			}
			if err == nil && valueString(v) != tc.want {
				t.Fatalf("got %s, want %s", valueString(v), tc.want)
			}
		})
	}
}

func TestNumberWords(t *testing.T) {
	cases := []struct {
		n    int
		lang string
		want string
	}{
		{15, "en", "fifteen"}, {42, "en", "forty-two"}, {100, "en", "one hundred"}, {1000, "en", "one thousand"},
		{15, "fr", "quinze"}, {21, "fr", "vingt et un"}, {71, "fr", "soixante et onze"}, {80, "fr", "quatre-vingts"},
		{97, "fr", "quatre-vingt-dix-sept"}, {200, "fr", "deux cents"}, {1000, "fr", "mille"}, {2500, "fr", "deux mille cinq cents"},
	}
	for _, tc := range cases {
		if got := NumberWords(tc.n, tc.lang); got[0] != tc.want {
			t.Errorf("%d %s = %q, want %q", tc.n, tc.lang, got[0], tc.want)
		}
	}
	if NumberWords(-1, "fr") != nil || NumberWords(MaxWordNumber+1, "en") != nil {
		t.Error("out of range must be nil")
	}
	if !slices.Contains(NumberWords(80, "fr"), "quatre-vingt") {
		t.Error("singular variant missing")
	}
}

func TestCheck(t *testing.T) {
	num := base()
	num.Answer.Value = raw("[3,2]")
	num.CommonErrors = []CommonError{{Value: raw("[3,3]"), Misconception: "mis_rest"}}
	cases := []struct {
		name  string
		q     *Question
		given string
		want  string
		code  string
	}{
		{"numeric right", base(), "15", OutcomeCorrect, ""},
		{"numeric right as text", base(), `"15"`, OutcomeCorrect, ""},
		{"numeric typical", base(), "14", OutcomeTypicalError, "mis_off_by_one"},
		{"numeric random", base(), "99", OutcomeRandomError, ""},
		{"numeric garbage", base(), `"abc"`, OutcomeRandomError, ""},
		{"numeric empty", base(), ``, OutcomeRandomError, ""},
		{"pair right", num, "[3,2]", OutcomeCorrect, ""},
		{"pair typical", num, "[3,3]", OutcomeTypicalError, "mis_rest"},
		{"pair wrong shape", num, "3", OutcomeRandomError, ""},
		{"choice by id", choiceQ(), `"b"`, OutcomeCorrect, ""},
		{"choice by number", choiceQ(), "15", OutcomeCorrect, ""},
		{"choice typical", choiceQ(), "13", OutcomeTypicalError, "mis_off_by_one"},
		{"choice random distractor", choiceQ(), `"d"`, OutcomeRandomError, ""},
		{"choice unknown", choiceQ(), `"zzz"`, OutcomeRandomError, ""},
		{"truefalse right", tfQ(), "false", OutcomeCorrect, ""},
		{"truefalse word", tfQ(), `"faux"`, OutcomeCorrect, ""},
		{"truefalse wrong", tfQ(), "true", OutcomeTypicalError, "mis_off_by_one"},
		{"order right", orderQ(), `["b","c","a"]`, OutcomeCorrect, ""},
		{"order wrong", orderQ(), `["a","b","c"]`, OutcomeTypicalError, "mis_order"},
		{"order garbage", orderQ(), `5`, OutcomeRandomError, ""},
		{"match right any order", matchQ(), `[["l2","r1"],["l1","r2"]]`, OutcomeCorrect, ""},
		{"match wrong", matchQ(), `[["l1","r1"],["l2","r2"]]`, OutcomeRandomError, ""},
		{"match repeated pair", matchQ(), `[["l1","r2"],["l1","r2"]]`, OutcomeRandomError, ""},
		{"range inside", estimateQ(), "199", OutcomeCorrect, ""},
		{"range edge", estimateQ(), "210", OutcomeCorrect, ""},
		{"range outside", estimateQ(), "211", OutcomeRandomError, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.q.Check(raw(tc.given))
			if r.Outcome != tc.want || r.Misconception != tc.code {
				t.Fatalf("got %+v, want %s/%s", r, tc.want, tc.code)
			}
		})
	}
}

func TestCorrectAnswerAndChoiceValue(t *testing.T) {
	cases := []struct {
		q    *Question
		lang string
		want string
	}{
		{base(), "fr", "15"},
		{choiceQ(), "en", "15"},
		{orderQ(), "fr", `["b","c","a"]`},
		{matchQ(), "fr", `[["l1","r2"],["l2","r1"]]`},
		{tfQ(), "fr", "false"},
		{estimateQ(), "fr", "198"},
	}
	for _, tc := range cases {
		if got := string(tc.q.CorrectAnswer(tc.lang)); got != tc.want {
			t.Errorf("%s: got %s want %s", tc.q.Type, got, tc.want)
		}
	}
	if got := string(ChoiceValue(Choice{Text: LText{"1,5", "1.5"}}, "fr")); got != "1.5" {
		t.Errorf("decimal choice = %s", got)
	}
	if got := string(ChoiceValue(Choice{Text: LText{"un demi", "a half"}}, "en")); got != `"a half"` {
		t.Errorf("text choice = %s", got)
	}
}

func TestCanonicalHashIgnoresStatusAndOrder(t *testing.T) {
	a, b := base(), base()
	b.Status, b.Version, b.Author = StatusPublished, 7, "someone"
	ha, _ := a.ContentHash()
	hb, _ := b.ContentHash()
	if ha != hb {
		t.Fatal("status/version/author must not change the content hash")
	}
	b.Prompt.FR += " "
	hb, _ = b.ContentHash()
	if ha == hb {
		t.Fatal("a text change must change the hash")
	}
	x, _ := Canonical(map[string]any{"b": 1, "a": json.Number("0.10")})
	if string(x) != `{"a":0.10,"b":1}` {
		t.Fatalf("canonical = %s", x)
	}
}

func TestDecodeStrictAndJSONRoundTrip(t *testing.T) {
	src, _ := json.Marshal(orderQ())
	q, issues := testEnv().ValidateJSON(src)
	if q == nil || HasErrors(issues) {
		t.Fatalf("round trip failed: %v", issues)
	}
	bad := strings.Replace(string(src), `"status"`, `"timer":3,"status"`, 1)
	if q, is := testEnv().ValidateJSON([]byte(bad)); q != nil || !HasErrors(is) {
		t.Fatal("an unknown field must be refused")
	}
	m, _ := json.Marshal(matchQ())
	var back Question
	if err := DecodeStrict(m, &back); err != nil || back.Items == nil || len(back.Items.Left) != 2 {
		t.Fatalf("match items round trip: %v %+v", err, back.Items)
	}
}
