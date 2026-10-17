package engine

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"
)

// Knowledge tracing (BKT) with forgetting. Two deliberate differences from classic BKT:
//   - three outcomes: a typical error weighs more than a random one;
//   - forgetting: mastery decays with a half-life that grows with every SPACED review.
const (
	MasteryThreshold  = 0.9 // a skill counts as mastered at this probability
	halfLifeFactor    = 2.3 // 1 d -> 2.3 -> 5.3 -> 12.2 -> 28
	spacingThreshold  = 0.5 // a success counts as a review if elapsed >= 0.5 x half-life
	slowFactor        = 2.0 // answer slower than 2 x target time: less convincing success
	typicalErrWeight  = 3.0 // divides p(slip): the wrong answer was targeted
	hoursPerSecondInv = 3600.0
)

// bktParams are the four BKT probabilities plus the starting half-life in hours.
type bktParams struct{ pInit, pLearn, pSlip, pGuess, halfLifeH float64 }

// bktProfiles route a skill to a profile by its generator name.
var bktProfiles = map[string]bktParams{
	"faits_numeriques": {0.10, 0.25, 0.10, 0.20, 18},
	"conceptuel":       {0.10, 0.12, 0.10, 0.20, 72},
	"procedural":       {0.10, 0.20, 0.10, 0.20, 36},
}

// profileFor picks the BKT profile of a skill (port of profil_pour).
func profileFor(gr *Graph, skillID string) bktParams {
	gen := ""
	if s, err := gr.Get(skillID); err == nil {
		gen = s.Generator
	}
	switch {
	case strings.Contains(gen, "faits"):
		return bktProfiles["faits_numeriques"]
	case strings.Contains(gen, "sens"):
		return bktProfiles["conceptuel"]
	}
	return bktProfiles["procedural"]
}

// SkillState is the mastery state of one skill for one child (persisted as JSON).
type SkillState struct {
	P        float64 `json:"p"`          // mastery at time Last
	HalfLife float64 `json:"demi_vie_h"` // hours
	Last     string  `json:"dernier"`    // RFC 3339 UTC
	Reviews  int     `json:"nb_revues"`
	Obs      int     `json:"nb_obs"`
	Peak     float64 `json:"pic"` // highest mastery reached
}

// Profile is one child's learner profile (Python: ProfilApprenant). It is what we persist.
type Profile struct {
	ChildID string                 `json:"enfant_id"`
	States  map[string]*SkillState `json:"etats"`
	Miscs   map[string][]string    `json:"misconceptions"`         // skill -> active error codes
	Descs   map[string]string      `json:"descriptions,omitempty"` // error code -> description
	graph   *Graph
}

// NewProfile creates an empty profile.
func NewProfile(childID string, gr *Graph) *Profile {
	return &Profile{ChildID: childID, States: map[string]*SkillState{}, Miscs: map[string][]string{}, Descs: map[string]string{}, graph: gr}
}

// ParseProfile reads the stored JSON (empty or "{}" gives a fresh profile).
func ParseProfile(childID, raw string, gr *Graph) (*Profile, error) {
	p := NewProfile(childID, gr)
	if strings.TrimSpace(raw) == "" || raw == "{}" {
		return p, nil
	}
	if err := json.Unmarshal([]byte(raw), p); err != nil {
		return nil, err
	}
	p.graph, p.ChildID = gr, childID
	if p.States == nil {
		p.States = map[string]*SkillState{}
	}
	if p.Miscs == nil {
		p.Miscs = map[string][]string{}
	}
	if p.Descs == nil {
		p.Descs = map[string]string{}
	}
	return p, nil
}

// JSON serialises the profile for storage.
func (p *Profile) JSON() (string, error) {
	b, err := json.Marshal(p)
	return string(b), err
}

// hoursBetween returns elapsed hours from an RFC 3339 time to t, never negative.
func hoursBetween(from string, t time.Time) float64 {
	f, err := time.Parse(time.RFC3339Nano, from)
	if err != nil {
		return 0
	}
	return math.Max(0, t.Sub(f).Seconds()/hoursPerSecondInv)
}

// masteryAt applies the forgetting curve: p * 0.5^(hours / half-life).
func masteryAt(e *SkillState, t time.Time) float64 {
	return e.P * math.Pow(0.5, hoursBetween(e.Last, t)/e.HalfLife)
}

// Observe records an answer and returns the new mastery. responseMs and targetMs may be 0 (unknown).
func (p *Profile) Observe(skillID string, res Result, responseMs, targetMs float64, now time.Time) float64 {
	prm := profileFor(p.graph, skillID)
	e, seen := p.States[skillID]
	elapsed := 0.0
	if !seen {
		e = &SkillState{P: prm.pInit, HalfLife: prm.halfLifeH, Last: now.UTC().Format(time.RFC3339Nano)}
	} else {
		elapsed = hoursBetween(e.Last, now)
	}
	cur := masteryAt(e, now)
	slow := responseMs > 0 && targetMs > 0 && responseMs > slowFactor*targetMs

	var post float64
	if res.Type == Correct {
		guess := prm.pGuess
		if slow {
			guess = math.Min(0.5, prm.pGuess*2)
		}
		post = cur * (1 - prm.pSlip) / (cur*(1-prm.pSlip) + (1-cur)*guess)
		// spaced review: only a real delay lengthens the half-life
		if e.Obs > 0 && elapsed >= spacingThreshold*e.HalfLife {
			e.HalfLife *= halfLifeFactor
			e.Reviews++
		}
		delete(p.Miscs, skillID) // a success clears the active misconceptions of this skill
	} else {
		slip := prm.pSlip
		if res.Type == ErreurTypique {
			slip /= typicalErrWeight
			if res.CodeErreur != "" {
				if !slices.Contains(p.Miscs[skillID], res.CodeErreur) {
					p.Miscs[skillID] = append(p.Miscs[skillID], res.CodeErreur)
				}
				p.Descs[res.CodeErreur] = res.DescriptionErreur
			}
		}
		post = cur * slip / (cur*slip + (1-cur)*(1-prm.pGuess))
	}
	e.P = post + (1-post)*prm.pLearn
	e.Last = now.UTC().Format(time.RFC3339Nano)
	e.Obs++
	e.Peak = math.Max(e.Peak, e.P)
	p.States[skillID] = e
	return e.P
}

// Mastery returns the current mastery of a skill (0 when never practised).
func (p *Profile) Mastery(skillID string, now time.Time) float64 {
	if e, ok := p.States[skillID]; ok {
		return masteryAt(e, now)
	}
	return 0
}

// Mastered returns the set of skills at or above the threshold now.
func (p *Profile) Mastered(now time.Time) map[string]bool {
	out := map[string]bool{}
	for id, e := range p.States {
		if masteryAt(e, now) >= MasteryThreshold {
			out[id] = true
		}
	}
	return out
}

// Due is a skill that was mastered once and has fallen back below the threshold.
type Due struct {
	SkillID string
	Mastery float64
}

// ToReview lists due skills, the most urgent (lowest mastery) first.
func (p *Profile) ToReview(now time.Time) []Due {
	var out []Due
	for id, e := range p.States {
		if m := masteryAt(e, now); e.Peak >= MasteryThreshold && m < MasteryThreshold {
			out = append(out, Due{id, m})
		}
	}
	slices.SortFunc(out, func(a, b Due) int {
		switch {
		case a.Mastery < b.Mastery:
			return -1
		case a.Mastery > b.Mastery:
			return 1
		}
		return strings.Compare(a.SkillID, b.SkillID)
	})
	return out
}

// NextReview returns when a mastered skill drops under the threshold (ok false if it never was mastered).
func (p *Profile) NextReview(skillID string, now time.Time) (time.Time, bool) {
	e, ok := p.States[skillID]
	if !ok || e.Peak < MasteryThreshold {
		return time.Time{}, false
	}
	m := masteryAt(e, now)
	hours := 0.0
	if m > MasteryThreshold {
		hours = e.HalfLife * math.Log2(m/MasteryThreshold)
	}
	return now.Add(time.Duration(hours * float64(time.Hour))), true
}

// ActiveMisconceptions returns code -> sorted skills, plus the stored description of each code.
func (p *Profile) ActiveMisconceptions() map[string][]string {
	out := map[string][]string{}
	for skill, codes := range p.Miscs {
		for _, c := range codes {
			out[c] = append(out[c], skill)
		}
	}
	for c := range out {
		slices.Sort(out[c])
	}
	return out
}
