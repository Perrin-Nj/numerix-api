package app

// Go for Spring people: same style as feed_test.go. The Discover items are synthetic here (30 rows written with
// feedmedia.Apply, tiny files in a temp MEDIA_DIR), so the tests do not depend on the real 200-item seed.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/feedmedia"
)

// discoverItems makes n valid items: every 5th is a video, every 4th an animation, the rest photos; grade_min 1..4.
func discoverItems(t *testing.T, dir string, n int) []feedmedia.Item {
	t.Helper()
	var out []feedmedia.Item
	write := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("x", 2000)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("item-%02d", i)
		it := feedmedia.Item{
			ID: id, Kind: feedmedia.KindPhoto, World: "nature", Topic: "counting", GradeMin: 1 + i%4, Tags: []string{"counting"},
			FR:  feedmedia.Text{Title: "Titre " + id, Caption: "Fait " + id, Alt: "Alt " + id, Question: "Combien ?", Answer: "Deux."},
			EN:  feedmedia.Text{Title: "Title " + id, Caption: "Fact " + id, Alt: "Alt " + id, Question: "How many?", Answer: "Two."},
			Src: id + "-aaaaaaaa.webp", SrcSmall: id + "-s-aaaaaaaa.webp", Width: 960, Height: 720, LQIP: "data:image/webp;base64,AAAA",
			SourceURL: "https://example.org/" + id, Creator: "Someone", Licence: "CC0 1.0", Attribution: "Someone, CC0 1.0",
		}
		write(it.Src)
		write(it.SrcSmall)
		switch {
		case i%5 == 4:
			it.Kind, it.Src, it.Poster, it.DurationMS = feedmedia.KindVideo, id+"-bbbbbbbb.mp4", id+"-poster-bbbbbbbb.webp", 12000
			it.SrcSmall = ""
			it.Captions = []feedmedia.Caption{{Lang: "fr-CA", Label: "Francais", Src: id + "-fr-cccccccc.vtt"}, {Lang: "en-CA", Label: "English", Src: id + "-en-cccccccc.vtt"}}
			write(it.Src)
			write(it.Poster)
			write(it.Captions[0].Src)
			write(it.Captions[1].Src)
		case i%4 == 3:
			it.Kind, it.Src, it.Poster, it.DurationMS = feedmedia.KindAnimation, id+"-dddddddd.mp4", id+"-poster-dddddddd.webp", 6000
			it.SrcSmall = ""
			write(it.Src)
			write(it.Poster)
		}
		if p := it.Validate(dir); len(p) > 0 {
			t.Fatalf("synthetic item invalid: %v", p)
		}
		out = append(out, it)
	}
	return out
}

// discoverFixture is a feed family with n curated items loaded and MEDIA_DIR pointing to their files.
func discoverFixture(t *testing.T, n int) (feedFixture, []feedmedia.Item) {
	t.Helper()
	f := newFeedFixture(t, true)
	dir := t.TempDir()
	f.s.Config.MediaDir = dir
	items := discoverItems(t, dir, n)
	if _, err := feedmedia.Apply(context.Background(), f.s.Store, items, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.s.Feed.Forget()
	return f, items
}

func discoverPath(f feedFixture, cursor string, limit int) string {
	p := fmt.Sprintf("/v1/children/%s/discover?limit=%d", f.childID, limit)
	if cursor != "" {
		p += "&cursor=" + cursor
	}
	return p
}

// walk reads `pages` pages and returns every item (id and kind) in order, plus the last body.
func (f feedFixture) walk(t *testing.T, pages, limit int) (ids, kinds []string, last map[string]any) {
	t.Helper()
	cursor := ""
	for p := 0; p < pages; p++ {
		code, body := f.call(t, "GET", discoverPath(f, cursor, limit), f.childTok, "")
		if code != 200 {
			t.Fatalf("page %d: %d %v", p, code, body)
		}
		for _, raw := range body["items"].([]any) {
			it := raw.(map[string]any)
			ids = append(ids, it["id"].(string))
			kinds = append(kinds, it["kind"].(string))
		}
		cursor = body["next_cursor"].(string)
		last = body
	}
	return ids, kinds, last
}

func TestDiscoverPagingNoRepeatKindMixAndPuzzles(t *testing.T) {
	f, items := discoverFixture(t, 30)
	// 30 items, all eligible (the fixture child has no grade). Media slots per page of 12: 10 or 11, so 4 pages
	// hold at least 40 media slots: one full pass (30) plus 10 of the next.
	ids, kinds, last := f.walk(t, 4, 12)
	if len(ids) != 48 {
		t.Fatalf("%d items over 4 pages, want 48", len(ids))
	}
	seen := map[string]int{}
	videoRun := 0
	for i, id := range ids {
		if i%8 == 7 {
			if kinds[i] != "puzzle" || !strings.HasPrefix(id, "feed-") {
				t.Fatalf("slot %d must be a puzzle, got %s %s", i, kinds[i], id)
			}
			videoRun = 0
			continue
		}
		if !strings.HasPrefix(id, "m-") || kinds[i] == "puzzle" {
			t.Fatalf("slot %d must be media, got %s %s", i, kinds[i], id)
		}
		if seen[id]++; seen[id] > 1 && len(seen) < len(items) {
			t.Fatalf("%s repeated before every item was shown (slot %d)", id, i)
		}
		if kinds[i] == "video" {
			if videoRun++; videoRun > feedmedia.MaxVideoRun {
				t.Fatalf("slot %d: %d videos in a row", i, videoRun)
			}
		} else {
			videoRun = 0
		}
	}
	if len(seen) != len(items) {
		t.Fatalf("after one pass only %d of %d items were shown", len(seen), len(items))
	}
	if int(last["cycle_size"].(float64)) != 30 || int(last["cycle"].(float64)) != 1 {
		t.Fatalf("cycle fields %v %v", last["cycle"], last["cycle_size"])
	}
}

func TestDiscoverCursorIsStableOpaqueAndBoundToTheChild(t *testing.T) {
	f, _ := discoverFixture(t, 30)
	f.sql(t, `UPDATE children SET grade = 3 WHERE id = ?`, "22222222-2222-4222-8222-222222222222") // other child exists
	_, first := f.call(t, "GET", discoverPath(f, "", 5), f.childTok, "")
	cursor := first["next_cursor"].(string)
	if strings.ContainsAny(cursor, ".=+/ ") || len(cursor) < 8 {
		t.Fatalf("cursor %q should be opaque url-safe text", cursor)
	}
	_, a := f.call(t, "GET", discoverPath(f, cursor, 5), f.childTok, "")
	_, b := f.call(t, "GET", discoverPath(f, cursor, 5), f.childTok, "")
	if fmt.Sprint(a["items"]) != fmt.Sprint(b["items"]) || a["next_cursor"] != b["next_cursor"] {
		t.Fatal("the same cursor must return the same page")
	}
	for name, bad := range map[string]string{"garbage": "zzzz", "tampered": cursor[:len(cursor)-2] + "AA", "empty number": "Lg"} {
		if code, _ := f.call(t, "GET", discoverPath(f, bad, 5), f.childTok, ""); code != 422 {
			t.Errorf("%s cursor: status %d, want 422", name, code)
		}
	}
	// A cursor issued to one child is refused for another (the other child has no consent: 403 comes first, so use the id check directly).
	if _, ok := parseDiscoverCursor("someone-else", cursor); ok {
		t.Fatal("a cursor must not work for another child")
	}
}

func TestDiscoverOrderDiffersPerChildAndLanguageSwitches(t *testing.T) {
	f, _ := discoverFixture(t, 30)
	idsA, _, _ := f.walk(t, 1, 12)
	a := discoverSlots("11111111-1111-4111-8111-111111111111", -1, 0, 7, discoverEligibleForTest(t, f))
	b := discoverSlots("22222222-2222-4222-8222-222222222222", -1, 0, 7, discoverEligibleForTest(t, f))
	same := 0
	for i := range a {
		if a[i].media.ID == b[i].media.ID {
			same++
		}
	}
	if same == len(a) {
		t.Fatal("two children got the same first seven items")
	}
	req := httptest.NewRequest("GET", discoverPath(f, "", 2), nil)
	req.Header.Set("Authorization", "Bearer "+f.childTok)
	req.Header.Set("Accept-Language", "en-CA")
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"title":"Title `) || len(idsA) != 12 {
		t.Fatalf("english titles expected: %s", rec.Body.String())
	}
}

func discoverEligibleForTest(t *testing.T, f feedFixture) []feedmedia.Item {
	t.Helper()
	all, err := f.s.Feed.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return all
}

func TestDiscoverRespectsGradeBand(t *testing.T) {
	cases := []struct {
		grade    int
		eligible int // items with grade_min <= grade+1 among 30 (grade_min cycles 1,2,3,4)
	}{{1, 16}, {2, 23}, {3, 30}, {6, 30}}
	for _, c := range cases {
		t.Run(fmt.Sprint("grade ", c.grade), func(t *testing.T) {
			f, _ := discoverFixture(t, 30)
			f.sql(t, `UPDATE children SET grade = ? WHERE id = ?`, c.grade, f.childID)
			_, _, last := f.walk(t, 1, 12)
			if got := int(last["cycle_size"].(float64)); got != c.eligible {
				t.Fatalf("cycle_size %d, want %d", got, c.eligible)
			}
			ids, _, _ := f.walk(t, 3, 12)
			for _, id := range ids {
				if !strings.HasPrefix(id, "m-") {
					continue
				}
				var n int
				_, _ = fmt.Sscanf(strings.TrimPrefix(id, "m-item-"), "%d", &n)
				if 1+n%4 > c.grade+1 {
					t.Fatalf("item %s (grade_min %d) shown to grade %d", id, 1+n%4, c.grade)
				}
			}
		})
	}
}

func TestDiscoverNeedsConsentAndChildRole(t *testing.T) {
	f, _ := discoverFixture(t, 10)
	if code, _ := f.call(t, "GET", discoverPath(f, "", 5), f.parentTok, ""); code != 403 {
		t.Errorf("parent: %d", code)
	}
	if code, _ := f.call(t, "GET", discoverPath(f, "", 5), f.otherChildTok, ""); code != 403 {
		t.Errorf("other child: %d", code)
	}
	if code, _ := f.call(t, "GET", discoverPath(f, "", 5), "", ""); code != 401 {
		t.Errorf("anonymous: %d", code)
	}
}

func TestDiscoverReactionsReuseTheFeedEndpoints(t *testing.T) {
	f, _ := discoverFixture(t, 30)
	ids, _, _ := f.walk(t, 1, 12)
	media := ids[0]
	if code, _ := f.call(t, "PUT", "/v1/children/"+f.childID+"/feed/"+media+"/reaction", f.childTok, `{"liked":true,"saved":true}`); code != 200 {
		t.Fatalf("like media: %d", code)
	}
	if code, _ := f.call(t, "PUT", "/v1/children/"+f.childID+"/feed/m-not-in-the-seed/reaction", f.childTok, `{"liked":true,"saved":false}`); code != 404 {
		t.Fatalf("unknown media id must be 404, got %d", code)
	}
	_, page := f.call(t, "GET", discoverPath(f, "", 12), f.childTok, "")
	first := page["items"].([]any)[0].(map[string]any)
	if first["liked"] != true || first["saved"] != true {
		t.Fatalf("the like must come back on the item: %v", first)
	}
}

func TestFeedMediaEndpointIsPublicImmutableAndRanged(t *testing.T) {
	f, items := discoverFixture(t, 10)
	var video feedmedia.Item
	for _, it := range items {
		if it.Kind == feedmedia.KindVideo {
			video = it
			break
		}
	}
	get := func(path string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil) // no Authorization header: the route is public
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, req)
		return rec
	}
	p := "/v1/feed-media/" + video.Src
	full := get(p, nil)
	if full.Code != 200 || full.Header().Get("Content-Type") != "video/mp4" || full.Header().Get("Accept-Ranges") != "bytes" ||
		full.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || full.Body.Len() != 2000 {
		t.Fatalf("full: %d %v", full.Code, full.Header())
	}
	if r := get(p, map[string]string{"Range": "bytes=100-199"}); r.Code != 206 || r.Body.Len() != 100 || r.Header().Get("Content-Range") != "bytes 100-199/2000" {
		t.Fatalf("range: %d %v", r.Code, r.Header())
	}
	if r := get(p, map[string]string{"If-None-Match": full.Header().Get("ETag")}); r.Code != http.StatusNotModified {
		t.Fatalf("etag: %d", r.Code)
	}
	if r := get("/v1/feed-media/"+video.Captions[0].Src, nil); r.Code != 200 || !strings.HasPrefix(r.Header().Get("Content-Type"), "text/vtt") {
		t.Fatalf("captions: %d %v", r.Code, r.Header())
	}
	for _, bad := range []string{"/v1/feed-media/missing-aaaaaaaa.mp4", "/v1/feed-media/..%2Fnumerix.db", "/v1/feed-media/readme.txt"} {
		if r := get(bad, nil); r.Code != 404 && r.Code != 422 {
			t.Errorf("%s: %d", bad, r.Code)
		}
	}
}

func TestFeedMediaApplyIsIdempotentAndRemovesStale(t *testing.T) {
	f, items := discoverFixture(t, 12)
	ctx := context.Background()
	res, err := feedmedia.Apply(ctx, f.s.Store, items, time.Now())
	if err != nil || res.Unchanged != 12 || res.Inserted+res.Updated+res.Deleted != 0 {
		t.Fatalf("second run %+v %v", res, err)
	}
	items[0].FR.Title = "Nouveau titre"
	res, _ = feedmedia.Apply(ctx, f.s.Store, items[:11], time.Now())
	if res.Updated != 1 || res.Deleted != 1 {
		t.Fatalf("after edit and removal %+v", res)
	}
}

func TestDiscoverValidateRefusesBadItems(t *testing.T) {
	dir := t.TempDir()
	good := discoverItems(t, dir, 1)[0]
	mut := map[string]func(*feedmedia.Item){
		"em dash":       func(i *feedmedia.Item) { i.EN.Caption = "Nice — fact" },
		"sa licence":    func(i *feedmedia.Item) { i.Licence = "CC BY-SA 4.0" },
		"nc licence":    func(i *feedmedia.Item) { i.Licence = "CC BY-NC 4.0" },
		"no alt":        func(i *feedmedia.Item) { i.FR.Alt = "" },
		"half qa":       func(i *feedmedia.Item) { i.EN.Answer = "" },
		"unknown world": func(i *feedmedia.Item) { i.World = "volcano" },
		"missing file":  func(i *feedmedia.Item) { i.Src = "ghost-aaaaaaaa.webp" },
		"http source":   func(i *feedmedia.Item) { i.SourceURL = "http://example.org" },
	}
	for name, m := range mut {
		it := good
		m(&it)
		if len(it.Validate(dir)) == 0 {
			t.Errorf("%s: must be refused", name)
		}
	}
}

// The seed query parameter reshuffles: same seed gives the same pages, another seed another first page, and a walk
// with one seed shows no media item twice inside one pass.
func TestDiscoverSeedReshuffles(t *testing.T) {
	f, _ := discoverFixture(t, 30)
	first := func(seed int) []string {
		_, p := f.call(t, "GET", discoverPath(f, "", 12)+fmt.Sprintf("&seed=%d", seed), f.childTok, "")
		var ids []string
		for _, it := range p["items"].([]any) {
			ids = append(ids, it.(map[string]any)["id"].(string))
		}
		return ids
	}
	if fmt.Sprint(first(5)) != fmt.Sprint(first(5)) {
		t.Fatal("same seed must give the same first page")
	}
	distinct := map[string]bool{}
	for s := 0; s < 5; s++ {
		distinct[fmt.Sprint(first(1000+s*7919))] = true
	}
	if len(distinct) < 5 {
		t.Fatalf("5 seeds gave only %d distinct first pages", len(distinct))
	}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 2; page++ {
		_, p := f.call(t, "GET", discoverPath(f, cursor, 12)+"&seed=42", f.childTok, "")
		for _, it := range p["items"].([]any) {
			id := it.(map[string]any)["id"].(string)
			if strings.HasPrefix(id, "m-") && seen[id] {
				t.Fatalf("%s repeated inside one pass", id)
			}
			seen[id] = true
		}
		cursor = p["next_cursor"].(string)
	}
}
