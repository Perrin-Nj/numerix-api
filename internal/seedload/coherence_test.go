package seedload

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/Perrin-Nj/numerix-api/internal/content"
)

// focusWorlds are the worlds a child can play right now (chess, medicine, space).
var focusWorlds = []string{"echecs.", "medecine.", "espace."}

// numRe finds whole numbers in a text; spaces inside numbers ("2 985") are removed first.
var numRe = regexp.MustCompile(`\d+`)

// textNumbers returns every number written in the question (story, prompt, math text, choices and items), fr and en.
func textNumbers(q *content.Question) map[string]bool {
	var b strings.Builder
	for _, s := range []string{q.Story.FR, q.Prompt.FR, q.Story.EN, q.Prompt.EN, q.MathText} {
		b.WriteString(s + " ")
	}
	for _, c := range q.Choices {
		b.WriteString(c.Text.FR + " ")
	}
	if q.Items != nil {
		for _, l := range [][]content.Item{q.Items.List, q.Items.Left, q.Items.Right} {
			for _, it := range l {
				b.WriteString(it.Text.FR + " ")
			}
		}
	}
	text := regexp.MustCompile(`(\d) (\d{3})\b`).ReplaceAllString(b.String(), "$1$2") // "2 985" -> "2985"
	out := map[string]bool{}
	for _, n := range numRe.FindAllString(text, -1) {
		out[n] = true
	}
	for word, n := range numberWords { // "deux tours" and "two rooks" count as 2
		if regexp.MustCompile(`(?i)\b` + word + `\b`).MatchString(text) { // whole word only
			out[n] = true
		}
	}
	return out
}

// numberWords are the small numbers written as words in fr and en.
var numberWords = map[string]string{"un": "1", "une": "1", "one": "1", "deux": "2", "trois": "3", "quatre": "4", "cinq": "5", "six": "6", "sept": "7", "huit": "8",
	"neuf": "9", "dix": "10", "onze": "11", "douze": "12", "two": "2", "three": "3", "four": "4", "five": "5", "seven": "7",
	"eight": "8", "nine": "9", "ten": "10", "eleven": "11", "twelve": "12"}

// knownExtras are numbers a diagram may show without the text saying them, each with its reason.
var knownExtras = map[string]string{
	"echecs.valeurs.013":   "the piece values are the lesson of the quest, the diagram is the value chart",
	"medecine.poids.006":   "2 kg shown on the same scale as the grams",
	"medecine.poids.008":   "2 kg shown in grams, the unit of the question",
	"medecine.dossier.007": "the question is about reading any bar chart, the diagram is a sample chart of the quest",
	"medecine.dossier.012": "the question names the parts of a bar chart, the diagram is a sample chart",
}

// shown lists the numbers a diagram displays (bar values, pictograph counts, number line arrow).
func shown(v *content.Visual) []float64 {
	var out []float64
	asList := func(x any) []any { l, _ := x.([]any); return l }
	num := func(x any) (float64, bool) { f, ok := x.(float64); return f, ok }
	switch v.Kind {
	case "bars":
		for _, b := range asList(v.Data["bars"]) {
			if f, ok := num(b.(map[string]any)["value"]); ok {
				out = append(out, f)
			}
		}
	case "pictograph":
		for _, r := range asList(v.Data["rows"]) {
			if f, ok := num(r.(map[string]any)["count"]); ok {
				out = append(out, f)
			}
		}
	case "numberline":
		if f, ok := num(v.Data["arrow"]); ok {
			out = append(out, f)
		}
	}
	return out
}

// TestFocusWorldsEveryQuestionHasAMatchingDiagram: in chess, medicine and space every published question carries a
// diagram, and every number the diagram shows is a number the question itself mentions (so a picture never
// belongs to another question). Spring: a data-integrity test over the seed "fixtures".
func TestFocusWorldsEveryQuestionHasAMatchingDiagram(t *testing.T) {
	f, err := Read(seedDir, ReadOptions{Exclude: []string{ExamplesFile}, SkipAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, q := range f.Questions() {
		focus := false
		for _, p := range focusWorlds {
			focus = focus || strings.HasPrefix(q.ID, p)
		}
		if !focus {
			continue
		}
		count++
		if q.Visual == nil {
			t.Errorf("%s: no diagram", q.ID)
			continue
		}
		if q.Type == content.TypeVisual || knownExtras[q.ID] != "" {
			continue // visual questions keep their data only in the diagram, by design
		}
		if q.Reaction == nil || q.Reaction.Right.FR == "" || q.Reaction.Wrong.FR == "" || q.Reaction.Right.EN == "" || q.Reaction.Wrong.EN == "" {
			t.Errorf("%s: no written reaction (right and wrong, fr and en)", q.ID)
		} else if bad := reactionNumbersNotInQuestion(q); len(bad) > 0 && !derivedInReaction[q.ID] {
			t.Errorf("%s: reaction mentions %v which the question never says", q.ID, bad)
		}
		nums := textNumbers(q)
		for _, n := range shown(q.Visual) {
			if !nums[fmt.Sprint(int64(n))] && n == float64(int64(n)) {
				t.Errorf("%s: diagram shows %v but the question never says it", q.ID, n)
			}
		}
	}
	if count < 200 {
		t.Fatalf("only %d focus questions found, expected about 210", count)
	}
}

// reactionNumbersNotInQuestion returns the numbers a written reaction mentions that appear nowhere in the question:
// not in its texts, its explanation, its diagram, its answer or the sums behind it. A reaction about another question
// would fail here.
func reactionNumbersNotInQuestion(q *content.Question) []string {
	known := textNumbers(q)
	for _, n := range numRe.FindAllString(q.Explain.FR+" "+q.Explain.EN+" "+q.Hint.FR+" "+q.Answer.Calc, -1) {
		known[n] = true
	}
	for _, n := range numRe.FindAllString(string(q.Answer.Value), -1) { // the numeric answer itself
		known[n] = true
	}
	for _, c := range q.Answer.CalcChoice {
		for _, n := range numRe.FindAllString(c, -1) {
			known[n] = true
		}
	}
	for _, n := range shown(q.Visual) {
		known[fmt.Sprint(int64(n))] = true
	}
	var bad []string
	for _, txt := range []string{q.Reaction.Right.FR, q.Reaction.Wrong.FR} {
		for _, n := range numRe.FindAllString(txt, -1) {
			if !known[n] {
				bad = append(bad, n)
			}
		}
	}
	return bad
}

// derivedInReaction lists the questions whose reaction shows a step worked out from the question's own numbers
// (a rounding, a counting-by list, a unit change, an estimate). Each was checked by hand.
var derivedInReaction = map[string]bool{
	"echecs.cases.013": true, "echecs.valeurs.015": true, "echecs.motifs.013": true, "echecs.partie.003": true,
	"espace.decompte.011": true, "espace.orbites.003": true, "espace.horloge.013": true, "espace.distances.002": true,
	"espace.distances.003": true, "espace.distances.004": true, "espace.distances.006": true, "espace.distances.007": true,
	"espace.distances.008": true, "medecine.poids.011": true,
}
