package app

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/Perrin-Nj/numerix-api/internal/appconfig"
)

// legacyPlatform gives a test server the platform settings of origin/main: every world is a default and ready world
// (the older tests play in nature, cuisine and so on) and grade-true play is off. Tests of the new behaviour set what
// they need on top.
func legacyPlatform(t *testing.T, s *Server) {
	t.Helper()
	ctx := context.Background()
	rows, err := s.Store.R.QueryContext(ctx, `SELECT id FROM worlds WHERE status = 'published' ORDER BY ord`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var all []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		all = append(all, id)
	}
	p := appconfig.Get[appconfig.Platform](ctx, s.Cfg, appconfig.NSPlatform)
	p.DefaultWorlds, p.ReadyWorlds, p.GradeWindow = all, all, false
	raw, _ := json.Marshal(p)
	if _, err := s.Cfg.Set(ctx, appconfig.NSPlatform, raw, "test", nil); err != nil {
		t.Fatal(err)
	}
}

// TestFocusWorldsAreChessMedicineSpace pins the production default: new children start in chess, medicine and space,
// and only those three are ready for a parent to turn on.
func TestFocusWorldsAreChessMedicineSpace(t *testing.T) {
	raw, err := appconfig.New(nil, false).Defaults(appconfig.NSPlatform)
	if err != nil {
		t.Fatal(err)
	}
	var p appconfig.Platform
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	want := []string{"echecs", "medecine", "espace"}
	if !slices.Equal(p.DefaultWorlds, want) || !slices.Equal(p.ReadyWorlds, want) {
		t.Fatalf("default worlds = %v, ready worlds = %v, want %v", p.DefaultWorlds, p.ReadyWorlds, want)
	}
	if !p.GradeWindow || !p.RetryEnabled {
		t.Fatal("grade_window and retry_enabled ship ON")
	}
}
