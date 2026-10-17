package visual

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// TestGolden compares Build with the output of the JavaScript mock (testdata/golden.json was produced by
// frontend/mock/lib/visual.mjs), so the Go server and the mock stay word for word identical.
func TestGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input struct {
			Kind   string         `json:"kind"`
			Params map[string]int `json:"params"`
		} `json:"input"`
		Lang string       `json:"lang"`
		Want MethodVisual `json:"want"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 20 {
		t.Fatalf("golden file too small: %d", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Want.CacheKey, func(t *testing.T) {
			got, ok := Build(c.Input.Kind, c.Input.Params, c.Lang)
			if !ok {
				t.Fatal("Build returned false")
			}
			if !reflect.DeepEqual(got, c.Want) {
				t.Fatalf("got  %+v\nwant %+v", got, c.Want)
			}
		})
	}
}

// TestBuildRejects covers unknown kinds, missing params and sharing into zero parts.
func TestBuildRejects(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		params map[string]int
	}{
		{"unknown kind", "pie_chart", map[string]int{"a": 1, "b": 2}},
		{"missing param", AddCarry, map[string]int{"a": 1}},
		{"zero parts", EqualShare, map[string]int{"total": 10, "parts": 0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := Build(tc.kind, tc.params, "fr"); ok {
				t.Fatal("expected false")
			}
		})
	}
}

// TestLanguageFallback: an unknown language reads as French; "en-CA" reads as English.
func TestLanguageFallback(t *testing.T) {
	p := map[string]int{"groups": 2, "size": 3}
	fr, _ := Build(EqualGroups, p, "de")
	en, _ := Build(EqualGroups, p, "en-CA")
	if fr.CacheKey != "equal_groups:2,3:fr" || en.CacheKey != "equal_groups:2,3:en" {
		t.Fatalf("keys: %s %s", fr.CacheKey, en.CacheKey)
	}
}
