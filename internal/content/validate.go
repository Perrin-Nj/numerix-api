package content

// The validator (docs/CONTENT_PLAN.md section 3.5). The seed loader, the admin publish step, the admin
// "validate" endpoint and the tests all call the same functions, so a question that passes in one place
// passes everywhere.
//
// Go for Spring people: think of Env as the ConstraintValidatorContext (the lookups the rules need) and of
// Issue as a ConstraintViolation. A rule with a nil lookup in Env is skipped, which keeps unit tests small.

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Perrin-Nj/numerix-api/internal/narrator"
)

// Issue is one finding. Warning true means "print, do not block".
type Issue struct {
	Code    string `json:"code"`    // short stable name of the rule, for tests and the admin UI
	Field   string `json:"field"`   // where, for example "prompt.fr" or "choices[2]"
	Message string `json:"message"` // readable sentence
	Warning bool   `json:"warning,omitempty"`
}

// String formats an issue for logs and test output.
func (i Issue) String() string {
	kind := "error"
	if i.Warning {
		kind = "warning"
	}
	return fmt.Sprintf("%s %s [%s]: %s", kind, i.Code, i.Field, i.Message)
}

// HasErrors reports whether any issue blocks.
func HasErrors(is []Issue) bool {
	return slices.ContainsFunc(is, func(i Issue) bool { return !i.Warning })
}

// Errors keeps the blocking issues.
func Errors(is []Issue) []Issue {
	return slices.DeleteFunc(slices.Clone(is), func(i Issue) bool { return i.Warning })
}

// Codes lists the rule codes of the issues (handy in table tests).
func Codes(is []Issue) []string {
	out := make([]string, 0, len(is))
	for _, i := range is {
		out = append(out, i.Code)
	}
	return out
}

// SkillInfo is what the rules need to know about a curriculum skill.
type SkillInfo struct{ YearIntro int }

// QuestInfo is what the rules need to know about a quest.
type QuestInfo struct {
	World, Skill       string
	GradeMin, GradeMax int
	TargetCount        int
}

// Env carries the lookups the rules need. Any nil field turns its rules off.
type Env struct {
	Skill         func(id string) (SkillInfo, bool)
	Quest         func(id string) (QuestInfo, bool)
	Misconception func(code string) bool
	Asset         func(ref string) bool // explicit asset id or seed alias
	Vocab         func(facet, value string) bool
	Denylist      []string        // brand and rude words (folded)
	Names         map[string]bool // allowed first names and proper nouns (folded)
	SeedFile      bool            // true for files in seed/: "retired" is refused there
}

var (
	idRe        = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9-]+)*\.\d{3}$`)
	sentenceRe  = regexp.MustCompile(`[.!?…]+(?:\s+|$)`)
	timerRes    = compileAll(`\bvite\b`, `\bdepeche`, `compte a rebours`, `\bchrono`, `\bhurry`, `quick!`, `\bcountdown`, `seconds left`, `time is running out`)
	bannedKeys  = []string{"timer", "time_limit", "seconds"}
	visualKinds = []string{"bars", "pictograph", "clock", "numberline", "grid", "fraction", "dice"}
	validRoles  = regexp.MustCompile(`^(scene|object|visual|choice:[a-z0-9_-]+)$`)
)

// compileAll compiles several patterns once at start-up.
func compileAll(ps ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(ps))
	for i, p := range ps {
		out[i] = regexp.MustCompile(p)
	}
	return out
}

// bandLimits are the reading-level limits of plan 3.5 rule 9.
type bandLimits struct {
	story, prompt, promptSentences, sentence, hint, explain, option int
}

// limitsFor returns the limits of the grade band (1-2, 3-4, 5-6).
func limitsFor(grade int) bandLimits {
	switch {
	case grade <= 2:
		return bandLimits{25, 20, 2, 14, 14, 30, 8}
	case grade <= 4:
		return bandLimits{35, 30, 3, 18, 18, 40, 10}
	}
	return bandLimits{45, 40, 3, 22, 22, 50, 12}
}

// maxWordLetters is the longest word a young reader gets.
const maxWordLetters = 14

// textField is one user-visible text with its location.
type textField struct {
	name string // "prompt.fr"
	lang string // "fr", "en" or "" for language neutral text
	text string
	kind string // story, prompt, hint, explain, option, other
}

// texts lists every text a person reads, in a stable order.
func (q *Question) texts() []textField {
	var out []textField
	add := func(name, kind string, t LText) {
		out = append(out, textField{name + ".fr", "fr", t.FR, kind}, textField{name + ".en", "en", t.EN, kind})
	}
	add("story", "story", q.Story)
	add("prompt", "prompt", q.Prompt)
	add("hint", "hint", q.Hint)
	add("explain", "explain", q.Explain)
	if q.MathText != "" {
		out = append(out, textField{"math_text", "", q.MathText, "other"})
	}
	for i, c := range q.Choices {
		add(fmt.Sprintf("choices[%d].text", i), "option", c.Text)
	}
	if q.Items != nil {
		for i, it := range q.Items.List {
			add(fmt.Sprintf("items[%d].text", i), "option", it.Text)
		}
		for i, it := range q.Items.Left {
			add(fmt.Sprintf("items.left[%d].text", i), "option", it.Text)
		}
		for i, it := range q.Items.Right {
			add(fmt.Sprintf("items.right[%d].text", i), "option", it.Text)
		}
	}
	for i, ce := range q.CommonErrors {
		if !ce.Feedback.IsZero() {
			add(fmt.Sprintf("common_errors[%d].feedback", i), "other", ce.Feedback)
		}
	}
	return out
}

// Validate runs every per-question rule of plan 3.5 and returns errors and warnings.
func (e *Env) Validate(q *Question) []Issue {
	v := &run{env: e, q: q}
	v.structure()
	v.languages()
	v.answer()
	v.distractors()
	v.numericTruth()
	v.leaks()
	v.pressure()
	v.kidText()
	v.lengths()
	v.media()
	v.status()
	return v.issues
}

// run collects the issues of one Validate call.
type run struct {
	env    *Env
	q      *Question
	issues []Issue
}

func (v *run) err(code, field, format string, a ...any) {
	v.issues = append(v.issues, Issue{Code: code, Field: field, Message: fmt.Sprintf(format, a...)})
}

func (v *run) warn(code, field, format string, a ...any) {
	v.issues = append(v.issues, Issue{Code: code, Field: field, Message: fmt.Sprintf(format, a...), Warning: true})
}

// structure: rule 1 (ids, links, grade, type/kind pairing).
func (v *run) structure() {
	q := v.q
	if !idRe.MatchString(q.ID) {
		v.err("id_format", "id", "id %q must look like world.quest.007", q.ID)
	}
	if q.Quest == "" || !strings.HasPrefix(q.ID, q.Quest+".") {
		v.err("id_prefix", "id", "id %q must start with the quest id %q", q.ID, q.Quest)
	}
	if v.env.Quest != nil {
		if qi, ok := v.env.Quest(q.Quest); !ok {
			v.err("quest_unknown", "quest", "quest %q does not exist in quests.json", q.Quest)
		} else {
			if qi.Skill != q.Skill {
				v.err("skill_mismatch", "skill", "question skill %q differs from the quest skill %q", q.Skill, qi.Skill)
			}
			if q.Grade < qi.GradeMin || q.Grade > qi.GradeMax {
				v.err("grade_range", "grade", "grade %d is outside the quest range %d-%d", q.Grade, qi.GradeMin, qi.GradeMax)
			}
		}
	}
	if v.env.Skill != nil {
		if si, ok := v.env.Skill(q.Skill); !ok {
			v.err("skill_unknown", "skill", "skill %q is not in the curriculum graph", q.Skill)
		} else if q.Grade < si.YearIntro {
			v.err("grade_before_skill", "grade", "grade %d is before the skill is introduced (year %d)", q.Grade, si.YearIntro)
		}
	}
	if q.Grade < 1 || q.Grade > 6 {
		v.err("grade_value", "grade", "grade must be 1 to 6")
	}
	if q.Difficulty < 1 || q.Difficulty > 5 {
		v.err("difficulty_value", "difficulty", "difficulty must be 1 to 5")
	}
	kinds, ok := AllowedKinds[q.Type]
	switch {
	case !ok:
		v.err("type_unknown", "type", "unknown type %q", q.Type)
	case !slices.Contains(kinds, q.Answer.Kind):
		v.err("kind_for_type", "answer.kind", "type %s cannot use answer kind %q (allowed: %s)", q.Type, q.Answer.Kind, strings.Join(kinds, ", "))
	}
	if q.Type == TypeVisual && q.Visual == nil {
		v.err("visual_missing", "visual", "a visual question needs a visual block")
	}
	if q.Visual != nil && !slices.Contains(visualKinds, q.Visual.Kind) {
		v.err("visual_kind", "visual.kind", "unknown visual kind %q", q.Visual.Kind)
	}
}

// identicalAllowed reports whether fr and en may be the same text (no letters at all, like "12" or "3/4").
func identicalAllowed(s string) bool {
	return !strings.ContainsFunc(s, unicode.IsLetter)
}

// languages: rule 2.
func (v *run) languages() {
	q := v.q
	need := func(name string, t LText, sameOK bool) {
		if strings.TrimSpace(t.FR) == "" {
			v.err("missing_fr", name+".fr", "French text is required")
		}
		if strings.TrimSpace(t.EN) == "" {
			v.err("missing_en", name+".en", "English text is required")
		}
		if !sameOK && t.FR != "" && t.FR == t.EN && !identicalAllowed(t.FR) {
			v.err("fr_equals_en", name, "French and English are identical")
		}
	}
	need("story", q.Story, false)
	need("prompt", q.Prompt, false)
	need("hint", q.Hint, false)
	need("explain", q.Explain, false)
	for i, c := range q.Choices {
		need(fmt.Sprintf("choices[%d].text", i), c.Text, true) // a word like "triangle" is the same in both languages
	}
	if q.Items != nil {
		for i, it := range slices.Concat(q.Items.List, q.Items.Left, q.Items.Right) {
			need(fmt.Sprintf("items[%d].text", i), it.Text, true)
		}
	}
	for i, ce := range q.CommonErrors {
		need(fmt.Sprintf("common_errors[%d].feedback", i), ce.Feedback, false)
	}
}

// keyValue reads an item sort key ("3/4", "0.5", "12") as an exact number.
func keyValue(s string) (*big.Rat, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	if s == "" {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(s)
	return r, ok
}

// answer: rule 3 (the answer is among the options and well formed).
func (v *run) answer() {
	q, a := v.q, v.q.Answer
	switch a.Kind {
	case KindChoice:
		if len(q.Choices) < 3 || len(q.Choices) > 5 {
			v.err("choice_count", "choices", "a choice question needs 3 to 5 options, has %d", len(q.Choices))
		}
		seen := map[string]bool{}
		found := false
		for i, c := range q.Choices {
			if c.ID == "" || seen[c.ID] {
				v.err("choice_id", fmt.Sprintf("choices[%d].id", i), "choice ids must be non-empty and unique")
			}
			seen[c.ID] = true
			found = found || c.ID == a.Correct
		}
		if !found {
			v.err("answer_not_in_choices", "answer.correct", "correct id %q is not among the choices", a.Correct)
		}
	case KindTrueFalse:
		if _, ok := parseBool(a.Value); !ok {
			v.err("truefalse_value", "answer.value", "a true/false answer needs a boolean value")
		}
		if len(q.Choices) != 0 {
			v.err("truefalse_choices", "choices", "a true/false question has no choices (the two buttons are fixed)")
		}
	case KindOrder:
		v.order()
	case KindMatch:
		v.match()
	case KindNumeric:
		if _, ok := parseNum(a.Value); !ok {
			v.err("numeric_value", "answer.value", "a numeric answer needs a number or [quotient, remainder]")
		}
	case KindRange:
		lo, ok1 := new(big.Rat).SetString(a.Min.String())
		hi, ok2 := new(big.Rat).SetString(a.Max.String())
		if !ok1 || !ok2 || lo.Cmp(hi) > 0 {
			v.err("range_bounds", "answer", "a range answer needs numeric min <= max")
		}
	}
}

func (v *run) order() {
	q, a := v.q, v.q.Answer
	if q.Items == nil || len(q.Items.List) < 2 || len(q.Items.List) > 8 {
		v.err("order_items", "items", "an order question needs 2 to 8 items")
		return
	}
	ids := map[string]Item{}
	for _, it := range q.Items.List {
		if it.ID == "" {
			v.err("item_id", "items", "item ids must be non-empty")
		}
		if _, dup := ids[it.ID]; dup {
			v.err("item_id", "items", "duplicate item id %q", it.ID)
		}
		ids[it.ID] = it
	}
	seq := slices.Clone(a.Sequence)
	slices.Sort(seq)
	want := make([]string, 0, len(ids))
	for id := range ids {
		want = append(want, id)
	}
	slices.Sort(want)
	if !slices.Equal(seq, want) {
		v.err("order_permutation", "answer.sequence", "the sequence must list every item id exactly once")
		return
	}
	// when every item has a numeric key, the sequence must agree with the keys
	type kv struct {
		id string
		r  *big.Rat
	}
	var keyed []kv
	for _, id := range a.Sequence {
		r, ok := keyValue(ids[id].Key)
		if !ok {
			return
		}
		keyed = append(keyed, kv{id, r})
	}
	if a.By != "asc" && a.By != "desc" {
		v.err("order_by", "answer.by", "answer.by must be asc or desc when items have keys")
		return
	}
	for i := 1; i < len(keyed); i++ {
		c := keyed[i-1].r.Cmp(keyed[i].r)
		if (a.By == "asc" && c > 0) || (a.By == "desc" && c < 0) {
			v.err("order_keys", "answer.sequence", "the sequence does not follow the item keys (%s)", a.By)
			return
		}
	}
}

func (v *run) match() {
	q, a := v.q, v.q.Answer
	if q.Items == nil || len(q.Items.Left) < 2 || len(q.Items.Right) < 2 || len(q.Items.Left) > 6 {
		v.err("match_items", "items", "a match question needs 2 to 6 items on each side")
		return
	}
	left, right := map[string]bool{}, map[string]bool{}
	for _, it := range q.Items.Left {
		left[it.ID] = true
	}
	for _, it := range q.Items.Right {
		right[it.ID] = true
	}
	usedL, usedR := map[string]bool{}, map[string]bool{}
	for _, p := range a.Pairs {
		if !left[p[0]] || !right[p[1]] {
			v.err("match_unknown_id", "answer.pairs", "pair %v uses an unknown id", p)
			continue
		}
		if usedL[p[0]] {
			v.err("match_left_twice", "answer.pairs", "left id %q is paired twice", p[0])
		}
		if usedR[p[1]] {
			v.err("match_right_twice", "answer.pairs", "right id %q is paired twice", p[1])
		}
		usedL[p[0]], usedR[p[1]] = true, true
	}
	if len(usedL) != len(left) {
		v.err("match_incomplete", "answer.pairs", "every left item needs a pair")
	}
}

// distractors: rule 4.
func (v *run) distractors() {
	q := v.q
	known := func(code, field string) {
		if code != "" && v.env.Misconception != nil && !v.env.Misconception(code) {
			v.err("misconception_unknown", field, "misconception code %q does not exist", code)
		}
	}
	if len(q.Choices) > 0 {
		random := 0
		for _, lang := range []string{"fr", "en"} {
			seen := map[string]bool{}
			seenNum := map[string]bool{}
			for i, c := range q.Choices {
				t := Fold(strings.TrimSpace(c.Text.Get(lang)))
				if seen[t] {
					v.err("choice_duplicate", fmt.Sprintf("choices[%d].text.%s", i, lang), "two choices have the same text")
				}
				seen[t] = true
				if n, ok := parseNumText(c.Text.Get(lang)); ok && n.r != nil {
					key := n.r.String()
					if seenNum[key] {
						v.err("choice_same_value", fmt.Sprintf("choices[%d].text.%s", i, lang), "two choices have the same numeric value")
					}
					seenNum[key] = true
				}
			}
		}
		for i, c := range q.Choices {
			field := fmt.Sprintf("choices[%d]", i)
			if q.Answer.Kind == KindChoice && c.ID == q.Answer.Correct {
				if c.Misconception != "" || c.Random {
					v.err("correct_choice_labelled", field, "the correct choice must not carry a misconception")
				}
				continue
			}
			known(c.Misconception, field+".misconception")
			switch {
			case c.Random:
				random++
			case c.Misconception == "":
				v.err("distractor_unlabelled", field, "a wrong choice needs a misconception code or random: true")
			}
		}
		if random > 1 {
			v.err("too_many_random", "choices", "at most 1 random distractor, found %d", random)
		}
	}
	known(q.Answer.Misconception, "answer.misconception")
	for i, ce := range q.CommonErrors {
		known(ce.Misconception, fmt.Sprintf("common_errors[%d].misconception", i))
		if ce.Misconception == "" {
			v.err("common_error_code", fmt.Sprintf("common_errors[%d]", i), "a common error needs a misconception code")
		}
	}
}

// numericTruth: rule 5 (calc must agree with the stored answer; wrong values must differ from the truth).
func (v *run) numericTruth() {
	q, a := v.q, v.q.Answer
	switch a.Kind {
	case KindNumeric:
		want, ok := parseNum(a.Value)
		if !ok {
			return
		}
		if a.Calc == "" {
			v.warn("calc_missing", "answer.calc", "no calc: the server cannot verify this number")
		} else if got, err := Eval(a.Calc); err != nil {
			v.err("calc_invalid", "answer.calc", "%v", err)
		} else if !sameAsCalc(want, got) {
			v.err("calc_mismatch", "answer.calc", "calc %q gives %s but the answer is %s", a.Calc, valueString(got), string(a.Value))
		}
		for i, ce := range q.CommonErrors {
			if g, ok := parseNum(ce.Value); ok && equalNum(g, want) {
				v.err("common_error_is_truth", fmt.Sprintf("common_errors[%d].value", i), "a common error equals the right answer")
			}
		}
	case KindRange:
		lo, ok1 := new(big.Rat).SetString(a.Min.String())
		hi, ok2 := new(big.Rat).SetString(a.Max.String())
		ex, ok3 := new(big.Rat).SetString(a.ExactForCheck.String())
		if !ok1 || !ok2 || !ok3 {
			v.err("range_exact", "answer.exact_for_check", "a range answer needs exact_for_check")
			return
		}
		if ex.Cmp(lo) < 0 || ex.Cmp(hi) > 0 {
			v.err("range_exact_outside", "answer.exact_for_check", "exact_for_check must lie inside min..max")
		}
		width := new(big.Rat).Sub(hi, lo)
		limit := new(big.Rat).Mul(new(big.Rat).Abs(ex), big.NewRat(1, 5))
		if width.Cmp(limit) > 0 {
			v.err("range_too_wide", "answer", "max - min is more than 20 percent of the value")
		}
		if a.Calc != "" {
			if got, err := Eval(a.Calc); err != nil {
				v.err("calc_invalid", "answer.calc", "%v", err)
			} else if got.IsPair || got.R.Cmp(ex) != 0 {
				v.err("calc_mismatch", "answer.calc", "calc %q does not give exact_for_check", a.Calc)
			}
		}
		for i, ce := range q.CommonErrors {
			if g, ok := parseNum(ce.Value); ok && g.r != nil && g.r.Cmp(lo) >= 0 && g.r.Cmp(hi) <= 0 {
				v.err("common_error_is_truth", fmt.Sprintf("common_errors[%d].value", i), "a common error lies inside the right range")
			}
		}
	case KindChoice:
		for id, expr := range a.CalcChoice {
			var text string
			for _, c := range q.Choices {
				if c.ID == id {
					text = c.Text.FR
				}
			}
			got, err := Eval(expr)
			want, ok := parseNumText(text)
			switch {
			case err != nil:
				v.err("calc_invalid", "answer.calc_choice."+id, "%v", err)
			case !ok || !sameAsCalc(want, got):
				v.err("calc_mismatch", "answer.calc_choice."+id, "calc %q does not give the text of choice %q", expr, id)
			}
		}
		if len(a.CalcChoice) > 0 {
			if _, ok := a.CalcChoice[a.Correct]; !ok {
				v.err("calc_choice_correct", "answer.calc_choice", "calc_choice must prove the correct choice %q", a.Correct)
			}
		}
	}
}

// sameAsCalc compares a stored answer with a calc result.
func sameAsCalc(want numOrPair, got Value) bool {
	if want.isPair != got.IsPair {
		return false
	}
	if want.isPair {
		return want.pair[0].Cmp(got.Pair[0]) == 0 && want.pair[1].Cmp(got.Pair[1]) == 0
	}
	return want.r.Cmp(got.R) == 0
}

// valueString formats a calc result for messages.
func valueString(v Value) string {
	if v.IsPair {
		return "[" + RatString(v.Pair[0]) + ", " + RatString(v.Pair[1]) + "]"
	}
	return RatString(v.R)
}

// leakTokens is what must not appear before answering: whole numbers (digits and words) and phrases.
type leakTokens struct {
	ints    []int
	decimal []string    // "0.5", "0,5", "1/3"
	phrases [2][]string // folded phrases per language (fr, en)
}

// answerTokens lists everything that would give the answer away.
func (q *Question) answerTokens() leakTokens {
	var lt leakTokens
	addRat := func(r *big.Rat) {
		if r == nil {
			return
		}
		if r.IsInt() && r.Num().IsInt64() {
			n := int(r.Num().Int64())
			if n < 0 {
				n = -n
			}
			lt.ints = append(lt.ints, n)
			return
		}
		s := RatString(r)
		lt.decimal = append(lt.decimal, s, strings.ReplaceAll(s, ".", ","))
	}
	addNum := func(n numOrPair) {
		if n.isPair {
			addRat(n.pair[0])
			addRat(n.pair[1])
			return
		}
		addRat(n.r)
	}
	switch q.Answer.Kind {
	case KindNumeric:
		if n, ok := parseNum(q.Answer.Value); ok {
			addNum(n)
		}
	case KindRange:
		if r, ok := new(big.Rat).SetString(q.Answer.ExactForCheck.String()); ok {
			addRat(r)
		}
	case KindChoice:
		for _, c := range q.Choices {
			if c.ID != q.Answer.Correct {
				continue
			}
			for li, t := range []string{c.Text.FR, c.Text.EN} {
				if n, ok := parseNumText(t); ok {
					addNum(n)
				} else if f := Fold(strings.TrimSpace(t)); utf8.RuneCountInString(f) >= 3 {
					lt.phrases[li] = append(lt.phrases[li], f)
				}
			}
		}
	}
	return lt
}

// containsPhrase reports whether the folded text holds the folded phrase as whole words.
func containsPhrase(foldedText, foldedPhrase string) bool {
	return regexp.MustCompile(`(^|[^a-z0-9])` + regexp.QuoteMeta(foldedPhrase) + `([^a-z0-9]|$)`).MatchString(foldedText)
}

// leaksIn reports whether the text reveals the answer, and what matched.
func (lt leakTokens) leaksIn(text, lang string) (string, bool) {
	folded := Fold(text)
	for _, n := range lt.ints {
		if narrator.Leaks(text, []string{strconv.Itoa(n)}) {
			return strconv.Itoa(n), true
		}
		for _, w := range NumberWords(n, lang) {
			if containsPhrase(folded, Fold(w)) {
				return w, true
			}
		}
	}
	for _, d := range lt.decimal {
		if strings.Contains(text, d) {
			return d, true
		}
	}
	li := 0
	if lang == "en" {
		li = 1
	}
	for _, p := range lt.phrases[li] {
		if containsPhrase(folded, p) {
			return p, true
		}
	}
	return "", false
}

// visualStrings collects every string value of a visual block (labels, titles).
func visualStrings(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, x)
	case []any:
		for _, e := range x {
			visualStrings(e, out)
		}
	case map[string]any:
		for _, e := range x {
			visualStrings(e, out)
		}
	}
}

// leaks: rule 6 (no answer in the text shown before answering).
func (v *run) leaks() {
	q := v.q
	if q.AllowOperandEqualsAnswer {
		return
	}
	if q.Answer.Kind == KindTrueFalse || q.Answer.Kind == KindOrder || q.Answer.Kind == KindMatch {
		return // the statement and the cards are the question; there is no hidden value
	}
	lt := q.answerTokens()
	check := func(field, text, lang string) {
		if tok, bad := lt.leaksIn(text, lang); bad {
			v.err("answer_leak", field, "the text shows the answer (%s) before the child answers", tok)
		}
	}
	for _, f := range []textField{
		{"story.fr", "fr", q.Story.FR, ""}, {"story.en", "en", q.Story.EN, ""},
		{"prompt.fr", "fr", q.Prompt.FR, ""}, {"prompt.en", "en", q.Prompt.EN, ""},
		{"hint.fr", "fr", q.Hint.FR, ""}, {"hint.en", "en", q.Hint.EN, ""},
	} {
		check(f.name, f.text, f.lang)
	}
	if q.MathText != "" {
		check("math_text", q.MathText, "fr")
	}
	if q.Visual != nil {
		var labels []string
		visualStrings(q.Visual.Data, &labels)
		for _, l := range labels {
			check("visual.data", l, "fr")
			check("visual.data", l, "en")
		}
	}
}

// walkKeys calls fn for every map key inside a decoded JSON value.
func walkKeys(v any, fn func(string)) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			fn(k)
			walkKeys(e, fn)
		}
	case []any:
		for _, e := range x {
			walkKeys(e, fn)
		}
	}
}

// pressure: rule 7 (no timers, no hurry words).
func (v *run) pressure() {
	q := v.q
	if q.Visual != nil {
		walkKeys(q.Visual.Data, func(k string) {
			if slices.Contains(bannedKeys, strings.ToLower(k)) {
				v.err("timer_key", "visual.data."+k, "no timers: the key %q is not allowed", k)
			}
		})
	}
	timeSkill := strings.HasPrefix(q.Skill, "MES-G") || (q.Visual != nil && q.Visual.Kind == "clock")
	for _, f := range q.texts() {
		folded := Fold(f.text)
		for _, re := range timerRes {
			if !re.MatchString(folded) {
				continue
			}
			if timeSkill && strings.Contains(re.String(), "chrono") {
				continue // a clock lesson may talk about a stopwatch
			}
			v.err("pressure_word", f.name, "wording that creates time pressure (%s)", re.String())
		}
	}
}

// isEmoji reports whether r is a pictograph (art belongs in media, not in text).
func isEmoji(r rune) bool {
	return (r >= 0x1F000 && r <= 0x1FAFF) || (r >= 0x2600 && r <= 0x27BF) || r == 0xFE0F || (r >= 0x2B00 && r <= 0x2BFF)
}

// kidText: rule 8 (dashes, emoji, deny list, names).
func (v *run) kidText() {
	q := v.q
	for _, f := range q.texts() {
		if strings.ContainsAny(f.text, "—–") {
			v.err("dash", f.name, "no em dash or en dash in kid text")
		}
		if strings.ContainsFunc(f.text, isEmoji) {
			v.err("emoji", f.name, "no emoji inside text (use media)")
		}
		words := strings.FieldsFunc(Fold(f.text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		joined := " " + strings.Join(words, " ") + " "
		for _, d := range v.env.Denylist {
			if strings.Contains(joined, " "+d+" ") {
				v.err("denylist", f.name, "word %q is not allowed in kid text", d)
			}
		}
		if v.env.Names != nil && (f.kind == "story" || f.kind == "prompt" || f.kind == "hint") {
			for _, w := range properNouns(f.text) {
				if !v.env.Names[Fold(w)] {
					v.warn("unknown_name", f.name, "capitalised word %q is not in names.txt", w)
				}
			}
		}
	}
}

// properNouns returns capitalised words that do not start a sentence.
func properNouns(text string) []string {
	var out []string
	start := true
	for _, tok := range strings.Fields(text) {
		word := strings.TrimFunc(tok, func(r rune) bool { return !unicode.IsLetter(r) })
		if word != "" && !start && unicode.IsUpper([]rune(word)[0]) && utf8.RuneCountInString(word) > 1 {
			out = append(out, word)
		}
		start = strings.HasSuffix(tok, ".") || strings.HasSuffix(tok, "!") || strings.HasSuffix(tok, "?") || strings.HasSuffix(tok, "…")
	}
	return out
}

// wordCount counts words (tokens that contain a letter or a digit).
func wordCount(s string) int {
	n := 0
	for _, t := range strings.Fields(s) {
		if strings.ContainsFunc(t, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
			n++
		}
	}
	return n
}

// sentences splits a text into sentences.
func sentences(s string) []string {
	parts := sentenceRe.Split(strings.TrimSpace(s), -1)
	return slices.DeleteFunc(parts, func(p string) bool { return strings.TrimSpace(p) == "" })
}

// lengths: rule 9.
func (v *run) lengths() {
	q := v.q
	lim := limitsFor(q.Grade)
	for _, f := range q.texts() {
		limit := 0
		switch f.kind {
		case "story":
			limit = lim.story
		case "prompt":
			limit = lim.prompt
		case "hint":
			limit = lim.hint
		case "explain":
			limit = lim.explain
		case "option":
			limit = lim.option
		}
		if limit > 0 {
			if n := wordCount(f.text); n > limit {
				v.err("too_long", f.name, "%d words, the limit is %d for grade %d", n, limit, q.Grade)
			}
		}
		if f.kind == "story" || f.kind == "prompt" || f.kind == "hint" || f.kind == "explain" {
			ss := sentences(f.text)
			if f.kind == "prompt" && len(ss) > lim.promptSentences {
				v.err("too_many_sentences", f.name, "%d sentences, the limit is %d", len(ss), lim.promptSentences)
			}
			for _, s := range ss {
				if n := wordCount(s); n > lim.sentence {
					v.err("sentence_too_long", f.name, "a sentence has %d words, the limit is %d", n, lim.sentence)
					break
				}
			}
		}
		for _, w := range strings.FieldsFunc(f.text, func(r rune) bool { return !unicode.IsLetter(r) }) {
			if utf8.RuneCountInString(w) > maxWordLetters {
				v.err("word_too_long", f.name, "word %q has more than %d letters", w, maxWordLetters)
			}
		}
	}
	if len(q.Tags) < 2 {
		v.warn("few_tags", "tags", "add at least 2 tags")
	}
	if q.Hint.FR != "" && q.Hint.FR == q.Explain.FR {
		v.warn("hint_equals_explain", "hint", "hint is identical to explain")
	}
}

// media: rule 10.
func (v *run) media() {
	q := v.q
	if len(q.Media) > 3 {
		v.err("media_count", "media", "at most 3 media per question, found %d", len(q.Media))
	}
	for i, m := range q.Media {
		field := fmt.Sprintf("media[%d]", i)
		if !validRoles.MatchString(m.Role) {
			v.err("media_role", field+".role", "role %q must be scene, object, visual or choice:<id>", m.Role)
		}
		if after, ok := strings.CutPrefix(m.Role, "choice:"); ok && !slices.ContainsFunc(q.Choices, func(c Choice) bool { return c.ID == after }) {
			v.err("media_role", field+".role", "role %q names a choice that does not exist", m.Role)
		}
		switch {
		case m.Asset == "" && m.Criteria == nil:
			v.err("media_empty", field, "a media entry needs an asset or criteria")
		case m.Asset != "" && v.env.Asset != nil && !v.env.Asset(m.Asset):
			v.err("media_asset", field+".asset", "asset %q does not exist", m.Asset)
		}
		if c := m.Criteria; c != nil {
			if c.Kind == "" || (c.World == "" && c.Topic == "" && c.Object == "") {
				v.err("media_criteria", field+".criteria", "criteria need kind and one of world, topic, object")
			}
			if v.env.Vocab != nil {
				for facet, val := range map[string]string{"world": c.World, "topic": c.Topic, "object": c.Object, "mood": c.Mood, "setting": c.Setting} {
					if val != "" && !v.env.Vocab(facet, val) {
						v.err("media_vocab", field+".criteria."+facet, "%q is not in the %s vocabulary", val, facet)
					}
				}
			}
		}
	}
}

// status: rule 11.
func (v *run) status() {
	q := v.q
	switch q.Status {
	case StatusDraft, StatusReview, StatusPublished:
	case StatusRetired:
		if v.env.SeedFile {
			v.err("status_retired", "status", "retired never appears in a seed file (remove the question instead)")
		}
	default:
		v.err("status_value", "status", "status must be draft, review or published")
	}
	if q.AIDrafted && q.Status == StatusPublished && q.ReviewedBy == "" {
		v.err("ai_unreviewed", "status", "an AI draft cannot be published without reviewed_by")
	}
	if q.Version < 0 {
		v.err("version_value", "version", "version cannot be negative")
	}
}

// ValidateJSON decodes strictly then validates (an unknown field is an error). It returns the question when it decoded.
func (e *Env) ValidateJSON(raw []byte) (*Question, []Issue) {
	var q Question
	if err := DecodeStrict(raw, &q); err != nil {
		return nil, []Issue{{Code: "json", Field: "$", Message: err.Error()}}
	}
	return &q, e.Validate(&q)
}

// ValidateSet runs the cross-question rules: unique ids, near duplicate prompts, type variety per quest.
func (e *Env) ValidateSet(qs []*Question) []Issue {
	var out []Issue
	seen := map[string]bool{}
	byQuest := map[string][]*Question{}
	for _, q := range qs {
		if seen[q.ID] {
			out = append(out, Issue{Code: "id_duplicate", Field: q.ID, Message: "id appears more than once"})
		}
		seen[q.ID] = true
		byQuest[q.Quest] = append(byQuest[q.Quest], q)
	}
	for quest, list := range byQuest {
		types := map[string]bool{}
		for _, q := range list {
			types[q.Type] = true
		}
		if len(types) < 4 {
			out = append(out, Issue{Code: "quest_type_variety", Field: quest, Message: fmt.Sprintf("only %d distinct types, aim for 4 or more", len(types)), Warning: true})
		}
		if e.Quest != nil {
			if qi, ok := e.Quest(quest); ok && len(list) < qi.TargetCount-2 {
				out = append(out, Issue{Code: "quest_below_target", Field: quest, Message: fmt.Sprintf("%d questions, target is %d", len(list), qi.TargetCount), Warning: true})
			}
		}
		for i := range list {
			for j := i + 1; j < len(list); j++ {
				if nearDuplicate(list[i].Prompt.FR, list[j].Prompt.FR) {
					out = append(out, Issue{Code: "near_duplicate", Field: list[i].ID + "," + list[j].ID, Message: "prompts are nearly identical", Warning: true})
				}
			}
		}
	}
	return out
}

// nearDuplicate is true when the edit distance is under 25 percent of the longer text.
func nearDuplicate(a, b string) bool {
	ra, rb := []rune(Fold(a)), []rune(Fold(b))
	longest := max(len(ra), len(rb))
	if longest == 0 || absInt(len(ra)-len(rb))*4 >= longest {
		return false
	}
	return levenshtein(ra, rb)*4 < longest
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// levenshtein is the classic edit distance with two rows of memory.
func levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// Type quotas of a whole published world (plan 2.2).
var typeQuota = map[string]int{TypeNumeric: 10, TypeChoice: 10, TypeWord: 8, TypeTrueFalse: 5, TypeOrder: 5, TypeMatch: 5, TypeVisual: 5, TypeEstimate: 4}

// WorldQuota checks a world file. It only reports errors when every question is published (a work in
// progress file is allowed to be incomplete); quotas then become hard errors.
func (e *Env) WorldQuota(world string, qs []*Question, quests []Quest) []Issue {
	all := len(qs) > 0
	for _, q := range qs {
		all = all && q.Status == StatusPublished
	}
	var out []Issue
	add := func(code, field, msg string) {
		out = append(out, Issue{Code: code, Field: field, Message: msg, Warning: !all})
	}
	counts := map[string]int{}
	perQuest := map[string]int{}
	for _, q := range qs {
		counts[q.Type]++
		if q.Status == StatusPublished {
			perQuest[q.Quest]++
		}
	}
	for _, t := range AllTypes {
		if counts[t] < typeQuota[t] {
			add("world_type_quota", world, fmt.Sprintf("type %s has %d questions, the quota is %d", t, counts[t], typeQuota[t]))
		}
	}
	for _, qu := range quests {
		if qu.World == world && perQuest[qu.ID] < qu.TargetCount-2 {
			add("world_quest_quota", qu.ID, fmt.Sprintf("%d published, needs at least %d", perQuest[qu.ID], qu.TargetCount-2))
		}
	}
	return out
}

// MarshalIssues renders issues as JSON for CLI output and API replies.
func MarshalIssues(is []Issue) json.RawMessage {
	if is == nil {
		is = []Issue{}
	}
	b, _ := json.Marshal(is) // a slice of plain structs always marshals
	return b
}
