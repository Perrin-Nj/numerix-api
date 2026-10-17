package engine

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// The curriculum JSON files are compiled into the binary (Spring: files under src/main/resources).
//
//go:embed data/pda-arithmetique.json data/pda-autres-champs.json
var curriculumFS embed.FS

// generator is the shape every problem generator has.
type generator func(g *RNG, p Params) (Problem, error)

// registry maps the generator name used in the curriculum to its Go function.
// Only 6 of the curriculum's generators exist today; the others are "not implemented" (same as Python).
var registry = map[string]generator{
	"gen_faits_addition":               genFaitsAddition,
	"gen_calcul_mental_add_soustr":     genCalculMentalAddSoustr,
	"gen_terme_manquant_add_soustr":    genTermeManquantAddSoustr,
	"gen_faits_multiplication":         genFaitsMultiplication,
	"gen_sens_multiplication_division": genSensMultiplicationDivision,
	"gen_mult_div_personnelle":         genMultDivPersonnelle,
}

// ErrUnknownSkill is returned for an id that is not in the curriculum.
var ErrUnknownSkill = errors.New("engine: unknown skill")

// ErrNotImplemented is returned when the skill's generator does not exist yet.
var ErrNotImplemented = errors.New("engine: generator not implemented")

// ErrNoFreshProblem is returned when no acceptable problem could be drawn.
var ErrNoFreshProblem = errors.New("engine: no fresh problem found")

// TypicalError is a known error of a skill (code and description).
type TypicalError struct {
	Code string `json:"code"`
	Desc string `json:"desc"`
}

// Skill is one curriculum entry ("competence" in the JSON).
type Skill struct {
	ID             string         `json:"id"`
	RefPDA         string         `json:"ref_pda"`
	Section        string         `json:"section"`
	Label          string         `json:"libelle"`
	YearIntro      int            `json:"annee_intro"`
	YearMastery    int            `json:"annee_maitrise"`
	Cycle          int            `json:"cycle"`
	Prerequisites  []string       `json:"prerequis"`
	Generator      string         `json:"generateur"`
	Params         Params         `json:"parametres"`
	TypicalErrors  []TypicalError `json:"erreurs_typiques"`
	Worlds         []string       `json:"univers"`
	Implementation bool           `json:"-"` // true when the generator exists
}

// Graph is the skill graph (Python: GraphePDA).
type Graph struct {
	skills map[string]*Skill
}

// LoadGraph reads both embedded JSON files.
func LoadGraph() (*Graph, error) {
	gr := &Graph{skills: map[string]*Skill{}}
	for _, name := range []string{"data/pda-arithmetique.json", "data/pda-autres-champs.json"} {
		raw, err := curriculumFS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		var doc struct {
			Competences []Skill `json:"competences"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		for i := range doc.Competences {
			s := doc.Competences[i]
			if s.YearIntro == 0 {
				s.YearIntro = 99
			}
			_, s.Implementation = registry[s.Generator]
			gr.skills[s.ID] = &s
		}
	}
	return gr, nil
}

// Get returns a skill or ErrUnknownSkill.
func (gr *Graph) Get(id string) (*Skill, error) {
	s, ok := gr.skills[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSkill, id)
	}
	return s, nil
}

// All returns every skill id, sorted.
func (gr *Graph) All() []string {
	ids := make([]string, 0, len(gr.skills))
	for id := range gr.skills {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Implemented returns the ids whose generator exists, sorted.
func (gr *Graph) Implemented() []string {
	var out []string
	for _, id := range gr.All() {
		if gr.skills[id].Implementation {
			out = append(out, id)
		}
	}
	return out
}

// ByWorld returns the implemented skills available in a world, sorted.
func (gr *Graph) ByWorld(world string) []string {
	var out []string
	for _, id := range gr.Implemented() {
		if slices.Contains(gr.skills[id].Worlds, world) {
			out = append(out, id)
		}
	}
	return out
}

// WorldSkillCount counts all curriculum skills (implemented or not) tagged with a world.
func (gr *Graph) WorldSkillCount(world string) int {
	n := 0
	for _, s := range gr.skills {
		if slices.Contains(s.Worlds, world) {
			n++
		}
	}
	return n
}

// PrereqsMet reports whether the prerequisites are mastered. With ignoreUnimplemented true, a prerequisite
// without a generator cannot be mastered by the child, so it is ignored (otherwise it would block everything).
func (gr *Graph) PrereqsMet(id string, mastered map[string]bool, ignoreUnimplemented bool) bool {
	s, err := gr.Get(id)
	if err != nil {
		return false
	}
	for _, p := range s.Prerequisites {
		if mastered[p] {
			continue
		}
		if ignoreUnimplemented {
			if ps, ok := gr.skills[p]; !ok || !ps.Implementation {
				continue
			}
		}
		return false
	}
	return true
}

// Available returns the unlocked skills: prerequisites met, not mastered yet.
func (gr *Graph) Available(mastered map[string]bool, ignoreUnimplemented bool) []string {
	var out []string
	for _, id := range gr.Implemented() {
		if !mastered[id] && gr.PrereqsMet(id, mastered, ignoreUnimplemented) {
			out = append(out, id)
		}
	}
	return out
}

// Generate produces one problem for a skill. Graph parameters apply first, then the overrides.
func (gr *Graph) Generate(id string, g *RNG, overrides Params) (Problem, error) {
	s, err := gr.Get(id)
	if err != nil {
		return Problem{}, err
	}
	gen, ok := registry[s.Generator]
	if !ok {
		return Problem{}, fmt.Errorf("%w: %s (skill %s)", ErrNotImplemented, s.Generator, id)
	}
	params := Params{}
	for k, v := range s.Params {
		params[k] = v
	}
	params["cycle"] = s.Cycle
	for k, v := range overrides {
		params[k] = v
	}
	p, err := gen(g, params)
	if err != nil {
		return Problem{}, err
	}
	// the graph is the source of truth for identity and worlds
	p.CompetenceID = id
	p.Metadonnees["univers_compatibles"] = s.Worlds
	p.Metadonnees["libelle"] = s.Label
	p.Metadonnees["ref_pda"] = s.RefPDA
	if _, has := p.ContexteNarratif["univers_compatibles"]; !has {
		p.ContexteNarratif["univers_compatibles"] = s.Worlds
	}
	return p, nil
}

// maxTries bounds how often GenerateFresh redraws.
const maxTries = 60

// GenerateFresh draws a problem of the skill whose Shape differs from avoidShape and which accept() allows
// (for example "not served to this child yet"). A wrong answer therefore never replays the same kind of
// exercise (port of the shape rotation in the mock session). If no different shape can be found
// after maxTries it relaxes the shape rule and keeps only accept().
func (gr *Graph) GenerateFresh(id string, g *RNG, avoidShape string, accept func(*Problem) bool) (Problem, error) {
	var lastErr error
	for pass := 0; pass < 2; pass++ {
		for i := 0; i < maxTries; i++ {
			p, err := gr.Generate(id, g, nil)
			if err != nil {
				lastErr = err
				if errors.Is(err, ErrNotImplemented) || errors.Is(err, ErrUnknownSkill) {
					return Problem{}, err
				}
				continue
			}
			if pass == 0 && avoidShape != "" && p.Shape == avoidShape {
				continue
			}
			if accept != nil && !accept(&p) {
				continue
			}
			return p, nil
		}
	}
	if lastErr != nil {
		return Problem{}, fmt.Errorf("%w: %v", ErrNoFreshProblem, lastErr)
	}
	return Problem{}, ErrNoFreshProblem
}
