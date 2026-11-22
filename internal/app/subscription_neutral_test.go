package app

// T021: parents and children never see a trial or free wording. Sign-up still starts a silent trial; the responses
// say `active` with a neutral `access_until`. An admin free access (stored in subscriptions.json) opens play.

import (
	"strings"
	"testing"
	"time"
)

func (f feedFixture) subscription(t *testing.T, tok string) (int, map[string]any, string) {
	t.Helper()
	return f.gcall(t, "GET", "/v1/subscription", tok, "", nil)
}

func mustNotMention(t *testing.T, label, raw string) {
	t.Helper()
	for _, banned := range []string{`"trial"`, "trial_ends_at", "days_left", "free_access"} {
		if strings.Contains(raw, banned) {
			t.Fatalf("%s mentions %s: %s", label, banned, raw)
		}
	}
}

func TestSubscriptionIsNeutralForParentsAndChildren(t *testing.T) {
	f := newFeedFixture(t, true)
	end := time.Now().Add(10 * 24 * time.Hour).UTC().Format(plTSLayout)
	f.sql(t, `INSERT INTO subscriptions (id, parent_id, plan_id, status, trial_ends_at) VALUES ('s1', 'p-1', 'free', 'trial', ?)`, end)
	for name, tok := range map[string]string{"parent": f.parentTok, "child": f.childTok} {
		code, out, raw := f.subscription(t, tok)
		if code == 403 && name == "child" {
			continue // a child token may not read the subscription at all
		}
		if code != 200 || out["status"] != "active" || out["can_play"] != true || out["access_until"] == nil {
			t.Fatalf("%s: %d %s", name, code, raw)
		}
		mustNotMention(t, name+" subscription", raw)
	}
	// a trial that ended reads expired, with no trial field either
	f.sql(t, `UPDATE subscriptions SET trial_ends_at = ? WHERE id = 's1'`, time.Now().Add(-time.Hour).UTC().Format(plTSLayout))
	code, out, raw := f.subscription(t, f.parentTok)
	if code != 200 || out["status"] != "expired" || out["can_play"] != false {
		t.Fatalf("expired: %d %s", code, raw)
	}
	mustNotMention(t, "expired subscription", raw)
}

func TestFreeAccessOpensPlayAndReadsActive(t *testing.T) {
	f := newFeedFixture(t, true)
	f.sql(t, `INSERT INTO subscriptions (id, parent_id, plan_id, status, trial_ends_at) VALUES ('s1', 'p-1', 'free', 'trial', ?)`, time.Now().Add(-time.Hour).UTC().Format(plTSLayout))
	until := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	f.sql(t, `UPDATE subscriptions SET json = ? WHERE id = 's1'`, `{"free_access":{"granted_by":"root","granted_at":"2026-11-01T00:00:00.000Z","until":"`+until.Format(time.RFC3339)+`","reason":"pilot"}}`)
	code, out, raw := f.subscription(t, f.parentTok)
	if code != 200 || out["status"] != "active" || out["can_play"] != true || out["access_until"] != until.Format(time.RFC3339) {
		t.Fatalf("free access: %d %s", code, raw)
	}
	mustNotMention(t, "free access subscription", raw)
	if err := f.s.gamePlayAllowed(t.Context(), "p-1"); err != nil {
		t.Fatalf("play must be allowed while the free access runs: %v", err)
	}
	// an ended free access falls back to the real subscription (expired trial)
	f.sql(t, `UPDATE subscriptions SET json = ? WHERE id = 's1'`, `{"free_access":{"until":"`+time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)+`"}}`)
	if code, out, _ := f.subscription(t, f.parentTok); code != 200 || out["status"] != "expired" || out["can_play"] != false {
		t.Fatalf("ended free access: %d %v", code, out)
	}
	// no end date: stays open
	f.sql(t, `UPDATE subscriptions SET json = '{"free_access":{"until":null,"reason":"founder"}}' WHERE id = 's1'`)
	if code, out, _ := f.subscription(t, f.parentTok); code != 200 || out["status"] != "active" || out["can_play"] != true || out["access_until"] != nil {
		t.Fatalf("open ended free access: %d %v", code, out)
	}
}

func TestParentVisibleResponsesNeverSayTrial(t *testing.T) {
	f := newFeedFixture(t, true)
	for _, path := range []string{"/v1/platform/public", "/v1/subscription", "/v1/subscription/plans", "/v1/children/" + f.childID + "/export", "/v1/notifications", "/v1/config/public"} {
		code, _, raw := f.gcall(t, "GET", path, f.parentTok, "", nil)
		if code != 200 {
			t.Fatalf("%s: %d %s", path, code, raw)
		}
		low := strings.ToLower(raw)
		// the plan id `free` is a stable machine identifier of the contract (never rendered by the web), so it is not scanned
		for _, banned := range []string{"trial", "essai", "gratuit", "free_access"} {
			if strings.Contains(low, banned) && !(path == "/v1/subscription/plans" && banned == "gratuit") { // the yearly plan feature "deux mois gratuits" is a price promise, flagged to the owner
				t.Fatalf("%s mentions %q: %s", path, banned, raw)
			}
		}
	}
}

func TestYearlyPlanTextIsNeutral(t *testing.T) {
	f := newFeedFixture(t, true)
	_, _, raw := f.gcall(t, "GET", "/v1/subscription/plans", f.parentTok, "", nil)
	if !strings.Contains(raw, "Économisez avec le forfait annuel") || !strings.Contains(raw, "Save with the yearly plan") {
		t.Fatalf("yearly plan text: %s", raw)
	}
	for _, old := range []string{"Deux mois offerts", "Two months free"} {
		if strings.Contains(raw, old) {
			t.Fatalf("old text %q still served", old)
		}
	}
	if !strings.Contains(raw, `"id":"family_yearly"`) {
		t.Fatal("the plan id must not change")
	}
}

// M1: the default plan shows a neutral display name to parents; the id stays internal, the admin keeps the real name.
func TestDefaultPlanHasANeutralDisplayName(t *testing.T) {
	f := newFeedFixture(t, true)
	_, out, raw := f.gcall(t, "GET", "/v1/subscription/plans", f.parentTok, "", nil)
	found := false
	for _, p := range out["plans"].([]any) {
		m := p.(map[string]any)
		if m["id"] == "free" {
			found = true
			name := m["name"].(map[string]any)
			if name["fr"] != "Standard" || name["en"] != "Standard" {
				t.Fatalf("default plan name: %v", name)
			}
		}
	}
	if !found {
		t.Fatalf("the plan id stays: %s", raw)
	}
	for _, banned := range []string{"Gratuit", `"Free"`} {
		if strings.Contains(raw, banned) {
			t.Fatalf("%s still shown: %s", banned, raw)
		}
	}
	// a stored catalog that an admin edited is neutralised too
	f.sql(t, `INSERT OR REPLACE INTO plan_catalog (id, json) VALUES (1, ?)`, `{"currency":"CAD","methods":[],"plans":[{"id":"free","price_cad_cents":0,"period":"none","name":{"fr":"Gratuit","en":"Free"},"features":{"fr":[],"en":[]}}]}`)
	if _, _, raw = f.gcall(t, "GET", "/v1/subscription/plans", f.parentTok, "", nil); strings.Contains(raw, "Gratuit") || !strings.Contains(raw, "Standard") {
		t.Fatalf("stored catalog: %s", raw)
	}
}
