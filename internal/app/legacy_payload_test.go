package app

// T017/T031: a legacy client (no `attempt`) gets the payload of origin/main. The expected key sets were recorded by
// running the same scenarios on origin/main (524afb4).

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func keysOf(m map[string]any) string {
	var k []string
	for key := range m {
		k = append(k, key)
	}
	sort.Strings(k)
	return strings.Join(k, ",")
}

func TestLegacyPayloadKeySetsAreUnchanged(t *testing.T) {
	f := newFeedFixture(t, true)
	sid, beatID, q := f.retryBeat(t, "cuisine.fournee.901", "cuisine", "ARI-OPE-A02c")
	open, _ := f.nextBeat(t, sid) // the same open beat again
	got := map[string]string{"beat": keysOf(open), "beat.question": keysOf(open["question"].(map[string]any))}
	_, wrong, _ := f.answerAttempt(t, sid, beatID, json.RawMessage(q.CommonErrors[0].Value), uuid.NewString(), 0)
	got["answer.wrong"], got["answer.session"] = keysOf(wrong), keysOf(wrong["session"].(map[string]any))
	_, hint, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats/"+beatID+"/hint", f.childTok, "{}", nil)
	got["hint.after-answer"] = keysOf(hint)
	sid2 := f.startChapter(t, "ARI-OPE-A06a")
	_, b2, _ := f.gcall(t, "POST", "/v1/sessions/"+sid2+"/beats", f.childTok, "{}", nil)
	got["gen.beat.question"] = keysOf(b2["question"].(map[string]any))
	_, a2 := f.answer(t, sid2, b2["id"].(string), 1, uuid.NewString())
	got["gen.answer"] = keysOf(a2)
	want := map[string]string{
		"beat":              "created_at,id,index,question,scene,scene_key,session_id,skill_id",
		"beat.question":     "choices,hint_available,kind,math_text,media,prompt,story",
		"answer.wrong":      "correct_answer,denouement,explain,feedback,misconception,outcome,rewards,scene_key,session",
		"answer.session":    "beats_answered,chapter_length,child_id,id,kind,score,started_at,status,story_title,variant_history,world",
		"hint.after-answer": "code,detail,status,title,type",
		"gen.beat.question": "choices,math_text",
		"gen.answer":        "correct_answer,denouement,method_visual,outcome,rewards,scene_key,session",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s keys changed for legacy clients:\n got  %s\n want %s", k, got[k], w)
		}
	}
}
