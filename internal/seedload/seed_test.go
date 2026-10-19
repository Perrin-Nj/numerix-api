package seedload

import (
	"testing"

	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
)

// seedDir is the repository's seed directory, seen from this package.
const seedDir = "../../seed"

func TestSeedFilesValidate(t *testing.T) {
	g, err := engine.LoadGraph()
	if err != nil {
		t.Fatal(err)
	}
	f, err := Read(seedDir, ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	issues := f.Validate(g)
	for _, is := range issues {
		if is.Warning {
			t.Logf("%s", is)
		}
	}
	for _, is := range content.Errors(issues) {
		t.Errorf("%s", is)
	}
}
