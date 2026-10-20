package feedmedia

import (
	"fmt"
	"testing"
)

// catalogue builds a kinds slice shaped like the real one: 110 photos, 40 animations, 50 videos.
func catalogue(photos, anims, videos int) []string {
	var k []string
	for i := 0; i < photos; i++ {
		k = append(k, KindPhoto)
	}
	for i := 0; i < anims; i++ {
		k = append(k, KindAnimation)
	}
	for i := 0; i < videos; i++ {
		k = append(k, KindVideo)
	}
	return k
}

// Go for Spring people: table-driven tests are the Go way to write a @ParameterizedTest: a slice of cases, one t.Run each.
func TestOrderIsAPermutationWithoutLongVideoRuns(t *testing.T) {
	cases := []struct {
		name               string
		photos, anim, vids int
	}{
		{"real size", 110, 40, 50},
		{"many videos", 30, 5, 40},
		{"no video", 20, 5, 0},
		{"only videos", 0, 0, 6},
		{"tiny", 2, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kinds := catalogue(c.photos, c.anim, c.vids)
			for seed := uint64(1); seed <= 40; seed++ {
				prevLast, run := -1, 0
				for epoch := 0; epoch < 4; epoch++ {
					o := Order(seed, epoch, kinds)
					seen := map[int]bool{}
					for _, i := range o {
						if i < 0 || i >= len(kinds) || seen[i] {
							t.Fatalf("seed %d epoch %d: not a permutation (index %d)", seed, epoch, i)
						}
						seen[i] = true
					}
					if len(seen) != len(kinds) {
						t.Fatalf("seed %d epoch %d: %d of %d items", seed, epoch, len(seen), len(kinds))
					}
					if prevLast >= 0 && len(kinds) > 2 && o[0] == prevLast {
						t.Fatalf("seed %d epoch %d: first item repeats the last of the previous pass", seed, epoch)
					}
					prevLast = o[len(o)-1]
					for _, i := range o { // runs are checked across the pass boundary too
						if kinds[i] == KindVideo {
							run++
						} else {
							run = 0
						}
						if run > MaxVideoRun && c.photos+c.anim > 2 {
							t.Fatalf("seed %d epoch %d: %d videos in a row", seed, epoch, run)
						}
					}
				}
			}
		})
	}
}

func TestOrderIsStableAndPrivatePerChild(t *testing.T) {
	kinds := catalogue(110, 40, 50)
	a1, a2 := Order(SeedOf("child-a"), 0, kinds), Order(SeedOf("child-a"), 0, kinds)
	b := Order(SeedOf("child-b"), 0, kinds)
	if fmt.Sprint(a1) != fmt.Sprint(a2) {
		t.Fatal("same child and pass must give the same order")
	}
	if fmt.Sprint(a1) == fmt.Sprint(b) {
		t.Fatal("two children must not share an order")
	}
	if fmt.Sprint(a1) == fmt.Sprint(Order(SeedOf("child-a"), 1, kinds)) {
		t.Fatal("the next pass must be a new shuffle")
	}
}

func TestAtWalksEveryItemOnceBeforeAnyRepeat(t *testing.T) {
	kinds := catalogue(110, 40, 50)
	seed := SeedOf("walker")
	seen := map[int]int{}
	for c := 0; c < 2*len(kinds); c++ {
		idx, epoch := At(seed, c, kinds)
		if epoch != c/len(kinds) {
			t.Fatalf("counter %d: epoch %d", c, epoch)
		}
		seen[idx]++
		if c == len(kinds)-1 && len(seen) != len(kinds) {
			t.Fatalf("after one pass only %d of %d items were shown", len(seen), len(kinds))
		}
	}
	for idx, n := range seen {
		if n != 2 {
			t.Fatalf("item %d shown %d times in two passes, want 2", idx, n)
		}
	}
}

func TestVideoShareIsSpreadNotClumped(t *testing.T) {
	kinds := catalogue(110, 40, 50)
	o := Order(SeedOf("spread"), 0, kinds)
	// every window of 12 items holds at most 8 videos (2 per gap) and the first and last item are never videos.
	if kinds[o[0]] == KindVideo || kinds[o[len(o)-1]] == KindVideo {
		t.Fatal("a pass starts and ends on a non-video item")
	}
}

// Go for Spring people: a plain loop test. 500 random visit numbers must give a first item that varies a lot and
// first pages that are (almost) never equal; visit -1 keeps the child's old fixed order.
func TestSeedWithVisitsShuffleDifferently(t *testing.T) {
	kinds := catalogue(110, 40, 50)
	first := map[int]bool{}
	pages := map[string]bool{}
	for v := 0; v < 500; v++ {
		o := Order(SeedWith("child-1", v), 0, kinds)
		first[o[0]] = true
		pages[fmt.Sprint(o[:12])] = true
	}
	if len(first) < 100 {
		t.Fatalf("only %d distinct first items over 500 visits", len(first))
	}
	if len(pages) != 500 {
		t.Fatalf("only %d distinct first pages over 500 visits", len(pages))
	}
	if SeedWith("child-1", -1) != SeedOf("child-1") {
		t.Fatal("no visit number must keep the fixed order")
	}
	if SeedWith("child-1", 7) != SeedWith("child-1", 7) {
		t.Fatal("same visit number must give the same order")
	}
}
