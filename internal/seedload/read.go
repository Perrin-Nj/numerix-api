// Package seedload reads the seed files (seed/worlds.json, quests.json, misconceptions.json, questions/*.json,
// assets/*.json), validates them with package content, and applies only what changed to the database.
//
// Go for Spring people: this is the ApplicationRunner / Flyway-repeatable-migration of the content. The same code
// runs from cmd/seed (production deploys), from the admin import endpoint and from the tests.
package seedload

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Perrin-Nj/numerix-api/internal/assetstore"
	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/contentdb"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
)

// ExamplesFile is the stem of the demo question file (mixed worlds, for development and tests).
const ExamplesFile = "_examples"

// QuestionFile is one parsed seed/questions/<name>.json.
type QuestionFile struct {
	Name      string // file stem, "cuisine"
	World     string // the "world" field ("_examples" for the demo file)
	Questions []*content.Question
}

// Files is everything read from a seed directory.
type Files struct {
	Dir           string
	Worlds        []content.World
	Quests        []content.Quest
	Misconception []contentdb.Misconception
	QuestionFiles []QuestionFile
	Assets        assetstore.Seed
	Denylist      []string
	Names         map[string]bool
	Hash          string // sha256 of all file names and contents, stored in seed_runs.files_hash
}

// Questions returns every question of every file in file order.
func (f *Files) Questions() []*content.Question {
	var out []*content.Question
	for _, qf := range f.QuestionFiles {
		out = append(out, qf.Questions...)
	}
	return out
}

// ReadOptions changes what Read loads.
type ReadOptions struct {
	Exclude    []string // question file stems to skip ("_examples")
	SkipAssets bool     // do not read seed/assets (tests of questions only)
}

// readJSON decodes a file strictly.
func readJSON(path string, v any) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := content.DecodeStrict(raw, v); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return raw, nil
}

// Read loads and decodes a seed directory. It does not validate content (see Validate).
func Read(dir string, opt ReadOptions) (*Files, error) {
	f := &Files{Dir: dir}
	sum := sha256.New()
	feed := func(name string, raw []byte) {
		sum.Write([]byte(name))
		sum.Write(raw)
	}
	var wdoc struct {
		Schema int             `json:"schema"`
		Worlds []content.World `json:"worlds"`
	}
	raw, err := readJSON(filepath.Join(dir, "worlds.json"), &wdoc)
	if err != nil {
		return nil, fmt.Errorf("read worlds: %w", err)
	}
	feed("worlds.json", raw)
	f.Worlds = wdoc.Worlds

	var qdoc struct {
		Schema int             `json:"schema"`
		Quests []content.Quest `json:"quests"`
	}
	if raw, err = readJSON(filepath.Join(dir, "quests.json"), &qdoc); err != nil {
		return nil, fmt.Errorf("read quests: %w", err)
	}
	feed("quests.json", raw)
	f.Quests = qdoc.Quests

	var mdoc struct {
		Schema        int `json:"schema"`
		Misconception []struct {
			Code  string        `json:"code"`
			Skill string        `json:"skill,omitempty"`
			Desc  content.LText `json:"desc"`
		} `json:"misconceptions"`
	}
	if raw, err = readJSON(filepath.Join(dir, "misconceptions.json"), &mdoc); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read misconceptions: %w", err)
	} else if err == nil {
		feed("misconceptions.json", raw)
	}
	for _, m := range mdoc.Misconception {
		f.Misconception = append(f.Misconception, contentdb.Misconception{Code: m.Code, Skill: m.Skill, DescFR: m.Desc.FR, DescEN: m.Desc.EN, Source: "seed"})
	}

	files, err := filepath.Glob(filepath.Join(dir, "questions", "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	for _, path := range files {
		stem := strings.TrimSuffix(filepath.Base(path), ".json")
		if slices.Contains(opt.Exclude, stem) {
			continue
		}
		var doc struct {
			World     string              `json:"world"`
			Schema    int                 `json:"schema"`
			Questions []*content.Question `json:"questions"`
		}
		raw, err := readJSON(path, &doc)
		if err != nil {
			return nil, err
		}
		feed("questions/"+stem+".json", raw)
		if doc.Schema != 1 {
			return nil, fmt.Errorf("%s: schema must be 1", filepath.Base(path))
		}
		for _, q := range doc.Questions {
			if q.Source == "" {
				q.Source = "seed:" + stem + ".json"
			}
			if q.Version == 0 {
				q.Version = 1
			}
		}
		f.QuestionFiles = append(f.QuestionFiles, QuestionFile{Name: stem, World: doc.World, Questions: doc.Questions})
	}

	if !opt.SkipAssets {
		if f.Assets, err = assetstore.LoadSeed(filepath.Join(dir, "assets")); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"assets/assets.json", "assets/vocab.json"} {
		if raw, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			feed(name, raw)
		}
	}
	if f.Denylist, err = readWordList(filepath.Join(dir, "schema", "denylist.txt")); err != nil {
		return nil, err
	}
	names, err := readWordList(filepath.Join(dir, "schema", "names.txt"))
	if err != nil {
		return nil, err
	}
	if len(names) > 0 {
		f.Names = map[string]bool{}
		for _, n := range names {
			f.Names[n] = true
		}
	}
	f.Hash = hex.EncodeToString(sum.Sum(nil))
	return f, nil
}

// readWordList reads one entry per line, ignoring blanks and # comments; entries are folded (lowercase, no accents).
// A missing file gives an empty list.
func readWordList(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, content.Fold(line))
	}
	return out, nil
}

// CurriculumMisconceptions lists the typical-error codes of the embedded curriculum graph.
func CurriculumMisconceptions(g *engine.Graph) []contentdb.Misconception {
	seen := map[string]bool{}
	var out []contentdb.Misconception
	for _, id := range g.All() {
		sk, _ := g.Get(id)
		for _, te := range sk.TypicalErrors {
			if seen[te.Code] {
				continue
			}
			seen[te.Code] = true
			out = append(out, contentdb.Misconception{Code: te.Code, Skill: id, DescFR: te.Desc, DescEN: te.Desc, Source: "curriculum"})
		}
	}
	return out
}
