package engine

import "testing"

// TestBankOnlyWorldPlaysWrittenQuestionsOnly: in a written-questions world a quest is open for every school year,
// and a skill with only a generator is not offered. Another world keeps the normal rules.
func TestBankOnlyWorldPlaysWrittenQuestionsOnly(t *testing.T) {
	gr, err := LoadGraph()
	if err != nil {
		t.Fatal(err)
	}
	const quest, generated = "GEO-A01", "ARI-OPE-A07a" // a skill with a written bank in "echecs", and one that only has a generator
	if !gr.HasGenerator(generated) {
		t.Skipf("%s has no generator any more, pick another", generated)
	}
	gr.SetBank([]BankEntry{{World: "echecs", Skill: quest, Published: 14, ByGrade: map[int]int{1: 3, 2: 11}}})
	gr.SetBankOnly("echecs")
	if !gr.IsBankOnly("echecs") || gr.IsBankOnly("nature") {
		t.Fatal("bank-only flag wrong")
	}
	if !gr.PlayableFor("echecs", quest, 1) { // school year 1 sees only 3 questions, but the quest still opens
		t.Error("the quest must be open for every school year")
	}
	if gr.PlayableFor("echecs", generated, 6) {
		t.Error("a generated skill must not be offered in a written-questions world")
	}
	if !gr.PlayableFor("nature", generated, 6) {
		t.Error("other worlds keep their generated chapters")
	}
}

// TestBankOnlyNeedsALoadedBank: with an empty bank the world falls back to the normal rules, so it never becomes unplayable.
func TestBankOnlyNeedsALoadedBank(t *testing.T) {
	gr, err := LoadGraph()
	if err != nil {
		t.Fatal(err)
	}
	gr.SetBankOnly("echecs")
	if gr.IsBankOnly("echecs") {
		t.Error("an empty bank must not switch the world to written questions only")
	}
}
