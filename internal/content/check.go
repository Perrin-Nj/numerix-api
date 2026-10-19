package content

// Answer checking per question type. One entry point, Check, returns the same three outcomes as the
// generated questions (correct, erreur_typique, erreur_aleatoire) plus the misconception code. It never
// calls a language model: every verdict comes from the answer spec stored with the question.

import (
	"encoding/json"
	"math/big"
	"regexp"
	"slices"
	"strings"
)

// The three outcomes (same strings as engine.Outcome; the content package does not import the engine).
const (
	OutcomeCorrect      = "correct"
	OutcomeTypicalError = "erreur_typique"
	OutcomeRandomError  = "erreur_aleatoire"
)

const (
	maxPairSetSize = 20   // a matching question never has more pairs than this
	maxGivenBytes  = 4096 // refuse absurdly large answers before parsing
)

// Result is the verdict of Check.
type Result struct {
	Outcome       string
	Misconception string // set for erreur_typique
	Feedback      LText  // curated message of the matched common error, if any
}

// Check grades the child's answer (raw JSON as received) against the question's answer spec.
// An unreadable answer is a random error, never a crash.
func (q *Question) Check(given json.RawMessage) Result {
	random := Result{Outcome: OutcomeRandomError}
	if len(given) == 0 || len(given) > maxGivenBytes {
		return random
	}
	switch q.Answer.Kind {
	case KindNumeric:
		return q.checkNumeric(given)
	case KindRange:
		return q.checkRange(given)
	case KindChoice:
		return q.checkChoice(given)
	case KindTrueFalse:
		return q.checkTrueFalse(given)
	case KindOrder:
		return q.checkOrder(given)
	case KindMatch:
		return q.checkMatch(given)
	}
	return random
}

// typical builds an erreur_typique result when a code is known, otherwise a random error.
func typical(code string, fb LText) Result {
	if code == "" {
		return Result{Outcome: OutcomeRandomError}
	}
	return Result{Outcome: OutcomeTypicalError, Misconception: code, Feedback: fb}
}

// numOrPair is a decoded numeric answer: one number, or a (quotient, remainder) pair.
type numOrPair struct {
	r      *big.Rat
	pair   [2]*big.Rat
	isPair bool
}

// parseNum reads a JSON number, a numeric string ("1,5" and "1.5" both work) or a two element array.
func parseNum(raw json.RawMessage) (numOrPair, bool) {
	var arr []json.Number
	if json.Unmarshal(raw, &arr) == nil && arr != nil {
		if len(arr) != 2 {
			return numOrPair{}, false
		}
		a, ok1 := new(big.Rat).SetString(arr[0].String())
		b, ok2 := new(big.Rat).SetString(arr[1].String())
		if !ok1 || !ok2 {
			return numOrPair{}, false
		}
		return numOrPair{pair: [2]*big.Rat{a, b}, isPair: true}, true
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil && n != "" {
		r, ok := new(big.Rat).SetString(n.String())
		return numOrPair{r: r}, ok
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return parseNumText(s)
	}
	return numOrPair{}, false
}

// thousandsRe matches numbers written with comma thousands separators.
var thousandsRe = regexp.MustCompile(`^-?d{1,3}(,d{3})+$`)

// parseNumText reads "42", "-3", "1,5" or "1.5" (spaces are ignored, so "1 000" works).
func parseNumText(s string) (numOrPair, bool) {
	s = strings.NewReplacer("00a0", "", "202f", "", " ", "").Replace(strings.TrimSpace(s))
	if thousandsRe.MatchString(s) { // English "1,200" is twelve hundred, not 1.2
		s = strings.ReplaceAll(s, ",", "")
	}
	s = strings.ReplaceAll(s, ",", ".")
	if s == "" || strings.ContainsAny(s, "/eE+") {
		return numOrPair{}, false
	}
	r, ok := new(big.Rat).SetString(s)
	return numOrPair{r: r}, ok
}

// equalNum compares two decoded answers exactly.
func equalNum(a, b numOrPair) bool {
	if a.isPair != b.isPair {
		return false
	}
	if a.isPair {
		return a.pair[0].Cmp(b.pair[0]) == 0 && a.pair[1].Cmp(b.pair[1]) == 0
	}
	return a.r != nil && b.r != nil && a.r.Cmp(b.r) == 0
}

// commonError looks for a typical wrong answer that matches a numeric answer.
func (q *Question) commonError(g numOrPair) (Result, bool) {
	for _, ce := range q.CommonErrors {
		if v, ok := parseNum(ce.Value); ok && equalNum(g, v) {
			return typical(ce.Misconception, ce.Feedback), true
		}
	}
	return Result{}, false
}

func (q *Question) checkNumeric(given json.RawMessage) Result {
	g, ok := parseNum(given)
	if !ok {
		return Result{Outcome: OutcomeRandomError}
	}
	if want, ok := parseNum(q.Answer.Value); ok && equalNum(g, want) {
		return Result{Outcome: OutcomeCorrect}
	}
	if r, ok := q.commonError(g); ok {
		return r
	}
	return Result{Outcome: OutcomeRandomError}
}

func (q *Question) checkRange(given json.RawMessage) Result {
	g, ok := parseNum(given)
	if !ok || g.isPair {
		return Result{Outcome: OutcomeRandomError}
	}
	lo, ok1 := new(big.Rat).SetString(q.Answer.Min.String())
	hi, ok2 := new(big.Rat).SetString(q.Answer.Max.String())
	if ok1 && ok2 && g.r.Cmp(lo) >= 0 && g.r.Cmp(hi) <= 0 {
		return Result{Outcome: OutcomeCorrect}
	}
	if r, ok := q.commonError(g); ok {
		return r
	}
	return Result{Outcome: OutcomeRandomError}
}

// ChoiceValue is how a choice is sent to the client and accepted back: a JSON number when its text is
// numeric ("15", "1,5" gives 1.5), else the text string in the language.
func ChoiceValue(c Choice, lang string) json.RawMessage {
	t := strings.TrimSpace(c.Text.Get(lang))
	if n, ok := parseNumText(t); ok && n.r != nil {
		return json.RawMessage(RatString(n.r))
	}
	b, _ := json.Marshal(t) // a string always marshals
	return b
}

// resolveChoice finds which choice the child picked: by id, by exact text in either language, or by number.
func (q *Question) resolveChoice(given json.RawMessage) (Choice, bool) {
	var s string
	if json.Unmarshal(given, &s) == nil {
		s = strings.TrimSpace(s)
		for _, c := range q.Choices {
			if strings.EqualFold(c.ID, s) {
				return c, true
			}
		}
		for _, c := range q.Choices {
			if strings.EqualFold(strings.TrimSpace(c.Text.FR), s) || strings.EqualFold(strings.TrimSpace(c.Text.EN), s) {
				return c, true
			}
		}
	}
	if g, ok := parseNum(given); ok && !g.isPair {
		for _, c := range q.Choices {
			for _, t := range []string{c.Text.FR, c.Text.EN} {
				if n, ok := parseNumText(t); ok && n.r != nil && n.r.Cmp(g.r) == 0 {
					return c, true
				}
			}
		}
	}
	return Choice{}, false
}

func (q *Question) checkChoice(given json.RawMessage) Result {
	c, ok := q.resolveChoice(given)
	if !ok {
		return Result{Outcome: OutcomeRandomError}
	}
	if c.ID == q.Answer.Correct {
		return Result{Outcome: OutcomeCorrect}
	}
	return typical(c.Misconception, LText{})
}

// parseBool accepts JSON true/false and the strings true/false/vrai/faux.
func parseBool(raw json.RawMessage) (bool, bool) {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b, true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "vrai":
			return true, true
		case "false", "faux":
			return false, true
		}
	}
	return false, false
}

func (q *Question) checkTrueFalse(given json.RawMessage) Result {
	g, ok := parseBool(given)
	want, wok := parseBool(q.Answer.Value)
	switch {
	case !ok || !wok:
		return Result{Outcome: OutcomeRandomError}
	case g == want:
		return Result{Outcome: OutcomeCorrect}
	}
	return typical(q.Answer.Misconception, LText{})
}

func (q *Question) checkOrder(given json.RawMessage) Result {
	var g []string
	if json.Unmarshal(given, &g) != nil {
		return Result{Outcome: OutcomeRandomError}
	}
	if slices.Equal(g, q.Answer.Sequence) {
		return Result{Outcome: OutcomeCorrect}
	}
	return typical(q.Answer.Misconception, LText{})
}

func (q *Question) checkMatch(given json.RawMessage) Result {
	var g [][2]string
	if json.Unmarshal(given, &g) != nil || len(g) == 0 || len(g) > maxPairSetSize {
		return Result{Outcome: OutcomeRandomError}
	}
	if len(g) != len(q.Answer.Pairs) {
		return typical(q.Answer.Misconception, LText{})
	}
	want := map[[2]string]bool{}
	for _, p := range q.Answer.Pairs {
		want[p] = true
	}
	for _, p := range g {
		if !want[p] {
			return typical(q.Answer.Misconception, LText{})
		}
		delete(want, p) // a repeated pair cannot count twice
	}
	return Result{Outcome: OutcomeCorrect}
}

// CorrectAnswer is the answer the server shows after grading, in the language of the request, as JSON.
func (q *Question) CorrectAnswer(lang string) json.RawMessage {
	switch q.Answer.Kind {
	case KindChoice:
		for _, c := range q.Choices {
			if c.ID == q.Answer.Correct {
				return ChoiceValue(c, lang)
			}
		}
	case KindOrder:
		b, _ := json.Marshal(q.Answer.Sequence)
		return b
	case KindMatch:
		b, _ := json.Marshal(q.Answer.Pairs)
		return b
	case KindRange:
		if q.Answer.ExactForCheck != "" {
			return json.RawMessage(q.Answer.ExactForCheck.String())
		}
		lo, ok1 := new(big.Rat).SetString(q.Answer.Min.String())
		hi, ok2 := new(big.Rat).SetString(q.Answer.Max.String())
		if ok1 && ok2 {
			mid := new(big.Rat).Add(lo, hi)
			return json.RawMessage(RatString(mid.Quo(mid, big.NewRat(2, 1))))
		}
	default: // numeric and truefalse keep their value as written
		if len(q.Answer.Value) > 0 {
			return q.Answer.Value
		}
	}
	return json.RawMessage("null")
}
