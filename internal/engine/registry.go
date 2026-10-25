package engine

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
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

// MinBankQuestions is how many published bank questions a quest needs before its skill counts as playable.
const MinBankQuestions = 8

// BankEntry says how many published curated questions a quest (one skill in one world) has.
type BankEntry struct {
	World, Skill string
	Published    int         // all published questions of the quest
	ByGrade      map[int]int // published questions per question grade (a child sees grades up to their own)
}

// Graph is the skill graph (Python: GraphePDA). The curriculum part never changes; the bank overlay (which
// skills have curated questions in which world) is refreshed while the server runs, so it sits behind a lock.
//
// Go for Spring people: sync.RWMutex is a read-write lock; many readers can hold RLock at the same time, a
// writer waits for them. Public methods lock once; the lower-case helpers assume the lock is already held.
type Graph struct {
	skills map[string]*Skill

	mu          sync.RWMutex
	bank        map[string]int         // "world|skill" -> published bank questions
	bankGrade   map[string]map[int]int // "world|skill" -> published questions per question grade
	questWorlds map[string][]string    // skill -> worlds that have a quest for it (any number of questions)
	bankOnly    map[string]bool        // worlds that play only their written questions: no generated chapter, no school-year or prerequisite lock
}

// SetBank replaces the bank overlay (called when the question bank changed).
func (gr *Graph) SetBank(entries []BankEntry) {
	bank := map[string]int{}
	byGrade := map[string]map[int]int{}
	qw := map[string][]string{}
	for _, e := range entries {
		bank[e.World+"|"+e.Skill] = e.Published
		byGrade[e.World+"|"+e.Skill] = e.ByGrade
		if !slices.Contains(qw[e.Skill], e.World) {
			qw[e.Skill] = append(qw[e.Skill], e.World)
		}
	}
	gr.mu.Lock()
	defer gr.mu.Unlock()
	gr.bank, gr.bankGrade, gr.questWorlds = bank, byGrade, qw
}

// implemented reports whether a skill can be played: a generator exists, or some world has enough bank questions.
func (gr *Graph) implemented(id string) bool {
	s, ok := gr.skills[id]
	if !ok {
		return false
	}
	if s.Implementation {
		return true
	}
	for _, w := range gr.questWorlds[id] {
		if gr.bank[w+"|"+id] >= MinBankQuestions {
			return true
		}
	}
	return false
}

// IsImplemented is the public form of implemented.
func (gr *Graph) IsImplemented(id string) bool {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	return gr.implemented(id)
}

// SetBankOnly names the worlds that play only their written bank questions (Spring: a setting read at start-up).
func (gr *Graph) SetBankOnly(worlds ...string) {
	gr.mu.Lock()
	defer gr.mu.Unlock()
	gr.bankOnly = map[string]bool{}
	for _, w := range worlds {
		gr.bankOnly[w] = true
	}
}

// IsBankOnly reports whether a world plays only its written questions.
func (gr *Graph) IsBankOnly(world string) bool {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	return gr.bankOnlyNow(world)
}

// bankOnlyNow is IsBankOnly with the lock held. A world counts as written-questions only once its bank is loaded
// (an empty database must still be playable with generated chapters).
func (gr *Graph) bankOnlyNow(world string) bool {
	if !gr.bankOnly[world] {
		return false
	}
	for key, perGrade := range gr.bankGrade {
		if strings.HasPrefix(key, world+"|") {
			n := 0
			for _, c := range perGrade {
				n += c
			}
			if n >= MinBankQuestions {
				return true
			}
		}
	}
	return false
}

// bankAny counts the published bank questions of a quest for any school year.
func (gr *Graph) bankAny(world, id string) int {
	n := 0
	for _, c := range gr.bankGrade[world+"|"+id] {
		n += c
	}
	return n
}

// PlayableIn reports whether a chapter of this skill can be served in this world: a generator exists (it works in
// any world) or the world has enough published bank questions for it.
func (gr *Graph) PlayableIn(world, id string) bool {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	s, ok := gr.skills[id]
	if gr.bankOnlyNow(world) { // written questions only, whatever the school year
		return ok && gr.bankAny(world, id) >= MinBankQuestions
	}
	return ok && (s.Implementation || gr.bank[world+"|"+id] >= MinBankQuestions)
}

// BankFor counts the published bank questions of a quest that a child of this school year can be served
// (question grade up to the child's grade, the same rule as the question picker).
func (gr *Graph) BankFor(world, id string, grade int) int {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	return gr.bankFor(world, id, grade)
}

// bankFor is BankFor with the lock already held.
func (gr *Graph) bankFor(world, id string, grade int) int {
	n := 0
	for g, c := range gr.bankGrade[world+"|"+id] {
		if g <= grade {
			n += c
		}
	}
	return n
}

// PlayableFor is PlayableIn for one school year: a generator exists, or the quest has at least MinBankQuestions
// published questions this grade can be served. Below that the chapter is locked, never offered.
func (gr *Graph) PlayableFor(world, id string, grade int) bool {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	s, ok := gr.skills[id]
	if gr.bankOnlyNow(world) {
		return ok && gr.bankAny(world, id) >= MinBankQuestions
	}
	return ok && (s.Implementation || gr.bankFor(world, id, grade) >= MinBankQuestions)
}

// HasGenerator reports whether a generator exists for the skill.
func (gr *Graph) HasGenerator(id string) bool {
	s, ok := gr.skills[id]
	return ok && s.Implementation
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

// Implemented returns the playable skill ids (generator or enough bank questions), sorted.
func (gr *Graph) Implemented() []string {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	return gr.implementedIDs()
}

// implementedIDs is Implemented with the lock already held.
func (gr *Graph) implementedIDs() []string {
	var out []string
	for _, id := range gr.All() {
		if gr.implemented(id) {
			out = append(out, id)
		}
	}
	return out
}

// ByWorld returns the playable skills of a world, sorted: skills with a generator that the curriculum or a quest
// places in the world, and skills whose quest in the world has enough published bank questions.
func (gr *Graph) ByWorld(world string) []string {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	var out []string
	for _, id := range gr.All() {
		s := gr.skills[id]
		switch {
		case s.Implementation && (slices.Contains(s.Worlds, world) || slices.Contains(gr.questWorlds[id], world)):
			out = append(out, id)
		case gr.bank[world+"|"+id] >= MinBankQuestions:
			out = append(out, id)
		}
	}
	return out
}

// WorldSkillCount counts the curriculum skills (implemented or not) tagged with a world, plus the skills of the
// world quests that the tag does not already cover.
func (gr *Graph) WorldSkillCount(world string) int {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	n := 0
	for id, s := range gr.skills {
		if slices.Contains(s.Worlds, world) || slices.Contains(gr.questWorlds[id], world) {
			n++
		}
	}
	return n
}

// PrereqsMet reports whether the prerequisites are mastered. With ignoreUnimplemented true, a prerequisite
// without a generator cannot be mastered by the child, so it is ignored (otherwise it would block everything).
func (gr *Graph) PrereqsMet(id string, mastered map[string]bool, ignoreUnimplemented bool) bool {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	return gr.prereqsMet(id, mastered, ignoreUnimplemented)
}

// prereqsMet is PrereqsMet with the lock already held.
func (gr *Graph) prereqsMet(id string, mastered map[string]bool, ignoreUnimplemented bool) bool {
	s, err := gr.Get(id)
	if err != nil {
		return false
	}
	for _, p := range s.Prerequisites {
		if mastered[p] {
			continue
		}
		if ignoreUnimplemented {
			if !gr.implemented(p) {
				continue
			}
		}
		return false
	}
	return true
}

// Available returns the unlocked skills: prerequisites met, not mastered yet.
func (gr *Graph) Available(mastered map[string]bool, ignoreUnimplemented bool) []string {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	var out []string
	for _, id := range gr.implementedIDs() {
		if !mastered[id] && gr.prereqsMet(id, mastered, ignoreUnimplemented) {
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

// WorldsOf lists the worlds of a skill: the curriculum tags plus the worlds whose quests use the skill.
func (gr *Graph) WorldsOf(id string) []string {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	s, ok := gr.skills[id]
	if !ok {
		return nil
	}
	out := slices.Clone(s.Worlds)
	for _, w := range gr.questWorlds[id] {
		if !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}
