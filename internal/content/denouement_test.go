package content

import (
	"strings"
	"testing"
)

// TestDenouementIsAboutTheQuestion: after an answer the text comes from the question itself, never a stock line.
func TestDenouementIsAboutTheQuestion(t *testing.T) {
	fb := LText{FR: "On ne réunit pas.", EN: "We do not put them together."}
	plain := &Question{Explain: LText{FR: "12 + 8 = 20.", EN: "12 + 8 = 20."}, Hint: LText{FR: "Compte à partir de 12.", EN: "Count up from 12."}}
	written := &Question{Explain: plain.Explain, Hint: plain.Hint, Reaction: &Reaction{
		Right: LText{FR: "Les 8 gouttes manquantes, tu les as trouvées !", EN: "You found the 8 missing drops!"},
		Wrong: LText{FR: "Regarde la flèche : combien de pas jusqu'à 20 ?", EN: "Look at the arrow: how many steps to 20?"},
	}}
	cases := []struct {
		q                   *Question
		outcome, lang, want string
	}{
		{plain, OutcomeCorrect, "fr", "Oui ! 12 + 8 = 20."},
		{plain, OutcomeRandomError, "en", "Not quite. A hint: Count up from 12."},
		{written, OutcomeCorrect, "fr", "Les 8 gouttes manquantes, tu les as trouvées !"},
		{written, OutcomeCorrect, "en", "You found the 8 missing drops!"},
		{written, OutcomeRandomError, "fr", "Regarde la flèche : combien de pas jusqu'à 20 ?"},
		{written, OutcomeTypicalError, "fr", "Pas tout à fait. On ne réunit pas."},
	}
	for _, c := range cases {
		if got := DenouementText(c.outcome, c.lang, c.q, fb, 0); got != c.want {
			t.Errorf("%s/%s = %q, want %q", c.outcome, c.lang, got, c.want)
		}
	}
	// nothing written at all: the neutral line is still returned, never an empty text
	if got := DenouementText(OutcomeRandomError, "fr", &Question{}, LText{}, 1); strings.TrimSpace(got) == "" {
		t.Error("empty fallback")
	}
}
