package app

// 3D scene delivery: Question.scene only for an approved id with platform.scenes_enabled on; AnswerResult.scene_reveal only with a released reveal.

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/internal/appconfig"
	"github.com/Perrin-Nj/numerix-api/internal/seedload"
)

const sceneDoc = `{"question":{"v":1,"archetype":"x.q"},"reveal":{"bind":{"left":{"from":"answer","reveal_only":true}}},"hash":"abc"}`

// sceneBeat seeds nine clones that all carry a scene document, approves them (or not), and returns the first beat.
func sceneBeat(t *testing.T, approve, flag bool) (f feedFixture, sid, beatID string, qid string) {
	t.Helper()
	f = newFeedFixture(t, true)
	clones := onlyClones("cuisine.fournee.901", 9)
	seedBank(t, f, func(files *seedload.Files) {
		clones(files)
		files.Scenes = map[string]json.RawMessage{}
		for i := 1; i <= 9; i++ {
			files.Scenes[fmt.Sprintf("cuisine.fournee.%d", 800+i)] = json.RawMessage(sceneDoc)
		}
	})
	old := approvedScenes
	t.Cleanup(func() { approvedScenes = old })
	approvedScenes = map[string]bool{}
	if approve {
		for i := 1; i <= 9; i++ {
			approvedScenes[fmt.Sprintf("cuisine.fournee.%d", 800+i)] = true
		}
	}
	f.setPlatform(t, func(p *appconfig.Platform) { p.ScenesEnabled = flag })
	sid = f.startIn(t, "cuisine", "ARI-OPE-A02c")
	beat, _ := f.nextBeat(t, sid)
	beatID = beat["id"].(string)
	return f, sid, beatID, f.curated(t, beatID).ID
}

func questionOf(t *testing.T, f feedFixture, sid string) map[string]any {
	t.Helper()
	_, raw := f.nextBeat(t, sid) // the open beat is returned again
	var b struct{ Question map[string]any }
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		t.Fatal(err)
	}
	return b.Question
}

func TestSceneOnlyForApprovedIdWithFlagOn(t *testing.T) {
	for _, c := range []struct {
		name          string
		approve, flag bool
		want          bool
	}{{"approved and on", true, true, true}, {"not approved", false, true, false}, {"flag off", true, false, false}} {
		t.Run(c.name, func(t *testing.T) {
			f, sid, _, _ := sceneBeat(t, c.approve, c.flag)
			q := questionOf(t, f, sid)
			_, has := q["scene"]
			if has != c.want {
				t.Fatalf("scene present=%v want %v: %v", has, c.want, q)
			}
			if c.want {
				if sc := q["scene"].(map[string]any); sc["archetype"] != "x.q" {
					t.Fatalf("scene is not the question part: %v", sc)
				}
			}
		})
	}
}

func TestSceneRevealOnlyWithReleasedAnswer(t *testing.T) {
	f, sid, beatID, _ := sceneBeat(t, true, true)
	q := f.curated(t, beatID)
	// the first answer is wrong with a second try open: verdict only, no reveal part
	code, res, raw := f.answerAttempt(t, sid, beatID, json.RawMessage(q.CommonErrors[0].Value), uuid.NewString(), 1)
	if code != 200 || res["retry_available"] != true {
		t.Fatalf("withheld: %d %s", code, raw)
	}
	if _, has := res["scene_reveal"]; has {
		t.Fatalf("scene_reveal leaked in the retry window: %s", raw)
	}
	code, res, raw = f.answerAttempt(t, sid, beatID, json.RawMessage(q.Answer.Value), uuid.NewString(), 2)
	rv, ok := res["scene_reveal"].(map[string]any)
	if code != 200 || !ok || rv["bind"] == nil {
		t.Fatalf("released reveal must carry scene_reveal: %d %s", code, raw)
	}
}

func TestSceneRevealLegacyAnswerAndOldClients(t *testing.T) {
	f, sid, beatID, _ := sceneBeat(t, true, true)
	q := f.curated(t, beatID)
	if _, res, raw := f.answerAttempt(t, sid, beatID, json.RawMessage(q.Answer.Value), uuid.NewString(), 0); res["scene_reveal"] == nil {
		t.Fatalf("legacy answer is a released reveal: %s", raw)
	}
	g, gsid, gbeat, _ := sceneBeat(t, false, true) // not approved: no trace of the feature
	gq := g.curated(t, gbeat)
	if _, res, raw := g.answerAttempt(t, gsid, gbeat, json.RawMessage(gq.Answer.Value), uuid.NewString(), 0); res["scene_reveal"] != nil {
		t.Fatalf("unapproved question leaked scene_reveal: %s", raw)
	} else if _, has := res["scene_reveal"]; has {
		t.Fatalf("key must be absent: %s", raw)
	}
}

func TestSeedSceneIsIdempotent(t *testing.T) {
	f, _, _, qid := sceneBeat(t, true, true)
	read := func() (string, string) {
		var sc, upd string
		if err := f.s.Store.R.QueryRow(`SELECT scene, updated_at FROM bank_questions WHERE id = ?`, qid).Scan(&sc, &upd); err != nil {
			t.Fatal(err)
		}
		return sc, upd
	}
	sc1, upd1 := read()
	seedBank(t, f, func(files *seedload.Files) {
		onlyClones("cuisine.fournee.901", 9)(files)
		files.Scenes = map[string]json.RawMessage{qid: json.RawMessage(sceneDoc), "no.such.id": json.RawMessage(sceneDoc)}
	})
	if sc2, upd2 := read(); sc1 != sceneDoc || sc2 != sceneDoc || upd1 != upd2 {
		t.Fatalf("second seed run changed the row: %q %q %s %s", sc1, sc2, upd1, upd2)
	}
}
