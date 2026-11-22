package app

// T018/T019 (app side): one world registry (worlds.status), inactive worlds closed to children and parents, and
// decision 18: a parent can only turn on worlds an admin marked ready.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Perrin-Nj/numerix-api/internal/appconfig"
)

func TestDraftWorldIsHiddenAndClosed(t *testing.T) {
	f := newFeedFixture(t, true)
	f.sql(t, `UPDATE worlds SET status = 'draft' WHERE id = 'jeux'`)
	for _, tok := range []string{f.childTok, f.parentTok} {
		code, _, raw := f.gcall(t, "GET", "/v1/worlds", tok, "", nil)
		if code != 200 || strings.Contains(raw, `"jeux"`) || !strings.Contains(raw, `"nature"`) {
			t.Fatalf("a draft world must not be listed: %d", code)
		}
	}
	code, out, _ := f.gcall(t, "POST", "/v1/children/"+f.childID+"/sessions", f.childTok, `{"world":"jeux","kind":"chapter"}`, nil)
	if code != 422 || out["code"] != "world_inactive" {
		t.Fatalf("starting a chapter in a draft world: %d %v", code, out)
	}
	code, out, _ = f.gcall(t, "GET", "/v1/children/"+f.childID+"/journey?world=jeux", f.childTok, "", nil)
	if code != 422 || out["code"] != "world_inactive" {
		t.Fatalf("journey of a draft world: %d %v", code, out)
	}
	code, out, _ = f.gcall(t, "GET", "/v1/children/"+f.childID+"/journey?world=atlantis", f.childTok, "", nil)
	if code != 422 || out["code"] != "validation_failed" {
		t.Fatalf("an unknown world stays a validation error: %d %v", code, out)
	}
}

func TestParentSettingsRespectReadyAndPublishedWorlds(t *testing.T) {
	f := newFeedFixture(t, true)
	f.sql(t, `UPDATE worlds SET status = 'draft' WHERE id = 'jeux'`)
	f.setPlatform(t, func(p *appconfig.Platform) {
		p.DefaultWorlds, p.ReadyWorlds = []string{"echecs"}, []string{"echecs", "medecine", "espace", "nature"}
	})
	put := func(tok string, worlds ...string) (int, map[string]any) {
		b, _ := json.Marshal(map[string]any{"daily_goal_chapters": 1, "enabled_worlds": worlds, "read_aloud": true, "sound": true})
		code, out, _ := f.gcall(t, "PUT", "/v1/children/"+f.childID+"/settings", tok, string(b), nil)
		return code, out
	}
	if code, out := put(f.parentTok, "echecs", "nature"); code != 200 {
		t.Fatalf("ready worlds are accepted: %d %v", code, out)
	}
	if code, out := put(f.parentTok, "echecs", "jeux"); code != 422 || out["code"] != "world_inactive" {
		t.Fatalf("a draft world is refused: %d %v", code, out)
	}
	if code, out := put(f.parentTok, "echecs", "cuisine"); code != 422 || out["code"] != "world_not_ready" {
		t.Fatalf("a published world that is not ready is refused with a clear code: %d %v", code, out)
	}
	if code, out := put(f.parentTok, "echecs", "atlantis"); code != 422 || out["code"] != "validation_failed" {
		t.Fatalf("an unknown world: %d %v", code, out)
	}
	// stored settings are never rewritten: a child that already has a world switched on keeps it when the parent saves again
	f.sql(t, `INSERT INTO child_settings (child_id, json) VALUES (?, ?) ON CONFLICT(child_id) DO UPDATE SET json = excluded.json`, f.childID, `{"enabled_worlds":["echecs","cuisine"]}`)
	if code, out := put(f.parentTok, "echecs", "cuisine"); code != 200 {
		t.Fatalf("keeping an already enabled world is allowed: %d %v", code, out)
	}
	// the admin widens the ready list: the same request now passes for a new world
	f.setPlatform(t, func(p *appconfig.Platform) { p.ReadyWorlds = append(p.ReadyWorlds, "sport") })
	if code, out := put(f.parentTok, "echecs", "sport"); code != 200 {
		t.Fatalf("a world the admin marked ready: %d %v", code, out)
	}
}

func TestDefaultWorldsComeFromTheConfig(t *testing.T) {
	f := newFeedFixture(t, true)
	f.setPlatform(t, func(p *appconfig.Platform) { p.DefaultWorlds = []string{"espace"} })
	got := f.s.plDefaultSettings(context.Background()).EnabledWorlds
	if len(got) != 1 || got[0] != "espace" {
		t.Fatalf("new child worlds = %v", got)
	}
	f.sql(t, `DELETE FROM child_settings`)
	if w := f.s.enabledWorlds(context.Background(), f.childID); len(w) != 1 || w[0] != "espace" {
		t.Fatalf("fallback for a child without settings = %v", w)
	}
}

// The admin cannot make a draft world a default or a ready world through the config editor.
func TestConfigRefusesUnpublishedDefaultWorlds(t *testing.T) {
	f := newFeedFixture(t, true)
	f.sql(t, `UPDATE worlds SET status = 'draft' WHERE id = 'jeux'`)
	p := appconfig.Get[appconfig.Platform](context.Background(), f.s.Cfg, appconfig.NSPlatform)
	p.ReadyWorlds = append(p.ReadyWorlds, "jeux")
	raw, _ := json.Marshal(p)
	if _, err := f.s.Cfg.Set(context.Background(), appconfig.NSPlatform, raw, "t", nil); err == nil {
		t.Fatal("a draft world must be refused as a ready world")
	}
}

// The world list tells the parent app which worlds it may offer in the toggle.
func TestWorldsExposeReadyFlag(t *testing.T) {
	f := newFeedFixture(t, true)
	f.setPlatform(t, func(p *appconfig.Platform) {
		p.DefaultWorlds, p.ReadyWorlds = []string{"echecs"}, []string{"echecs", "espace"}
	})
	for _, tok := range []string{f.parentTok, f.childTok} {
		code, out, _ := f.gcall(t, "GET", "/v1/worlds", tok, "", nil)
		if code != 200 {
			t.Fatalf("%d", code)
		}
		ready := map[string]bool{}
		for _, it := range out["items"].([]any) {
			m := it.(map[string]any)
			r, ok := m["ready"].(bool)
			if !ok {
				t.Fatalf("every world carries ready: %v", m["id"])
			}
			ready[m["id"].(string)] = r
		}
		if !ready["echecs"] || !ready["espace"] || ready["cuisine"] || ready["nature"] {
			t.Fatalf("ready flags: %v", ready)
		}
	}
}
