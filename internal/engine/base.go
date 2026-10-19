package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Outcome is the three-way verdict that drives the story (never a red cross).
type Outcome string

// The three outcomes of an answer.
const (
	Correct         Outcome = "correct"
	ErreurTypique   Outcome = "erreur_typique"
	ErreurAleatoire Outcome = "erreur_aleatoire"
)

// Value is an answer: an int, a string or a []int{quotient, remainder}. (Python: int | str | tuple.)
type Value = any

// Result is what the narrative layer receives after an answer.
type Result struct {
	Type              Outcome `json:"type"`
	ReponseDonnee     Value   `json:"reponse_donnee"`
	ReponseCorrecte   Value   `json:"reponse_correcte"`
	CodeErreur        string  `json:"code_erreur,omitempty"`
	DescriptionErreur string  `json:"description_erreur,omitempty"`
	CompetenceID      string  `json:"competence_id"`
}

// Distractor is a wrong answer built from a known error.
type Distractor struct {
	Valeur      Value  `json:"valeur"`
	CodeErreur  string `json:"code_erreur"`
	Description string `json:"description"`
}

// VisualHint tells the visual builder which picture fits this question (kind and integer params).
type VisualHint struct {
	Kind   string         `json:"kind"`
	Params map[string]int `json:"params"`
}

// Problem is one generated question with its answer. It is stored in the question bank as JSON.
// The answer fields never leave the server before grading.
type Problem struct {
	CompetenceID     string         `json:"competence_id"`
	Generateur       string         `json:"generateur"`
	EnonceMath       string         `json:"enonce_math"`
	Reponse          Value          `json:"reponse"`
	Distracteurs     []Distractor   `json:"distracteurs"`
	Choix            []Value        `json:"choix"`
	ContexteNarratif map[string]any `json:"contexte_narratif"`
	ClesReponse      []string       `json:"cles_reponse"`
	Difficulte       float64        `json:"difficulte"`
	Metadonnees      map[string]any `json:"metadonnees"`
	Shape            string         `json:"shape"`            // exercise family, so consecutive beats differ
	Visual           *VisualHint    `json:"visual,omitempty"` // picture hint, nil when no picture exists
	// Curated holds the whole curated bank question (package content JSON) when this row was projected from the
	// bank. The engine never reads it: the app grades and renders curated rows from it. nil for generated rows.
	Curated json.RawMessage `json:"curated,omitempty"`
}

// Normalize repairs types after json.Unmarshal (JSON numbers arrive as float64, lists as []any).
// Spring: like a custom Jackson deserializer, but explicit.
func (p *Problem) Normalize() {
	p.Reponse = NormValue(p.Reponse)
	for i := range p.Choix {
		p.Choix[i] = NormValue(p.Choix[i])
	}
	for i := range p.Distracteurs {
		p.Distracteurs[i].Valeur = NormValue(p.Distracteurs[i].Valeur)
	}
	// numbers inside the narrative context come back as float64 too
	for k, v := range p.ContexteNarratif {
		if f, ok := v.(float64); ok && f == math.Trunc(f) {
			p.ContexteNarratif[k] = int(f)
		}
	}
}

// NormValue turns decoded JSON values into int, string or []int so comparisons are exact.
func NormValue(v Value) Value {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) {
			return int(x) // 42.0 -> 42
		}
		return x
	case []any:
		out := make([]int, 0, len(x))
		for _, e := range x {
			if f, ok := e.(float64); ok {
				out = append(out, int(f))
			}
		}
		return out
	}
	return v
}

// asFloat reads a numeric value (int, float64 or numeric text) and reports whether it was numeric.
func asFloat(v Value) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(x, ",", ".")), 64)
		return f, err == nil
	}
	return 0, false
}

// Equivalent compares two answers (port of Probleme._equivalent): lists exactly, numbers within 1e-9,
// otherwise trimmed case-insensitive text.
func Equivalent(a, b Value) bool {
	a, b = NormValue(a), NormValue(b)
	la, aList := a.([]int)
	lb, bList := b.([]int)
	if aList || bList {
		return aList && bList && len(la) == len(lb) && sliceEq(la, lb)
	}
	fa, okA := asFloat(a)
	fb, okB := asFloat(b)
	if okA && okB {
		return math.Abs(fa-fb) < 1e-9
	}
	return strings.EqualFold(strings.TrimSpace(fmt.Sprint(a)), strings.TrimSpace(fmt.Sprint(b))) // case-insensitive compare
}

// sliceEq compares two equally long int slices.
func sliceEq(a, b []int) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Evaluate grades an answer. Single entry point, never an LLM.
func (p *Problem) Evaluate(given Value) Result {
	res := Result{ReponseDonnee: given, ReponseCorrecte: p.Reponse, CompetenceID: p.CompetenceID}
	if Equivalent(given, p.Reponse) {
		res.Type = Correct
		return res
	}
	for _, d := range p.Distracteurs {
		if Equivalent(given, d.Valeur) {
			res.Type, res.CodeErreur, res.DescriptionErreur = ErreurTypique, d.CodeErreur, d.Description
			return res
		}
	}
	res.Type = ErreurAleatoire
	return res
}

// ForLLMScene is the payload sent to the narrator BEFORE the answer. Keys listed in ClesReponse are removed,
// so the narrator cannot reveal the answer even by accident.
func (p *Problem) ForLLMScene() map[string]any {
	ctx := map[string]any{}
	for k, v := range p.ContexteNarratif {
		if !contains(p.ClesReponse, k) {
			ctx[k] = v
		}
	}
	return map[string]any{
		"phase": "mise_en_scene", "enonce_math": p.EnonceMath,
		"contexte_narratif": ctx, "choix_affiches": p.Choix,
	}
}

// consignes is the story instruction per outcome (French, as in the Python engine).
var consignes = map[Outcome]string{
	Correct:         "Le héros réussit. Ce que l'enfant a calculé rend l'action possible.",
	ErreurTypique:   "Complication narrative ciblée sur cette erreur précise. Jamais un échec — un détour qui crée l'envie de réessayer.",
	ErreurAleatoire: "Un imprévu relance la situation. Ni jugement, ni correction explicite.",
}

// ForLLMDenouement is the payload AFTER the answer. With hideAnswer true the answer keys are removed again
// (phase 1 rule: the narrator never receives the answer; the verdict alone is enough to tell the story).
func (p *Problem) ForLLMDenouement(res Result, hideAnswer bool) map[string]any {
	ctx := map[string]any{}
	for k, v := range p.ContexteNarratif {
		if hideAnswer && contains(p.ClesReponse, k) {
			continue
		}
		ctx[k] = v
	}
	return map[string]any{
		"phase": "denouement", "issue": string(res.Type), "contexte_narratif": ctx,
		"code_erreur": res.CodeErreur, "nature_de_l_erreur": res.DescriptionErreur, "consigne": consignes[res.Type],
	}
}

var numberRe = regexp.MustCompile(`-?\d+(?:[.,]\d+)?`)

// numbersOf returns the numbers written in a statement (what the child can read anyway).
func numbersOf(s string) map[float64]bool {
	out := map[float64]bool{}
	for _, m := range numberRe.FindAllString(strings.ReplaceAll(s, ",", "."), -1) {
		if f, err := strconv.ParseFloat(m, 64); err == nil {
			out[f] = true
		}
	}
	return out
}

// LeakDetected is the leak check: it returns the keys of the scene payload that reveal the answer.
// It must always be empty. A value already written in the statement is not a leak (port of fuite_detectee).
func (p *Problem) LeakDetected() []string {
	visible := numbersOf(p.EnonceMath)
	var bad []string
	scene := p.ForLLMScene()["contexte_narratif"].(map[string]any)
	for k, v := range scene {
		if !leakEquivalent(v, p.Reponse) {
			continue
		}
		if f, ok := v.(int); ok && visible[float64(f)] {
			continue // data from the statement, not a leak
		}
		bad = append(bad, k)
	}
	return bad
}

// leakEquivalent mirrors _equivalent_pour_fuite.
func leakEquivalent(val, rep Value) bool {
	rep = NormValue(rep)
	if r, ok := rep.([]int); ok {
		v, ok := NormValue(val).([]int)
		return ok && len(v) == len(r) && sliceEq(v, r)
	}
	switch val.(type) {
	case map[string]any, []any, []int:
		return false
	}
	return Equivalent(val, rep)
}

// contains reports whether xs holds s.
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// key makes a comparable string for dedup (port of _cle).
func key(v Value) string { return fmt.Sprintf("%v", NormValue(v)) }

// AssembleChoices builds the choices shown to the child: no duplicate, no distractor equal to the answer,
// exactly nb options, shuffled. minBound nil means no lower bound. Port of assembler_choix.
func AssembleChoices(g *RNG, answer Value, ds []Distractor, nb int, minBound *int) ([]Value, []Distractor, error) {
	var kept []Distractor
	seen := map[string]bool{key(answer): true}
	for _, d := range ds {
		if seen[key(d.Valeur)] {
			continue
		}
		if f, ok := d.Valeur.(int); ok && minBound != nil && f < *minBound {
			continue
		}
		seen[key(d.Valeur)] = true
		kept = append(kept, d)
		if len(kept) == nb-1 {
			break
		}
	}
	switch a := NormValue(answer).(type) {
	case int: // numeric filler: neighbours at growing distance
		for gap := 1; len(kept) < nb-1 && gap < 40; gap++ {
			for _, sign := range []int{1, -1} {
				cand := a + sign*gap
				if (minBound != nil && cand < *minBound) || seen[key(cand)] {
					continue
				}
				seen[key(cand)] = true
				kept = append(kept, Distractor{cand, "ecart_proche", fmt.Sprintf("Écart de %d par rapport au résultat", gap)})
				if len(kept) == nb-1 {
					break
				}
			}
		}
	case []int: // (quotient, remainder) filler
		if len(a) == 2 {
			q, r := a[0], a[1]
			cands := []Distractor{
				{[]int{q + 1, r}, "ecart_quotient", "Quotient décalé de 1"},
				{[]int{q - 1, r}, "ecart_quotient", "Quotient décalé de 1"},
				{[]int{q, r + 1}, "ecart_reste", "Reste décalé de 1"},
				{[]int{q, max(r-1, 0)}, "ecart_reste", "Reste décalé de 1"},
				{[]int{q + 1, max(r-1, 0)}, "ecart_mixte", "Quotient et reste tous deux décalés"},
			}
			for _, c := range cands {
				v := c.Valeur.([]int)
				if len(kept) == nb-1 {
					break
				}
				if v[0] < 0 || v[1] < 0 || seen[key(v)] {
					continue
				}
				seen[key(v)] = true
				kept = append(kept, c)
			}
		}
	}
	if len(kept) < nb-1 {
		return nil, nil, fmt.Errorf("cannot assemble %d distinct choices for answer %v: only %d valid distractors", nb, answer, len(kept))
	}
	choices := []Value{NormValue(answer)}
	for _, d := range kept {
		choices = append(choices, d.Valeur)
	}
	g.Shuffle(len(choices), func(i, j int) { choices[i], choices[j] = choices[j], choices[i] })
	return choices, kept, nil
}

// boundByCycle returns the curriculum ceiling: cycle 1 uses c1, cycle 2 uses c2.
func boundByCycle(cycle, c1, c2 int) int {
	if cycle <= 1 {
		return c1
	}
	return c2
}

// scale normalises v into [0,1] between lo and hi for the difficulty field.
func scale(v, lo, hi float64) float64 {
	if hi <= lo {
		return 0.5
	}
	return math.Max(0, math.Min(1, (v-lo)/(hi-lo)))
}

// zeroBound is the "no negative distractor" lower bound used by most generators.
func zeroBound() *int { z := 0; return &z }

// AnswerTokens lists the strings that must never appear in a narration: the answer's numbers and phrases,
// except numbers already written in the statement (the child reads those anyway).
func (p *Problem) AnswerTokens() []string {
	visible := numbersOf(p.EnonceMath)
	var out []string
	add := func(n int) {
		if !visible[float64(n)] {
			out = append(out, strconv.Itoa(n))
		}
	}
	switch a := NormValue(p.Reponse).(type) {
	case int:
		add(a)
	case []int:
		for _, n := range a {
			add(n)
		}
	case string:
		out = append(out, a)
	}
	return out
}
