package engine

import (
	"fmt"
	"math"
	"sort"
)

// Params carries generator settings from the curriculum JSON (and caller overrides).
// Go for Spring people: a map[string]any is like a Map<String,Object>; the helpers below do the casts.
type Params map[string]any

// Int reads an integer setting (JSON numbers are float64), or def when absent.
func (p Params) Int(k string, def int) int {
	switch v := p[k].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return def
}

// Str reads a string setting, or def when absent.
func (p Params) Str(k, def string) string {
	if v, ok := p[k].(string); ok {
		return v
	}
	return def
}

// Strings reads a list of strings; a single string becomes a one-item list. Nil when absent.
func (p Params) Strings(k string) []string {
	switch v := p[k].(type) {
	case string:
		return []string{v}
	case []string:
		return v
	case []any:
		var out []string
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// Ints reads a list of ints. Nil when absent.
func (p Params) Ints(k string) []int {
	var out []int
	switch v := p[k].(type) {
	case []int:
		return v
	case []any:
		for _, e := range v {
			if f, ok := e.(float64); ok {
				out = append(out, int(f))
			}
		}
	}
	return out
}

// Float reads an optional float setting.
func (p Params) Float(k string) (float64, bool) {
	switch v := p[k].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	}
	return 0, false
}

// Bool reads an optional bool setting.
func (p Params) Bool(k string) (bool, bool) {
	v, ok := p[k].(bool)
	return v, ok
}

// d is a short constructor for a Distractor.
func d(v Value, code, desc string) Distractor {
	return Distractor{Valeur: v, CodeErreur: code, Description: desc}
}

// shuffleDistractors shuffles in place (the order decides which ones survive the 3-of-n cut).
func shuffleDistractors(g *RNG, ds []Distractor) {
	g.Shuffle(len(ds), func(i, j int) { ds[i], ds[j] = ds[j], ds[i] })
}

// ───────────── 1. gen_faits_addition: ARI-OPE-A02a / A02c ─────────────

// genFaitsAddition builds an addition fact (0+0 .. 10+10) or the matching subtraction.
// Typical errors: off by one (counting on), neighbouring fact, wrong operation.
func genFaitsAddition(g *RNG, p Params) (Problem, error) {
	termesMax := p.Int("termes_max", 10)
	mode := p.Str("mode", "construction")
	op := p.Str("operation", "")
	if op == "" {
		op = g.ChooseString([]string{"addition", "soustraction"})
	}
	target, hasTarget := p.Float("difficulte_cible")
	a, b := drawAdditiveTerms(g, termesMax, target, hasTarget)

	var answer int
	var enonce string
	var ctx map[string]any
	var ds []Distractor
	var visual *VisualHint
	if op == "addition" {
		answer = a + b
		enonce = fmt.Sprintf("%d + %d = ?", a, b)
		ctx = map[string]any{"operation": "addition", "sens": "reunion", "terme_1": a, "terme_2": b, "resultat": answer, "role_resultat": "total"}
		ds = []Distractor{
			d(answer+1, "erreur_plus_un", "Résultat décalé de 1 vers le haut (surcomptage)"),
			d(answer-1, "erreur_plus_un", "Résultat décalé de 1 vers le bas (surcomptage)"),
			d(a+b+2, "confusion_proche", "Confond avec le fait numérique voisin"),
			d(absInt(a-b), "operation_inverse", "Soustrait au lieu d'additionner"),
		}
		visual = &VisualHint{"add_carry", map[string]int{"a": a, "b": b}}
	} else {
		total := a + b
		answer = a
		enonce = fmt.Sprintf("%d − %d = ?", total, b)
		ctx = map[string]any{"operation": "soustraction", "sens": "retrait", "depart": total, "retire": b, "resultat": answer, "role_resultat": "reste"}
		ds = []Distractor{
			d(answer+1, "erreur_plus_un", "Résultat décalé de 1 (surcomptage)"),
			d(answer-1, "erreur_plus_un", "Résultat décalé de 1 (surcomptage)"),
			d(total+b, "operation_inverse", "Additionne au lieu de soustraire"),
			d(b, "confusion_proche", "Donne le terme retiré au lieu du reste"),
		}
		visual = &VisualHint{"sub_borrow", map[string]int{"a": total, "b": b}}
	}
	shuffleDistractors(g, ds)
	choices, kept, err := AssembleChoices(g, answer, ds, 4, zeroBound())
	if err != nil {
		return Problem{}, err
	}
	cross := a+b > 10 && a < 10 && b < 10
	var targetMs any
	if mode == "maitrise" {
		targetMs = 4000
	}
	id := "ARI-OPE-A02a"
	if mode == "maitrise" {
		id = "ARI-OPE-A02c"
	}
	return Problem{
		CompetenceID: id, Generateur: "gen_faits_addition", EnonceMath: enonce, Reponse: answer,
		Distracteurs: kept, Choix: choices, ContexteNarratif: ctx, ClesReponse: []string{"resultat"},
		Difficulte:  additiveDifficulty(a, b),
		Metadonnees: map[string]any{"mode": mode, "franchit_dix": cross, "est_double": a == b, "temps_cible_ms": targetMs},
		Shape:       fmt.Sprintf("faits_add:%s:%t", op, cross), Visual: visual,
	}, nil
}

// drawAdditiveTerms picks two terms, aiming at the requested difficulty when given.
func drawAdditiveTerms(g *RNG, termesMax int, target float64, hasTarget bool) (int, int) {
	if !hasTarget {
		return g.Int(0, termesMax), g.Int(0, termesMax)
	}
	type pair struct{ a, b int }
	var cands []pair
	for a := 0; a <= termesMax; a++ {
		for b := 0; b <= termesMax; b++ {
			cands = append(cands, pair{a, b})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		return math.Abs(additiveDifficulty(cands[i].a, cands[i].b)-target) < math.Abs(additiveDifficulty(cands[j].a, cands[j].b)-target)
	})
	pick := cands[g.Int(0, min(12, len(cands))-1)]
	return pick.a, pick.b
}

// additiveDifficulty: crossing the ten is the real obstacle, not size.
func additiveDifficulty(a, b int) float64 {
	score := 0.0
	switch {
	case a == 0 || b == 0:
	case a == 1 || b == 1:
		score += 0.10
	case a == b:
		score += 0.25 // doubles are learned early
	case a+b == 10:
		score += 0.30 // complements to 10 are taught explicitly
	default:
		score += 0.50
	}
	if a+b > 10 && a < 10 && b < 10 {
		score += 0.35 // crossing the ten
	}
	score += scale(float64(max(a, b)), 0, 10) * 0.15
	return round2(math.Min(1, score))
}

// round2 rounds to 2 decimals.
func round2(f float64) float64 { return math.Round(f*100) / 100 }

// absInt is |x| for ints.
func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ───────────── 2. gen_calcul_mental_add_soustr: ARI-OPE-A03a ─────────────

// genCalculMentalAddSoustr builds a sum or difference of two naturals (personal strategies).
// Typical errors: forgotten carry, column-wise subtraction of small from big digit.
func genCalculMentalAddSoustr(g *RNG, p Params) (Problem, error) {
	cycle := p.Int("cycle", 1)
	bound := boundByCycle(cycle, p.Int("max_c1", 100), p.Int("max_c2", 1000))
	op := p.Str("operation", "")
	if op == "" {
		op = g.ChooseString([]string{"addition", "soustraction"})
	}
	obstacle, ok := p.Bool("forcer_retenue")
	if !ok {
		obstacle = g.Float() < 0.6 // most cases have an obstacle
	}

	var a, b, answer int
	var enonce string
	var ctx map[string]any
	var ds []Distractor
	var visual *VisualHint
	if op == "addition" {
		a, b = additionPair(g, bound, obstacle)
		answer = a + b
		enonce = fmt.Sprintf("%d + %d = ?", a, b)
		ctx = map[string]any{"operation": "addition", "sens": "reunion", "terme_1": a, "terme_2": b, "resultat": answer, "role_resultat": "total"}
		ds = []Distractor{
			d(carrylessAdd(a, b), "oubli_retenue", "Oublie la retenue lors de l'addition"),
			d(answer+10, "retenue_ajoutee_deux_fois", "Reporte la retenue deux fois"),
			d(absInt(a-b), "operation_inverse", "Soustrait au lieu d'additionner"),
		}
		visual = &VisualHint{"add_carry", map[string]int{"a": a, "b": b}}
	} else {
		a, b = subtractionPair(g, bound, obstacle)
		answer = a - b
		enonce = fmt.Sprintf("%d − %d = ?", a, b)
		ctx = map[string]any{"operation": "soustraction", "sens": "retrait", "depart": a, "retire": b, "resultat": answer, "role_resultat": "reste"}
		ds = []Distractor{
			d(failedBorrow(a, b), "emprunt_rate", "Soustrait le petit chiffre du grand dans chaque colonne"),
			d(a+b, "operation_inverse", "Additionne au lieu de soustraire"),
			d(answer-10, "oubli_retenue", "Erreur de dizaine lors de l'emprunt"),
		}
		visual = &VisualHint{"sub_borrow", map[string]int{"a": a, "b": b}}
	}
	shuffleDistractors(g, ds)
	choices, kept, err := AssembleChoices(g, answer, ds, 4, zeroBound())
	if err != nil {
		return Problem{}, err
	}
	diff := scale(float64(max(a, b)), 0, float64(bound)) * 0.6
	if obstacle {
		diff += 0.4
	}
	return Problem{
		CompetenceID: "ARI-OPE-A03a", Generateur: "gen_calcul_mental_add_soustr", EnonceMath: enonce, Reponse: answer,
		Distracteurs: kept, Choix: choices, ContexteNarratif: ctx, ClesReponse: []string{"resultat"}, Difficulte: diff,
		Metadonnees: map[string]any{"cycle": cycle, "avec_obstacle": obstacle},
		Shape:       fmt.Sprintf("calcul_mental:%s:%dx%d:%t", op, digits(a), digits(b), obstacle), Visual: visual,
	}, nil
}

// digits counts the decimal digits of n (0 has one digit).
func digits(n int) int {
	c := 1
	for n >= 10 {
		n /= 10
		c++
	}
	return c
}

func additionPair(g *RNG, bound int, withCarry bool) (int, int) {
	for i := 0; i < 80; i++ {
		a := g.Int(10, bound-10)
		var b int
		if bound-a > 10 {
			b = g.Int(10, bound-a)
		} else {
			b = g.Int(1, 9)
		}
		if hasCarry(a, b) == withCarry {
			return a, b
		}
	}
	return g.Int(10, bound/2), g.Int(10, bound/2)
}

func subtractionPair(g *RNG, bound int, withBorrow bool) (int, int) {
	for i := 0; i < 80; i++ {
		a := g.Int(20, bound)
		b := g.Int(1, a-1)
		if hasBorrow(a, b) == withBorrow {
			return a, b
		}
	}
	a := g.Int(20, bound)
	return a, g.Int(1, a-1)
}

func hasCarry(a, b int) bool {
	for a > 0 || b > 0 {
		if a%10+b%10 >= 10 {
			return true
		}
		a, b = a/10, b/10
	}
	return false
}

func hasBorrow(a, b int) bool {
	for b > 0 {
		if a%10 < b%10 {
			return true
		}
		a, b = a/10, b/10
	}
	return false
}

// carrylessAdd simulates the error: add each column without propagating the carry.
func carrylessAdd(a, b int) int {
	res, pos := 0, 1
	for a > 0 || b > 0 {
		res += (a%10 + b%10) % 10 * pos
		a, b, pos = a/10, b/10, pos*10
	}
	return res
}

// failedBorrow simulates the error: |digit a - digit b| column by column.
func failedBorrow(a, b int) int {
	res, pos := 0, 1
	for a > 0 || b > 0 {
		res += absInt(a%10-b%10) * pos
		a, b, pos = a/10, b/10, pos*10
	}
	return res
}

// ───────────── 3. gen_terme_manquant_add_soustr: ARI-OPE-A05 ─────────────

// additiveForms are the six equation shapes with one unknown.
var additiveForms = []string{"a+b=?", "a+?=c", "?+b=c", "a-b=?", "a-?=c", "?-b=c"}

// genTermeManquantAddSoustr builds an additive equation with a missing term.
// Typical errors: apply the visible operation instead of the inverse, ignore the unknown's position.
func genTermeManquantAddSoustr(g *RNG, p Params) (Problem, error) {
	cycle := p.Int("cycle", 1)
	bound := boundByCycle(cycle, p.Int("max_c1", 100), p.Int("max_c2", 1000))
	if m := p.Int("max", 0); m > 0 {
		bound = m // the curriculum graph gives "max" as an alias for the bound
	}
	var forms []string
	for _, f := range p.Strings("formes") {
		if contains(additiveForms, f) {
			forms = append(forms, f)
		}
	}
	if len(forms) == 0 {
		forms = additiveForms
	}
	form := p.Str("forme", "")
	if form == "" {
		form = g.ChooseString(forms)
	}
	a := g.Int(2, max(4, bound/2))
	b := g.Int(1, max(2, bound/2))
	c := a + b

	var enonce, unknown string
	var answer int
	var ds []Distractor
	switch form {
	case "a+b=?":
		enonce, answer, unknown = fmt.Sprintf("%d + %d = ?", a, b), c, "total"
		ds = []Distractor{d(absInt(a-b), "operation_directe", "Soustrait au lieu d'additionner"), d(c+1, "erreur_plus_un", "Résultat décalé de 1"), d(c-1, "erreur_plus_un", "Résultat décalé de 1")}
	case "a+?=c":
		enonce, answer, unknown = fmt.Sprintf("%d + ? = %d", a, c), b, "terme_2"
		ds = []Distractor{d(a+c, "operation_directe", "Additionne au lieu de soustraire"), d(c, "position_ignoree", "Donne le total au lieu du terme manquant"), d(a, "position_ignoree", "Recopie le terme connu")}
	case "?+b=c":
		enonce, answer, unknown = fmt.Sprintf("? + %d = %d", b, c), a, "terme_1"
		ds = []Distractor{d(b+c, "operation_directe", "Additionne au lieu de soustraire"), d(c, "position_ignoree", "Donne le total au lieu du terme manquant"), d(b, "position_ignoree", "Recopie le terme connu")}
	case "a-b=?":
		enonce, answer, unknown = fmt.Sprintf("%d − %d = ?", c, b), a, "reste"
		ds = []Distractor{d(c+b, "operation_directe", "Additionne au lieu de soustraire"), d(a+1, "erreur_plus_un", "Résultat décalé de 1"), d(b, "position_ignoree", "Donne le terme retiré")}
	case "a-?=c":
		enonce, answer, unknown = fmt.Sprintf("%d − ? = %d", c, a), b, "retire"
		ds = []Distractor{d(c+a, "operation_directe", "Additionne au lieu de soustraire"), d(a, "position_ignoree", "Recopie le résultat visible"), d(c, "position_ignoree", "Recopie le départ")}
	default: // "?-b=c"
		form = "?-b=c"
		enonce, answer, unknown = fmt.Sprintf("? − %d = %d", b, a), c, "depart"
		ds = []Distractor{d(absInt(a-b), "operation_directe", "Soustrait au lieu d'additionner"), d(a, "position_ignoree", "Recopie le résultat visible"), d(b, "position_ignoree", "Recopie le terme retiré")}
	}
	shuffleDistractors(g, ds)
	choices, kept, err := AssembleChoices(g, answer, ds, 4, zeroBound())
	if err != nil {
		return Problem{}, err
	}
	diff := 0.65
	if form == "a+b=?" || form == "a-b=?" {
		diff = 0.25
	}
	if form == "?-b=c" {
		diff = 0.85 // hardest of the set
	}
	opName := "soustraction"
	if containsRune(enonce, '+') {
		opName = "addition"
	}
	return Problem{
		CompetenceID: "ARI-OPE-A05", Generateur: "gen_terme_manquant_add_soustr", EnonceMath: enonce, Reponse: answer,
		Distracteurs: kept, Choix: choices,
		ContexteNarratif: map[string]any{
			"operation": opName, "sens": "information_manquante", "forme": form, "inconnue": unknown,
			"valeurs_connues": map[string]any{"a": a, "b": b, "total": c}, "resultat": answer, "role_resultat": unknown,
			"amorce_narrative": "une information manque dans le plan",
		},
		// valeurs_connues holds a, b and the total: one of them IS the answer, so the whole block is hidden.
		ClesReponse: []string{"resultat", "valeurs_connues", "role_resultat"},
		Difficulte:  diff, Metadonnees: map[string]any{"cycle": cycle, "forme": form}, Shape: "terme_manquant:" + form,
	}, nil
}

// containsRune reports whether s contains r.
func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
