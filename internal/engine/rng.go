// Package engine is the pure maths core: generators, grading, error taxonomy, leak check, mastery (BKT)
// and the planner. It does no I/O and imports nothing from this repository. The LLM never calculates:
// every number, answer and distractor is produced here (port of the Python engine in ../numerix-).
//
// Go for Spring people: this package is like a plain domain module with no Spring beans. Functions are
// pure; randomness comes from an explicit RNG argument so tests are reproducible (Spring: inject a Random).
package engine

import "math/rand/v2"

// RNG wraps the standard seeded generator and adds the Python-style helpers the generators use.
type RNG struct {
	r *rand.Rand // the underlying PCG generator (deterministic for a given seed)
}

// NewRNG builds a reproducible generator from a seed (Python: random.Random(seed)).
func NewRNG(seed uint64) *RNG {
	return &RNG{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

// Int returns a random integer in [lo, hi] inclusive (Python: randint). If hi <= lo it returns lo.
func (g *RNG) Int(lo, hi int) int {
	if hi <= lo {
		return lo // degenerate range: nothing to draw
	}
	return lo + g.r.IntN(hi-lo+1)
}

// Float returns a random float in [0, 1) (Python: random()).
func (g *RNG) Float() float64 { return g.r.Float64() }

// Shuffle permutes n items in place using swap (Python: shuffle).
func (g *RNG) Shuffle(n int, swap func(i, j int)) { g.r.Shuffle(n, swap) }

// ChooseString picks one element of a non-empty slice (Python: choice).
func (g *RNG) ChooseString(xs []string) string { return xs[g.r.IntN(len(xs))] }

// ChooseInt picks one element of a non-empty slice.
func (g *RNG) ChooseInt(xs []int) int { return xs[g.r.IntN(len(xs))] }
