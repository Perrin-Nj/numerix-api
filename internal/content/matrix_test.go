package content_test

// T007: seed/curriculum/matrix.json (grade x strand x skill x difficulty x style) is the level guide behind grade-true
// play. Every skill used by the three worlds must be in it, the matrix must agree with the curriculum graph, and the
// questions must fall inside it. The three draft sample worlds must pass; the existing worlds are reported as content
// to-do (they were written before the matrix existed).

import (
	"encoding/json"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/seedload"
)

type matrixRow struct {
	Grade  int    `json:"grade"`
	Strand string `json:"strand"`
	Skill  string `json:"skill"`
	Diff   [2]int `json:"difficulty_range"`
	Style  string `json:"style"`
	Source string `json:"source_ref"`
}

func loadMatrix(t *testing.T) []matrixRow {
	t.Helper()
	raw, err := os.ReadFile("../../seed/curriculum/matrix.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Rows []matrixRow `json:"rows"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Rows
}

func allowed(rows []matrixRow, q *content.Question) bool {
	style := q.Style
	if style == "" {
		style = content.StyleContext
	}
	for _, r := range rows {
		if r.Skill == q.Skill && r.Grade == q.Grade && q.Difficulty >= r.Diff[0] && q.Difficulty <= r.Diff[1] {
			if r.Style == "any" || r.Style == style || (r.Style == "drill" && style == content.StyleContext) {
				return true
			}
		}
	}
	return false
}

func TestMatrixShapeAndGraphDiffs(t *testing.T) {
	rows := loadMatrix(t)
	gr, err := engine.LoadGraph()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[[2]any]bool{}
	var diffs []string
	for _, r := range rows {
		if r.Grade < 1 || r.Grade > 4 || r.Diff[0] < 1 || r.Diff[1] > 5 || r.Diff[0] > r.Diff[1] || !slices.Contains([]string{"drill", "context", "any"}, r.Style) || r.Source == "" || r.Strand == "" {
			t.Errorf("bad row %+v", r)
		}
		if r.Style == "drill" && r.Grade > 2 {
			t.Errorf("drills belong to grades 1 and 2 only: %+v", r)
		}
		k := [2]any{r.Skill, r.Grade}
		if r.Style != "any" && seen[k] {
			t.Errorf("duplicate row for %s grade %d", r.Skill, r.Grade)
		}
		seen[k] = true
		sk, err := gr.Get(r.Skill)
		if err != nil {
			t.Errorf("matrix skill %s is not in the curriculum graph", r.Skill)
			continue
		}
		if r.Grade < sk.YearIntro { // a documented difference: the level guide serves a skill before the graph introduces it
			diffs = append(diffs, r.Skill+" grade "+string(rune('0'+r.Grade))+" before annee_intro "+string(rune('0'+sk.YearIntro)))
		}
	}
	sort.Strings(diffs)
	t.Logf("matrix vs pda-*.json: %d rows, %d differences (served before annee_intro):\n  %s", len(rows), len(diffs), strings.Join(diffs, "\n  "))
}

func TestEverySkillOfTheThreeWorldsIsInTheMatrix(t *testing.T) {
	rows := loadMatrix(t)
	files, err := seedload.Read("../../seed", seedload.ReadOptions{SkipAssets: true, Exclude: []string{seedload.ExamplesFile}})
	if err != nil {
		t.Fatal(err)
	}
	inMatrix := map[string]bool{}
	for _, r := range rows {
		inMatrix[r.Skill] = true
	}
	for _, q := range files.Quests {
		switch q.World {
		case "echecs", "medecine", "espace", "histoires", "english", "francais":
			if !inMatrix[q.Skill] {
				t.Errorf("quest %s uses %s, which is not in the matrix", q.ID, q.Skill)
			}
		}
	}
	// the questions: the draft sample worlds must pass, the older worlds are listed
	bad := map[string][]string{}
	for _, q := range files.Questions() {
		world := strings.SplitN(q.ID, ".", 2)[0]
		if q.Status != content.StatusPublished || allowed(rows, q) {
			continue
		}
		bad[world] = append(bad[world], q.ID)
	}
	for _, w := range []string{"histoires", "english", "francais"} {
		if len(bad[w]) > 0 {
			t.Errorf("sample questions outside the matrix: %v", bad[w])
		}
	}
	for _, w := range []string{"echecs", "medecine", "espace"} {
		t.Logf("content to-do: %s has %d published questions outside the matrix", w, len(bad[w]))
	}
}
