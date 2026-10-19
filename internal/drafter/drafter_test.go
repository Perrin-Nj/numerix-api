package drafter

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestFromEnvNeedsKeyAndNarratorModel(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"nothing", nil, false},
		{"key only", map[string]string{"ANTHROPIC_API_KEY": "k"}, false},
		{"model only", map[string]string{"NARRATOR_MODEL": "m"}, false},
		{"both", map[string]string{"ANTHROPIC_API_KEY": "k", "NARRATOR_MODEL": "m"}, true},
	}
	for _, tc := range cases {
		if got := FromEnv(func(k string) string { return tc.env[k] }, quiet()) != nil; got != tc.want {
			t.Errorf("%s: configured=%v want %v", tc.name, got, tc.want)
		}
	}
	env := map[string]string{"ANTHROPIC_API_KEY": "k", "NARRATOR_MODEL": "m", "ANTHROPIC_DRAFT_MODEL": "d"}
	if p := FromEnv(func(k string) string { return env[k] }, quiet()); p.Model() != "d" {
		t.Errorf("draft model override: %s", p.Model())
	}
}

func TestDraftCallsMessagesAPIAndExtractsJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" || r.URL.Path != "/v1/messages" {
			http.Error(w, "bad", 400)
			return
		}
		fence := "`" + "`" + "`"
		text := "Here:\n" + fence + "json\n{\"id\":\"x\"}\n" + fence
		body, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": text}}})
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	a := NewAnthropic(AnthropicConfig{APIKey: "k", Model: "m", BaseURL: srv.URL})
	raw, err := a.Draft(context.Background(), Request{Quest: "q", Type: "numeric"})
	if err != nil || string(raw) != `{"id":"x"}` {
		t.Fatalf("raw=%s err=%v", raw, err)
	}
	bad := NewAnthropic(AnthropicConfig{APIKey: "wrong", Model: "m", BaseURL: srv.URL})
	if _, err := bad.Draft(context.Background(), Request{}); err == nil {
		t.Fatal("a 400 must be an error")
	}
	for _, s := range []string{"no json", "{broken", ""} {
		if _, err := ExtractJSON(s); err == nil {
			t.Errorf("ExtractJSON(%q) should fail", s)
		}
	}
}
