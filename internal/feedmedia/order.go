package feedmedia

import (
	"hash/fnv"
	"strconv"
	"math/rand/v2"
)

// MaxVideoRun is how many videos may follow each other.
const MaxVideoRun = 2

// SeedOf turns a child id into the number that drives that child's private random order.
func SeedOf(childID string) uint64 {
	h := fnv.New64a() // a small, fast, non-cryptographic hash (the order is not a secret)
	_, _ = h.Write([]byte("discover:" + childID))
	return h.Sum64()
}

// SeedWith is SeedOf for one visit: the same child with a different visit number (the client picks a fresh random one
// each time the Discover tab opens) gets a different order. visit < 0 means "no visit number": the child's fixed order.
func SeedWith(childID string, visit int) uint64 {
	if visit < 0 {
		return SeedOf(childID)
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte("discover:" + childID + ":visit:" + strconv.Itoa(visit)))
	return h.Sum64()
}

// rawOrder is one pass over n items in random order for (seed, epoch): the non-video items are shuffled, then the
// videos are dropped into the gaps between them, at most MaxVideoRun per gap, never at the first or last place.
// kinds[i] is the kind of item i. Deterministic: same input, same output.
func rawOrder(seed uint64, epoch int, kinds []string) []int {
	n := len(kinds)
	r := rand.New(rand.NewPCG(seed, uint64(epoch)+1)) // Spring: a seeded java.util.Random
	var others, videos []int
	for i, k := range kinds {
		if k == KindVideo {
			videos = append(videos, i)
		} else {
			others = append(others, i)
		}
	}
	r.Shuffle(len(others), func(i, j int) { others[i], others[j] = others[j], others[i] })
	r.Shuffle(len(videos), func(i, j int) { videos[i], videos[j] = videos[j], videos[i] })
	gaps := len(others) - 1 // interior gaps between two non-video items
	if gaps < 1 {
		return append(others, videos...) // degenerate catalogue (tests): nothing to interleave
	}
	// Each interior gap can hold MaxVideoRun videos. Shuffle those tokens and give the first len(videos) of them out.
	tokens := make([]int, 0, gaps*MaxVideoRun)
	for g := 1; g <= gaps; g++ {
		for k := 0; k < MaxVideoRun; k++ {
			tokens = append(tokens, g)
		}
	}
	r.Shuffle(len(tokens), func(i, j int) { tokens[i], tokens[j] = tokens[j], tokens[i] })
	perGap := make([]int, len(others)+1) // videos to place before others[g]
	placed := 0
	for _, g := range tokens {
		if placed == len(videos) {
			break
		}
		perGap[g]++
		placed++
	}
	out := make([]int, 0, n)
	vi := 0
	for g, o := range others {
		for k := 0; k < perGap[g]; k++ {
			out = append(out, videos[vi])
			vi++
		}
		out = append(out, o)
	}
	return append(out, videos[vi:]...) // only reached when there are more videos than room (tiny catalogue)
}

// Order returns the pass number epoch as a permutation of 0..len(kinds)-1. Every index appears exactly once, so
// no item repeats inside a pass; the next pass is a fresh shuffle. The first item of a pass differs from the last
// item of the previous pass.
func Order(seed uint64, epoch int, kinds []string) []int {
	out := rawOrder(seed, epoch, kinds)
	if epoch > 0 && len(out) > 2 {
		prev := rawOrder(seed, epoch-1, kinds)
		if out[0] == prev[len(prev)-1] { // would repeat the item seen just before: swap with the second (any run of videos stays within the limit)
			out[0], out[1] = out[1], out[0]
		}
	}
	return out
}

// At returns the item index shown at media counter c (0 for the first media item), and the pass it belongs to.
func At(seed uint64, c int, kinds []string) (idx, epoch int) {
	n := len(kinds)
	epoch = c / n
	return Order(seed, epoch, kinds)[c%n], epoch
}
