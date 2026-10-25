package app

import (
	"os"
	"slices"
	"testing"
)

// focusWorlds keeps the real production default so one test can check it (the rest of the suite plays in every world).
var focusWorlds = slices.Clone(plDefaultWorlds)

// TestMain runs before every test in this package (Spring: a @BeforeAll for the whole suite).
// Production children start with only the focus worlds; the older tests play in nature, cuisine and so on,
// so for them every world starts enabled.
func TestMain(m *testing.M) {
	plDefaultWorlds = plWorldIDs
	os.Exit(m.Run())
}

// TestFocusWorldsAreChessMedicineSpace pins the production default.
func TestFocusWorldsAreChessMedicineSpace(t *testing.T) {
	want := []string{"echecs", "medecine", "espace"}
	if !slices.Equal(focusWorlds, want) {
		t.Fatalf("default worlds = %v, want %v", focusWorlds, want)
	}
}
