package app

// Go for Spring people: an integration test of the Mongo producers and resolver. It plays a real chapter over
// HTTP on a throw-away SQLite file, then checks the outbox rows and the pseudonymous documents.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/internal/mongosync"
)

// playPerfectChapter answers all 3 beats correctly.
func playPerfectChapter(t *testing.T, f feedFixture) {
	t.Helper()
	sid := f.startChapter(t, "ARI-OPE-A03a")
	for i := 0; i < 3; i++ {
		_, beat, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", f.childTok, "{}", nil)
		p := f.problemOf(t, beat["id"].(string))
		f.answer(t, sid, beat["id"].(string), p.Reponse, uuid.NewString())
	}
}

func outboxKinds(t *testing.T, f feedFixture) map[string]int {
	t.Helper()
	rows, err := f.s.Store.R.Query(`SELECT kind, COUNT(*) FROM outbox GROUP BY kind`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			t.Fatal(err)
		}
		out[k] = n
	}
	return out
}

func TestSyncOffWritesNoOutboxRows(t *testing.T) {
	f := newFeedFixture(t, true) // f.s.Sync is nil: sync off
	playPerfectChapter(t, f)
	if k := outboxKinds(t, f); len(k) != 0 {
		t.Fatalf("sync off must not queue events: %v", k)
	}
}

func TestChapterQueuesEventsAndResolvesWithoutPII(t *testing.T) {
	f := newFeedFixture(t, true)
	f.s.Sync = mongosync.NewOutbox(true)
	playPerfectChapter(t, f)
	k := outboxKinds(t, f)
	if k[mongosync.KindBests] != 1 || k[mongosync.KindFamily] != 1 || k[mongosync.KindStats] != 1 {
		t.Fatalf("one event per kind expected: %v", k)
	}
	pseudo := mongosync.NewPseudonymizer(strings.Repeat("s", 32))
	res := f.s.SyncResolver(pseudo)
	rows, err := f.s.Store.R.Query(`SELECT id, kind, key, payload, created_at FROM outbox ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ops := map[string]mongosync.Op{}
	for rows.Next() {
		var ev mongosync.Event
		var at string
		if err := rows.Scan(&ev.ID, &ev.Kind, &ev.Key, &ev.Payload, &at); err != nil {
			t.Fatal(err)
		}
		ev.CreatedAt = feedTime(at)
		op, err := res.Resolve(context.Background(), ev)
		if err != nil {
			t.Fatalf("resolve %s: %v", ev.Kind, err)
		}
		ops[ev.Kind] = op
		raw, _ := json.Marshal(op)
		// Law 25: no name, email, birth data, nor any real id may reach Mongo.
		for _, banned := range []string{"Zoé", "Tremblay", "Max", "a@b.c", f.childID, f.parentID, "11111111"} {
			if strings.Contains(string(raw), banned) {
				t.Fatalf("%s doc leaks %q: %s", ev.Kind, banned, raw)
			}
		}
	}
	b := ops[mongosync.KindBests]
	if b.Delete || b.Doc["chapters_completed"] != 1 || b.Doc["longest_streak_days"].(int) < 1 {
		t.Fatalf("personal best doc: %+v", b)
	}
	fam := ops[mongosync.KindFamily]
	if fam.Delete || len(fam.Doc["members"].([]map[string]any)) != 2 || fam.Doc["total_chapters"] != 1 {
		t.Fatalf("family doc (two siblings, one chapter): %+v", fam)
	}
	if st := ops[mongosync.KindStats]; !st.Delete { // only one child: under the k-anonymity floor
		t.Fatalf("stats bucket below the floor must not be published: %+v", st)
	}
}

func TestStatsKAnonymityFloor(t *testing.T) {
	for _, tc := range []struct {
		children int
		publish  bool
	}{{0, false}, {1, false}, {4, false}, {5, true}, {40, true}} {
		op := statsOp("nature", "ARI-OPE-A03a", tc.children, 50, 25)
		if op.Delete == tc.publish {
			t.Fatalf("children=%d: delete=%v, want publish=%v", tc.children, op.Delete, tc.publish)
		}
		if tc.publish && (op.Doc["correct_rate"] != 0.5 || op.Doc["children"] != tc.children) {
			t.Fatalf("doc: %+v", op.Doc)
		}
	}
}

func TestDeleteChildQueuesErasure(t *testing.T) {
	f := newFeedFixture(t, true)
	f.s.Sync = mongosync.NewOutbox(true)
	if code, _, raw := f.gcall(t, "DELETE", "/v1/children/"+f.childID, f.parentTok, "", nil); code != 204 {
		t.Fatalf("delete child: %d %s", code, raw)
	}
	if k := outboxKinds(t, f); k[mongosync.KindBests] != 1 || k[mongosync.KindFamily] != 1 {
		t.Fatalf("erasure events: %v", k)
	}
	op, err := f.s.SyncResolver(mongosync.NewPseudonymizer(strings.Repeat("s", 32))).Resolve(context.Background(),
		mongosync.Event{Kind: mongosync.KindBests, Key: f.childID})
	if err != nil || !op.Delete {
		t.Fatalf("a deleted child must resolve to a delete: %+v %v", op, err)
	}
}
