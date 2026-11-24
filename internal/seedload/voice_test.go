package seedload

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Every class e line of the voice script is a question prompt: its qid must be a live question of the world in its id,
// and its text must be that question's prompt in the line's language. A renamed question or reworded prompt fails here
// instead of leaving a recorded line that voices something else.
func TestVoiceScriptMatchesQuestions(t *testing.T) {
	raw, err := os.ReadFile(seedDir + "/voice/script.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Lines []struct {
			ID, Key, Class, Lang, Text, QID string
		} `json:"lines"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	f, err := Read(seedDir, ReadOptions{SkipAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	prompts := map[string][2]string{}
	for _, q := range f.Questions() {
		prompts[q.ID] = [2]string{q.Prompt.FR, q.Prompt.EN}
	}
	keys, perWorld := map[string]bool{}, map[string]int{}
	for _, l := range s.Lines {
		if keys[l.Key] {
			t.Errorf("duplicate voice key %s", l.Key)
		}
		keys[l.Key] = true
		if l.Class != "e" {
			continue
		}
		p, ok := prompts[l.QID]
		if !ok {
			t.Errorf("%s: qid %q is not a question", l.Key, l.QID)
			continue
		}
		if l.ID != "e."+l.QID {
			t.Errorf("%s: id must be e.<qid> (%s)", l.Key, l.QID)
		}
		want := p[0]
		if l.Lang == "en-CA" {
			want = p[1]
		}
		if strings.TrimSpace(l.Text) != strings.TrimSpace(want) {
			t.Errorf("%s: text %q differs from the prompt %q", l.Key, l.Text, want)
		}
		perWorld[strings.SplitN(l.QID, ".", 2)[0]]++
	}
	for _, w := range []string{"echecs", "medecine", "espace"} {
		if perWorld[w] == 0 {
			t.Errorf("no voice line for world %s", w)
		}
	}
}
