package engine

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Selection is the planner's choice: which skill and why.
type Selection struct {
	SkillID string
	Reason  string // revision | consolidation | nouvelle | entretien
}

// ErrNoSkill is returned when no implemented skill matches the world and grade.
var ErrNoSkill = errors.New("engine: no implemented skill for this world and grade")

// Choose picks the next skill. Priority: revision (mastery fell) > consolidation (in progress) >
// new (prerequisites met) > upkeep (everything mastered). Deterministic for a given rng. No LLM.
func Choose(gr *Graph, prof *Profile, world string, grade int, g *RNG, now time.Time) (Selection, error) {
	impl := map[string]bool{}
	for _, id := range gr.Implemented() {
		impl[id] = true
	}
	var inWorld []string
	for _, id := range gr.ByWorld(world) {
		if s, _ := gr.Get(id); (s.YearIntro <= grade || gr.IsBankOnly(world)) && gr.PlayableFor(world, id, grade) { // only chapters this grade can really be served
			inWorld = append(inWorld, id)
		}
	}

	// revision ignores the world: same skill, new story
	for _, due := range prof.ToReview(now) {
		if impl[due.SkillID] && gr.PlayableFor(world, due.SkillID, grade) {
			return Selection{due.SkillID, "revision"}, nil
		}
	}

	mastered := prof.Mastered(now)
	var inProgress []string
	for _, id := range inWorld {
		if _, seen := prof.States[id]; seen && !mastered[id] {
			inProgress = append(inProgress, id)
		}
	}
	if len(inProgress) > 0 { // weakest first
		best := inProgress[0]
		for _, id := range inProgress[1:] {
			if prof.Mastery(id, now) < prof.Mastery(best, now) {
				best = id
			}
		}
		return Selection{best, "consolidation"}, nil
	}

	var fresh []string
	for _, id := range inWorld {
		if _, seen := prof.States[id]; !seen && gr.PrereqsMet(id, mastered, true) {
			fresh = append(fresh, id)
		}
	}
	if len(fresh) > 0 { // earliest school year first, random among ties
		first := 99
		for _, id := range fresh {
			s, _ := gr.Get(id)
			first = min(first, s.YearIntro)
		}
		var ties []string
		for _, id := range fresh {
			if s, _ := gr.Get(id); s.YearIntro == first {
				ties = append(ties, id)
			}
		}
		return Selection{g.ChooseString(ties), "nouvelle"}, nil
	}

	var upkeep []string
	for id := range mastered {
		if impl[id] {
			upkeep = append(upkeep, id)
		}
	}
	slices.Sort(upkeep)
	if len(upkeep) == 0 {
		upkeep = inWorld
	}
	if len(upkeep) == 0 {
		return Selection{}, fmt.Errorf("%w: world=%s grade=%d", ErrNoSkill, world, grade)
	}
	return Selection{g.ChooseString(upkeep), "entretien"}, nil
}
