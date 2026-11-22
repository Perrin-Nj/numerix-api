package adminapi

import "testing"

// T004: origin/main only knew the first eight worlds in the admin settings rule. T018 replaced that list with the
// world registry: any published world is accepted, a draft or unknown one is not (see threeworlds_test.go).
func TestCharAdminKnowsEightWorlds(t *testing.T) {
	h := setup(t)
	if c, _, _ := call(h, "PATCH", "/admin/v1/children/c1", `{"settings":{"enabled_worlds":["ocean"]}}`, tok); c != 200 {
		t.Fatalf("ocean is a published world and is accepted now (origin/main refused it): %d", c)
	}
	if c, _, _ := call(h, "PATCH", "/admin/v1/children/c1", `{"settings":{"enabled_worlds":["nature","echecs"]}}`, tok); c != 200 {
		t.Fatalf("an original world is accepted: %d", c)
	}
}
