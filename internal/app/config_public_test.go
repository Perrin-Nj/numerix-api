package app

// Tests for the admin-controlled configuration as the game sees it: the public endpoint, its cache, and the
// switches that change behaviour (maintenance block, hidden Discover items).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/appconfig"
	"github.com/Perrin-Nj/numerix-api/internal/feedmedia"
)

func TestPublicConfigEndpointAndCache(t *testing.T) {
	f := newFeedFixture(t, true)
	get := func() (*httptest.ResponseRecorder, map[string]any) {
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/config/public", nil)) // no token: public
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return rec, m
	}
	rec, m := get()
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "max-age=60") {
		t.Fatalf("public config: %d %v", rec.Code, rec.Header())
	}
	if m["maintenance"].(map[string]any)["enabled"] != false || m["music"].(map[string]any)["default_volume"] != 30.0 {
		t.Fatalf("defaults: %v", m)
	}
	// an admin save is visible at once (cache invalidated), no restart
	if _, err := f.s.Cfg.Set(context.Background(), "maintenance", []byte(`{"enabled":true,"level":"warning","message":{"fr":"Entretien","en":"Maintenance"}}`), "admin", nil); err != nil {
		t.Fatal(err)
	}
	_, m = get()
	if mt := m["maintenance"].(map[string]any); mt["enabled"] != true || mt["level"] != "warning" {
		t.Fatalf("after save: %v", mt)
	}
}

// The Go document and the generated contract type must agree on every key (drift guard).
func TestPublicConfigMatchesContract(t *testing.T) {
	f := newFeedFixture(t, true)
	raw, err := f.s.Cfg.PublicJSON(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var out api.AppPublicConfig
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("public document drifted from the contract: %v", err)
	}
}

func TestMaintenanceBlocksPlayOnlyWhenAsked(t *testing.T) {
	f := newFeedFixture(t, true)
	ctx := context.Background()
	set := func(body string) {
		t.Helper()
		if _, err := f.s.Cfg.Set(ctx, "maintenance", []byte(body), "admin", nil); err != nil {
			t.Fatal(err)
		}
	}
	banner := `"enabled":true,"level":"info","message":{"fr":"Entretien","en":"Maintenance"}`
	set(`{` + banner + `,"block_play":false}`)
	if err := f.s.gamePlayAllowed(ctx, "p-1"); err != nil {
		t.Fatalf("a banner alone must not block play: %v", err)
	}
	set(`{` + banner + `,"block_play":true}`)
	if err := f.s.gamePlayAllowed(ctx, "p-1"); err == nil || !strings.Contains(err.Error(), "Maintenance") && !strings.Contains(err.Error(), "maintenance") {
		t.Fatalf("block_play must refuse: %v", err)
	}
}

func TestDiscoverHonoursHiddenItemsAndStretch(t *testing.T) {
	items := []feedmedia.Item{{ID: "a", GradeMin: 1}, {ID: "b", GradeMin: 2}, {ID: "c", GradeMin: 4}}
	fc := appconfig.Feed{DiscoverEnabled: true, MaxGradeStretch: 1, HiddenItemIDs: []string{"a"}}
	got := discoverEligible(items, 1, fc)
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("hidden + stretch: %+v", got)
	}
	fc.MaxGradeStretch = 0
	if got := discoverEligible(items, 1, fc); len(got) != 0 {
		t.Fatalf("stretch 0 keeps only grade 1, which is hidden: %+v", got)
	}
}

var _ = http.StatusOK
