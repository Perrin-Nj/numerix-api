package content

import (
	"slices"
	"testing"
)

// Order, match and true/false questions have no single hidden number, but a hint can still give the answer away.
func TestLeaksForOrderMatchAndTrueFalse(t *testing.T) {
	o := orderQ() // 1/4, 1/2, 3/4 in this order
	m := matchQ() // 2 + 2 with quatre, 3 + 4 with sept
	tf := tfQ()
	for _, c := range []struct {
		q    *Question
		text string
		lang string
		want bool
	}{
		{o, "Mets d’abord 1/4 puis 1/2.", "fr", true},
		{o, "Compare 3/4 et 1/4.", "fr", false}, // two items but not in the answer order
		{o, "Compare les fractions avec un même dénominateur.", "fr", false},
		{m, "2 + 2 va avec quatre.", "fr", true},
		{m, "Relie chaque calcul à son résultat.", "fr", false},
		{m, "3 + 4 and seven go together.", "en", true},
		{tf, "C’est faux, relis bien.", "fr", true},
		{tf, "Yes, that is true.", "en", true},
		{tf, "Relis l’énoncé avec calme.", "fr", false},
	} {
		if got := c.q.Leaks(c.text, c.lang); got != c.want {
			t.Errorf("%s %q: leak=%v, want %v", c.q.Type, c.text, got, c.want)
		}
	}
}

func TestValidatorChecksHintsOfOrderMatchAndTrueFalse(t *testing.T) {
	q := orderQ()
	q.ID = "echecs.rangs.003"
	q.Hint = LText{"Mets 1/4 puis 1/2 puis 3/4.", "Put 1/4 then 1/2 then 3/4."}
	if !HasErrors(testEnv().Validate(q)) || !slices.Contains(Codes(testEnv().Validate(q)), "answer_leak") {
		t.Fatal("an order hint that lists the answer must fail in the three worlds")
	}
	q.ID = "cuisine.fournee.003"
	if HasErrors(testEnv().Validate(q)) {
		t.Fatal("in the older worlds it is a warning")
	}
	q = tfQ()
	q.ID = "medecine.gestes.005"
	q.Hint = LText{"C’est faux.", "It is false."}
	if !slices.Contains(Codes(testEnv().Validate(q)), "answer_leak") || !HasErrors(testEnv().Validate(q)) {
		t.Fatal("a true/false hint that says the answer must fail")
	}
	if Errors(testEnv().Validate(orderQ())) != nil || Errors(testEnv().Validate(matchQ())) != nil {
		t.Fatal("the clean fixtures stay valid")
	}
}

// feedback_leak: a warning everywhere, an error with the strict flag in echecs, medecine and espace.
func TestFeedbackLeakIsStrictInTheThreeWorlds(t *testing.T) {
	q := base()
	q.CommonErrors[0].Feedback = LText{"Presque ! La réponse est 15.", "Close! The answer is 15."}
	find := func(e *Env, q *Question) *Issue {
		for _, i := range e.Validate(q) {
			if i.Code == "feedback_leak" {
				return &i
			}
		}
		return nil
	}
	e := testEnv()
	if i := find(e, q); i == nil || !i.Warning {
		t.Fatalf("a warning by default: %v", i)
	}
	e.StrictStyle = true
	if i := find(e, q); i == nil || !i.Warning {
		t.Fatalf("still a warning outside the three worlds: %v", i)
	}
	q.ID = "echecs.valeurs.001"
	if i := find(e, q); i == nil || i.Warning {
		t.Fatalf("an error under --strict in echecs: %v", i)
	}
	e.StrictStyle = false
	if i := find(e, q); i == nil || !i.Warning {
		t.Fatalf("a warning without --strict: %v", i)
	}
}
