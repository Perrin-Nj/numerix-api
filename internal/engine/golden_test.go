package engine

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

const goldenFile = "testdata/choose_flag_off.golden"

// TestChooseFlagOffEqualsBaseline replays Choose over worlds, grades and seeds with the grade window OFF.
// The golden file was produced by the planner of origin/main before the grade-true change (T012).
func TestChooseFlagOffEqualsBaseline(t *testing.T) {
	gr := mustGraph(t)
	gr.SetGradeWindow(false)
	gr.SetBank([]BankEntry{{World: "echecs", Skill: "GEO-A01", Published: 14, ByGrade: map[int]int{1: 3, 2: 11}}})
	gr.SetBankOnly("echecs")
	now := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	var b strings.Builder
	for _, world := range []string{"nature", "echecs", "cuisine"} {
		for grade := 0; grade <= 6; grade++ {
			for seed := uint64(1); seed <= 3; seed++ {
				sel, err := Choose(gr, NewProfile("kid", gr), world, grade, NewRNG(seed), now)
				fmt.Fprintf(&b, "%s/%d/%d=%s:%s:%v\n", world, grade, seed, sel.SkillID, sel.Reason, err != nil)
			}
		}
	}
	if os.Getenv("UPDATE_GOLDEN") != "" {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(goldenFile, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != b.String() {
		t.Errorf("flag-off selections differ from the baseline golden file:\n%s", b.String())
	}
}
