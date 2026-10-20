package feedmedia

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The real seed: 200 items (110 photos, 40 animations, 50 videos), every file present, licences clean, within budget.
func TestRealSeedIsComplete(t *testing.T) {
	raw, err := os.ReadFile("../../seed/feed/items.json")
	if err != nil {
		t.Skip("seed/feed/items.json not built:", err)
	}
	dir := "../../seed/feed-media"
	items, problems := ParseAll(raw, dir)
	if len(problems) > 0 {
		t.Fatalf("%d problems, first: %s", len(problems), problems[0])
	}
	count := map[string]int{}
	var bytes int64
	withQuestion := 0
	files := map[string]bool{}
	for _, it := range items {
		count[it.Kind]++
		if it.FR.Question != "" {
			withQuestion++
		}
		for _, f := range []string{it.Src, it.SrcSmall, it.Poster} {
			if f != "" {
				files[f] = true
			}
		}
		for _, c := range it.Captions {
			files[c.Src] = true
		}
		if it.Kind == KindVideo && (it.DurationMS < 5000 || it.DurationMS > 30000) {
			t.Errorf("%s: video of %d ms, want 5 to 30 s", it.ID, it.DurationMS)
		}
	}
	for f := range files {
		st, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		bytes += st.Size()
		if strings.HasSuffix(f, "-"+f[strings.LastIndex(f, "-")+1:]) && strings.HasSuffix(f, ".mp4") && (st.Size() > 900*1024 || st.Size() < 20*1024) {
			t.Errorf("%s: %d bytes outside the clip budget", f, st.Size())
		}
	}
	if len(items) != 200 || count[KindPhoto] != 110 || count[KindAnimation] != 40 || count[KindVideo] != 50 {
		t.Fatalf("counts %v total %d, want 110/40/50 and 200", count, len(items))
	}
	if bytes > 60<<20 {
		t.Fatalf("media is %d MB, budget is 60", bytes>>20)
	}
	if withQuestion < 100 {
		t.Fatalf("only %d items have a micro question", withQuestion)
	}
}
