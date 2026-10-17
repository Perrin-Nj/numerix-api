package engine

import (
	"fmt"
	"math"
	"sort"
)

// ───────────── 4. gen_faits_multiplication: ARI-OPE-A06a / A06c ─────────────

// genFaitsMultiplication builds a multiplication fact (0x0 .. 10x10) or the matching division.
// Typical errors: neighbouring table, adding the factors, 0/1 properties.
func genFaitsMultiplication(g *RNG, p Params) (Problem, error) {
	maxF := p.Int("facteurs_max", 10)
	mode := p.Str("mode", "construction")
	op := p.Str("operation", "")
	if op == "" {
		op = g.ChooseString([]string{"multiplication", "division"})
	}
	target, hasTarget := p.Float("difficulte_cible")
	a, b := drawFactors(g, maxF, target, hasTarget, p.Ints("tables_cibles"))

	var answer int
	var enonce string
	var ctx map[string]any
	var ds []Distractor
	var keys []string
	var visual *VisualHint
	if op == "multiplication" {
		answer = a * b
		enonce = fmt.Sprintf("%d × %d = ?", a, b)
		ctx = map[string]any{"operation": "multiplication", "sens": "addition_repetee", "nb_groupes": a, "par_groupe": b, "resultat": answer, "role_resultat": "total"}
		ds = []Distractor{
			d(a*(b+1), "table_voisine", fmt.Sprintf("Résultat de %d × %d au lieu de %d × %d", a, b+1, a, b)),
			d(a*(b-1), "table_voisine", fmt.Sprintf("Résultat de %d × %d au lieu de %d × %d", a, b-1, a, b)),
			d(a+b, "addition_facteurs", "Additionne les facteurs au lieu de multiplier"),
		}
		if a == 0 || a == 1 || b == 0 || b == 1 {
			keep := b // Python: a if b in (0,1) else b
			if b == 0 || b == 1 {
				keep = a
			}
			ds = append([]Distractor{d(keep, "zero_un", "Confusion sur la propriété de 0 ou de 1")}, ds...)
		}
		keys = []string{"resultat"}
		visual = &VisualHint{"equal_groups", map[string]int{"groups": b, "size": a}}
	} else {
		if b == 0 {
			b = g.Int(1, maxF)
		}
		product := a * b
		answer = a
		enonce = fmt.Sprintf("%d ÷ %d = ?", product, b)
		ctx = map[string]any{"operation": "division", "sens": "partage", "total": product, "nb_groupes": b, "par_groupe": answer, "resultat": answer, "role_resultat": "taille_du_groupe"}
		ds = []Distractor{
			d(answer+1, "table_voisine", "Résultat de la table voisine"),
			d(max(answer-1, 0), "table_voisine", "Résultat de la table voisine"),
			d(product-b, "operation_inverse", "Soustrait au lieu de diviser"),
			d(b, "division_inversee", "Inverse dividende et diviseur"),
		}
		keys = []string{"resultat", "par_groupe"}
		visual = &VisualHint{"equal_share", map[string]int{"total": product, "parts": b}}
	}
	shuffleDistractors(g, ds)
	choices, kept, err := AssembleChoices(g, answer, ds, 4, zeroBound())
	if err != nil {
		return Problem{}, err
	}
	var targetMs any
	id := "ARI-OPE-A06a"
	if mode == "maitrise" {
		id, targetMs = "ARI-OPE-A06c", 5000
	}
	return Problem{
		CompetenceID: id, Generateur: "gen_faits_multiplication", EnonceMath: enonce, Reponse: answer,
		Distracteurs: kept, Choix: choices, ContexteNarratif: ctx, ClesReponse: keys, Difficulte: multiplicativeDifficulty(a, b),
		Metadonnees: map[string]any{"mode": mode, "table": max(a, b), "est_carre": a == b, "temps_cible_ms": targetMs},
		Shape:       fmt.Sprintf("faits_mult:%s:%s", op, factClass(a, b)), Visual: visual,
	}, nil
}

// factClass groups facts by how hard they are, so the shape rotation changes the kind of fact.
func factClass(a, b int) string {
	switch dif := multiplicativeDifficulty(a, b); {
	case dif <= 0.1:
		return "trivial"
	case dif < 0.6:
		return "medium"
	}
	return "hard"
}

func drawFactors(g *RNG, maxF int, target float64, hasTarget bool, tables []int) (int, int) {
	if len(tables) > 0 {
		a, b := g.ChooseInt(tables), g.Int(0, maxF)
		if g.Float() < 0.5 {
			return a, b
		}
		return b, a
	}
	if !hasTarget {
		return g.Int(0, maxF), g.Int(0, maxF)
	}
	type pair struct{ a, b int }
	var cands []pair
	for a := 0; a <= maxF; a++ {
		for b := 0; b <= maxF; b++ {
			cands = append(cands, pair{a, b})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		return math.Abs(multiplicativeDifficulty(cands[i].a, cands[i].b)-target) < math.Abs(multiplicativeDifficulty(cands[j].a, cands[j].b)-target)
	})
	c := cands[g.Int(0, min(12, len(cands))-1)]
	return c.a, c.b
}

// multiplicativeDifficulty: x0, x1, x10 are trivial; x6, x7, x8 are the costliest to memorise.
func multiplicativeDifficulty(a, b int) float64 {
	easy := func(n int) bool { return n == 0 || n == 1 || n == 10 }
	mid := func(n int) bool { return n == 2 || n == 5 || n == 9 }
	if easy(a) || easy(b) {
		return 0.1
	}
	var score float64
	switch {
	case a == b:
		score = 0.30 // squares are anchored visually
	case mid(a) || mid(b):
		score = 0.45
	default:
		score = 0.75 // 3,4,6,7,8 among themselves
	}
	score += scale(float64(max(a, b)), 2, 10) * 0.2
	return round2(math.Min(1, score))
}

// ───────────── 5. gen_sens_multiplication_division: ARI-SOP-A03a ─────────────

// multiplicativeSenses are the four meanings the skill covers.
var multiplicativeSenses = []string{"disposition_rectangulaire", "addition_repetee", "partage", "contenance"}

// genSensMultiplicationDivision asks to recognise the structure of a situation, not to calculate.
// Typical errors: sharing versus grouping, adding instead of multiplying.
func genSensMultiplicationDivision(g *RNG, p Params) (Problem, error) {
	maxVal := p.Int("max_val", 100)
	if m := p.Int("max", 0); m > 0 {
		maxVal = m
	}
	var sens string
	options := p.Strings("sens") // a string or the list given by the curriculum graph
	var valid []string
	for _, s := range options {
		if contains(multiplicativeSenses, s) {
			valid = append(valid, s)
		}
	}
	switch {
	case len(options) == 0, len(valid) == 0:
		sens = g.ChooseString(multiplicativeSenses)
	case len(options) == 1:
		sens = valid[0]
	default:
		sens = g.ChooseString(valid)
	}
	format := p.Str("format_question", "")
	if format == "" {
		format = g.ChooseString([]string{"expression", "role"})
	}

	// nb_groupes must differ from par_groupe, otherwise "total / nb_groupes" and "total / par_groupe"
	// are the same expression and the sharing-vs-grouping distractor equals the answer.
	nb := g.Int(2, 12)
	ceiling := min(12, max(3, maxVal/nb))
	var cands []int
	for v := 2; v <= ceiling; v++ {
		if v != nb {
			cands = append(cands, v)
		}
	}
	per := nb + 1
	if len(cands) > 0 {
		per = g.ChooseInt(cands)
	}
	total := nb * per

	var enonce, answer string
	var ds []Distractor
	var ctx map[string]any
	switch sens {
	case "disposition_rectangulaire", "addition_repetee":
		ctx = map[string]any{"operation": "multiplication", "sens": sens, "nb_groupes": nb, "par_groupe": per, "total": total, "resultat": total, "role_resultat": "total", "inconnue": "total"}
		if format == "expression" {
			enonce = fmt.Sprintf("Il y a %d groupes de %d. Quelle expression donne le total ?", nb, per)
			answer = fmt.Sprintf("%d × %d", nb, per)
			ds = []Distractor{
				d(fmt.Sprintf("%d + %d", nb, per), "addition_au_lieu_mult", "Additionne les deux nombres au lieu de multiplier"),
				d(fmt.Sprintf("%d ÷ %d", total, per), "operation_inverse", "Divise alors que le total est inconnu"),
				d(fmt.Sprintf("%d − %d", nb, per), "addition_au_lieu_mult", "Soustrait les deux nombres"),
			}
		} else {
			enonce = fmt.Sprintf("On calcule %d × %d. Que représente le résultat ?", nb, per)
			answer = "le nombre total d'objets"
			ds = []Distractor{
				d("le nombre de groupes", "partage_vs_contenance", "Confond le total avec le nombre de groupes"),
				d("le nombre dans chaque groupe", "partage_vs_contenance", "Confond le total avec la taille d'un groupe"),
				d("la différence entre les deux nombres", "addition_au_lieu_mult", "Interprète l'opération comme une comparaison"),
			}
		}
	case "partage":
		ctx = map[string]any{"operation": "division", "sens": "partage", "total": total, "nb_groupes": nb, "par_groupe": per, "resultat": per, "role_resultat": "taille_du_groupe", "inconnue": "par_groupe"}
		if format == "expression" {
			enonce = fmt.Sprintf("On partage %d objets également entre %d groupes. Quelle expression donne le nombre par groupe ?", total, nb)
			answer = fmt.Sprintf("%d ÷ %d", total, nb)
			ds = []Distractor{
				d(fmt.Sprintf("%d ÷ %d", total, per), "partage_vs_contenance", "Divise par la taille du groupe au lieu du nombre de groupes"),
				d(fmt.Sprintf("%d × %d", total, nb), "operation_inverse", "Multiplie au lieu de diviser"),
				d(fmt.Sprintf("%d − %d", total, nb), "addition_au_lieu_mult", "Soustrait au lieu de diviser"),
			}
		} else {
			enonce = fmt.Sprintf("On partage %d objets entre %d groupes. On calcule %d ÷ %d. Que représente le résultat ?", total, nb, total, nb)
			answer = "le nombre d'objets dans chaque groupe"
			ds = []Distractor{
				d("le nombre de groupes", "partage_vs_contenance", "Confond taille du groupe et nombre de groupes"),
				d("le nombre total d'objets", "operation_inverse", "Confond le résultat avec le point de départ"),
				d("le nombre d'objets restants", "reste_ignore", "Interprète le quotient comme un reste"),
			}
		}
	default: // contenance
		ctx = map[string]any{"operation": "division", "sens": "contenance", "total": total, "nb_groupes": nb, "par_groupe": per, "resultat": nb, "role_resultat": "nombre_de_groupes", "inconnue": "nb_groupes"}
		if format == "expression" {
			enonce = fmt.Sprintf("On répartit %d objets par paquets de %d. Quelle expression donne le nombre de paquets ?", total, per)
			answer = fmt.Sprintf("%d ÷ %d", total, per)
			ds = []Distractor{
				d(fmt.Sprintf("%d ÷ %d", total, nb), "partage_vs_contenance", "Divise par le nombre de groupes au lieu de leur taille"),
				d(fmt.Sprintf("%d × %d", total, per), "operation_inverse", "Multiplie au lieu de diviser"),
				d(fmt.Sprintf("%d ÷ %d", per, total), "division_inversee", "Inverse dividende et diviseur"),
			}
		} else {
			enonce = fmt.Sprintf("On répartit %d objets par paquets de %d. On calcule %d ÷ %d. Que représente le résultat ?", total, per, total, per)
			answer = "le nombre de paquets"
			ds = []Distractor{
				d("le nombre d'objets par paquet", "partage_vs_contenance", "Confond nombre de groupes et taille du groupe"),
				d("le nombre total d'objets", "operation_inverse", "Confond le résultat avec le point de départ"),
				d("le nombre d'objets restants", "reste_ignore", "Interprète le quotient comme un reste"),
			}
		}
	}
	shuffleDistractors(g, ds)
	choices, kept, err := AssembleChoices(g, answer, ds, 4, nil)
	if err != nil {
		return Problem{}, err
	}
	diff := map[string]float64{"addition_repetee": 0.30, "disposition_rectangulaire": 0.40, "partage": 0.60, "contenance": 0.80}[sens]
	var visual *VisualHint
	if sens == "partage" && format == "expression" {
		visual = &VisualHint{"equal_share", map[string]int{"total": total, "parts": nb}}
	}
	return Problem{
		CompetenceID: "ARI-SOP-A03a", Generateur: "gen_sens_multiplication_division", EnonceMath: enonce, Reponse: answer,
		Distracteurs: kept, Choix: choices, ContexteNarratif: ctx,
		// In the "role" format, role_resultat IS the answer worded differently.
		ClesReponse: []string{"resultat", "role_resultat", "inconnue"}, Difficulte: diff,
		Metadonnees: map[string]any{"sens": sens, "format": format, "sans_calcul": true},
		Shape:       "sens:" + sens + ":" + format, Visual: visual,
	}, nil
}

// ───────────── 6. gen_mult_div_personnelle: ARI-OPE-A07a ─────────────

// genMultDivPersonnelle builds a product or quotient of a 3-digit number by a 1-digit number.
// The remainder is the pedagogical heart. Typical errors: ignored remainder, remainder larger than divisor.
func genMultDivPersonnelle(g *RNG, p Params) (Problem, error) {
	dividendMax := p.Int("dividende_max", 999)
	divisorMax := p.Int("diviseur_max", 9)
	op := p.Str("operation", "")
	if op == "" {
		op = g.ChooseString([]string{"multiplication", "division"})
	}
	var enonce string
	var answer Value
	var ctx map[string]any
	var ds []Distractor
	var meta map[string]any
	var keys []string
	var diff float64
	shape := ""
	if op == "multiplication" {
		a := g.Int(100, min(999, dividendMax))
		b := g.Int(2, divisorMax)
		prod := a * b
		answer = prod
		enonce = fmt.Sprintf("%d × %d = ?", a, b)
		ctx = map[string]any{"operation": "multiplication", "sens": "addition_repetee", "nb_groupes": b, "par_groupe": a, "resultat": prod, "role_resultat": "total", "a_un_reste": false}
		ds = []Distractor{
			d(multiplyNoCarry(a, b), "oubli_retenue", "Oublie la retenue dans la multiplication"),
			d(a*(b-1), "table_voisine", "Utilise la table voisine"),
			d(a+b, "addition_facteurs", "Additionne au lieu de multiplier"),
		}
		meta = map[string]any{"reste": 0}
		keys, diff, shape = []string{"resultat"}, 0.55, "mult_div_perso:multiplication"
	} else {
		b := g.Int(2, divisorMax)
		withRem := g.Float() < 0.65 // most with a remainder: that is the goal
		if v, ok := p.Bool("avec_reste"); ok {
			withRem = v
		}
		q := g.Int(10, max(11, dividendMax/b))
		rem := 0
		if withRem {
			rem = g.Int(1, b-1)
		}
		a := q*b + rem
		answer = []int{q, rem}
		enonce = fmt.Sprintf("%d ÷ %d = ? (avec reste)", a, b)
		hook := "tout se répartit exactement"
		if rem > 0 {
			hook = fmt.Sprintf("il reste %d — que fait-on de ce qui ne rentre pas ?", rem)
		}
		ctx = map[string]any{"operation": "division", "sens": "partage", "total": a, "nb_groupes": b, "par_groupe": q, "reste": rem, "resultat": q, "role_resultat": "taille_du_groupe", "a_un_reste": rem > 0, "amorce_narrative": hook}
		ds = []Distractor{
			d([]int{q + 1, rem}, "arrondi_quotient", "Arrondit le quotient vers le haut sans traiter le reste"),
			d([]int{q - 1, rem + b}, "reste_superieur_diviseur", "Laisse un reste plus grand que le diviseur"),
		}
		if rem > 0 {
			// the central trap: ignoring a remainder that exists
			ds = append([]Distractor{d([]int{q, 0}, "reste_ignore", "Ignore le reste alors que le contexte l'exige")}, ds...)
			diff, shape = 0.80, "mult_div_perso:division:reste"
		} else {
			// exact division: the trap is inventing a remainder
			ds = append([]Distractor{d([]int{q, 1}, "reste_invente", "Invente un reste alors que la division est exacte")}, ds...)
			ds = append(ds, d([]int{q - 1, b}, "reste_superieur_diviseur", "Laisse un reste égal au diviseur au lieu de compléter le quotient"))
			diff, shape = 0.60, "mult_div_perso:division:exacte"
		}
		meta = map[string]any{"reste": rem, "quotient": q, "diviseur": b}
		keys = []string{"resultat", "par_groupe", "reste", "a_un_reste", "amorce_narrative"}
	}
	shuffleDistractors(g, ds)
	choices, kept, err := AssembleChoices(g, answer, ds, 4, nil)
	if err != nil {
		return Problem{}, err
	}
	return Problem{
		CompetenceID: "ARI-OPE-A07a", Generateur: "gen_mult_div_personnelle", EnonceMath: enonce, Reponse: answer,
		Distracteurs: kept, Choix: choices, ContexteNarratif: ctx, ClesReponse: keys, Difficulte: diff, Metadonnees: meta, Shape: shape,
	}, nil
}

// multiplyNoCarry simulates the error: multiply each digit without carrying.
func multiplyNoCarry(a, b int) int {
	res, pos := 0, 1
	for a > 0 {
		res += (a % 10 * b) % 10 * pos
		a, pos = a/10, pos*10
	}
	return res
}
