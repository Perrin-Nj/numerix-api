package narrator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestCannedGolden checks the canned bank against the mock's own texts (testdata/golden.json comes from
// frontend/mock/lib/session.mjs): same pick, same slots, same words, in Go and in JS.
func TestCannedGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		World   string `json:"world"`
		Lang    string `json:"lang"`
		Seed    uint32 `json:"seed"`
		Index   int    `json:"index"`
		Phase   string `json:"phase"`
		Outcome string `json:"outcome"`
		Math    string `json:"math"`
		Want    string `json:"want"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	c := NewCanned()
	for _, tc := range cases {
		got, err := c.Generate(context.Background(), Request{World: tc.World, Lang: tc.Lang, Phase: tc.Phase, Outcome: tc.Outcome, Seed: tc.Seed, Index: tc.Index, MathText: tc.Math})
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.Want {
			t.Fatalf("%s %s seed=%d i=%d %s/%s\n got: %s\nwant: %s", tc.World, tc.Lang, tc.Seed, tc.Index, tc.Phase, tc.Outcome, got, tc.Want)
		}
	}
	if len(cases) < 300 {
		t.Fatalf("golden too small: %d", len(cases))
	}
}

// TestCannedEveryWorld: every world and language gives a non-empty scene and three denouements, with no leftover {slot}.
func TestCannedEveryWorld(t *testing.T) {
	for w := range kit {
		for _, l := range []string{"fr", "en"} {
			for _, math := range []string{"6 × 7 = ?", "12 + 30 = ?", "50 − 8 = ?", "7 + ? = 20", "48 ÷ 6 = ?", "47 ÷ 6 = ? (avec reste)", "? + 3 = 9", "On partage 12 objets. Combien ?"} {
				txt, err := NewCanned().Generate(context.Background(), Request{World: w, Lang: l, Phase: PhaseScene, MathText: math, Seed: 9})
				if err != nil || txt == "" || strings.ContainsAny(txt, "{}") {
					t.Fatalf("%s/%s/%q: %q %v", w, l, math, txt, err)
				}
			}
			for _, o := range []string{"correct", "erreur_typique", "erreur_aleatoire"} {
				txt, err := NewCanned().Generate(context.Background(), Request{World: w, Lang: l, Phase: PhaseDenouement, Outcome: o, Seed: 9})
				if err != nil || txt == "" || strings.ContainsAny(txt, "{}") {
					t.Fatalf("%s/%s/%s: %q %v", w, l, o, txt, err)
				}
			}
		}
	}
	if _, err := NewCanned().Generate(context.Background(), Request{Phase: "bogus"}); err == nil {
		t.Fatal("unknown phase must fail")
	}
}

// TestLeaks is a table test of the leak check.
func TestLeaks(t *testing.T) {
	tests := []struct {
		name string
		text string
		bad  []string
		want bool
	}{
		{"whole number found", "Il y a 42 graines", []string{"42"}, true},
		{"not inside a bigger number", "Il y a 142 graines", []string{"42"}, false},
		{"number at end", "Le total est 42", []string{"42"}, true},
		{"phrase found case-insensitive", "C’est LE NOMBRE TOTAL d’objets", []string{"le nombre total d’objets"}, true},
		{"no tokens", "Rien à voir", nil, false},
		{"empty token ignored", "texte", []string{" "}, false},
		{"second token leaks", "6 sacs", []string{"13", "6"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Leaks(tc.text, tc.bad); got != tc.want {
				t.Fatalf("Leaks(%q,%v)=%v", tc.text, tc.bad, got)
			}
		})
	}
}

// fakeProvider returns a fixed text or error and counts calls.
type fakeProvider struct {
	text  string
	err   error
	calls int
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) Generate(context.Context, Request) (string, error) {
	f.calls++
	return f.text, f.err
}

// memCache is an in-memory Cache for tests.
type memCache struct {
	mu sync.Mutex
	m  map[string]string
}

func (c *memCache) Get(_ context.Context, k string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok, nil
}
func (c *memCache) Put(_ context.Context, k, v, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[k] = v
	return nil
}

// TestNarratorRules covers canned-only, provider success with cache, leak, error and empty text.
func TestNarratorRules(t *testing.T) {
	req := Request{World: "nature", Lang: "fr", Phase: PhaseScene, MathText: "6 × 7 = ?", Seed: 1, CacheKey: "k1"}
	ctx := context.Background()

	n := New(nil, nil, nil)
	if r := n.Narrate(ctx, req, []string{"42"}); r.Status != StatusReady || r.Source != "canned" || n.Mode() != "stub" {
		t.Fatalf("canned only: %+v mode=%s", r, n.Mode())
	}

	good := &fakeProvider{text: "  Mira sourit et ouvre la porte.  "}
	cache := &memCache{m: map[string]string{}}
	n = New(good, cache, nil)
	r := n.Narrate(ctx, req, []string{"42"})
	if r.Text != "Mira sourit et ouvre la porte." || r.Source != "fake" || r.Status != StatusReady {
		t.Fatalf("provider: %+v", r)
	}
	if r = n.Narrate(ctx, req, []string{"42"}); r.Source != "cache" || good.calls != 1 {
		t.Fatalf("second call must hit the cache: %+v calls=%d", r, good.calls)
	}

	for name, p := range map[string]*fakeProvider{
		"leak":   {text: "Le résultat est 42 !"},
		"error":  {err: errors.New("boom")},
		"empty":  {text: "   "},
		"budget": {err: ErrBudget},
	} {
		t.Run(name, func(t *testing.T) {
			r := New(p, nil, nil).Narrate(ctx, req, []string{"42"})
			if r.Status != StatusFallback || r.Source != "canned" || r.Text == "" || Leaks(r.Text, []string{"42"}) {
				t.Fatalf("want canned fallback, got %+v", r)
			}
		})
	}

	// a leaking cached text is not reused
	cache.m["k2"] = "C’est 42"
	r = New(&fakeProvider{text: "Une belle scène."}, cache, nil).Narrate(ctx, Request{World: "nature", Lang: "fr", Phase: PhaseScene, MathText: "6 × 7 = ?", CacheKey: "k2"}, []string{"42"})
	if r.Source != "fake" {
		t.Fatalf("leaky cache entry must be ignored: %+v", r)
	}
}

// TestWords checks the SSE chunking keeps all text.
func TestWords(t *testing.T) {
	text := "un deux trois quatre cinq six sept"
	if got := strings.Join(Words(text), ""); got != text {
		t.Fatalf("chunks lost text: %q", got)
	}
	if n := len(Words(text)); n != 2 {
		t.Fatalf("want 2 chunks, got %d", n)
	}
}

// TestAnthropic runs the HTTP provider against a fake server: success, API error, cost cap, timeout.
func TestAnthropic(t *testing.T) {
	var gotKey, gotVersion, gotBody string
	mode := "ok"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotVersion = r.Header.Get("x-api-key"), r.Header.Get("anthropic-version")
		b := new(strings.Builder)
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		gotBody = b.String()
		switch mode {
		case "error":
			http.Error(w, "nope", http.StatusInternalServerError)
		case "slow":
			time.Sleep(300 * time.Millisecond)
			fallthrough
		default:
			_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"Mira ouvre la porte."}],"usage":{"input_tokens":1000000,"output_tokens":0}}`))
		}
	}))
	defer srv.Close()

	a := NewAnthropic(AnthropicConfig{APIKey: "k", Model: "m", BaseURL: srv.URL, Timeout: 100 * time.Millisecond, DailyCapUSD: 5, PriceInUSD: 3, PriceOutUSD: 15})
	ctx := context.Background()
	req := Request{World: "nature", Lang: "fr", Phase: PhaseScene, Payload: map[string]any{"enonce_math": "6 × 7 = ?"}}

	txt, err := a.Generate(ctx, req)
	if err != nil || txt != "Mira ouvre la porte." {
		t.Fatalf("ok: %q %v", txt, err)
	}
	if gotKey != "k" || gotVersion != "2023-06-01" || !strings.Contains(gotBody, `"model":"m"`) || strings.Contains(gotBody, "reponse") {
		t.Fatalf("request: key=%s version=%s body=%s", gotKey, gotVersion, gotBody)
	}
	if s := a.SpentToday(); s < 2.99 || s > 3.01 {
		t.Fatalf("1M input tokens at $3 should cost $3, got %.3f", s)
	}

	// second call costs another $3: spend reaches $6, over the $5 cap, so the third call is refused
	if _, err = a.Generate(ctx, req); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Generate(ctx, req); !errors.Is(err, ErrBudget) {
		t.Fatalf("want ErrBudget, got %v", err)
	}
	// the cap resets on the next UTC day
	a.now = func() time.Time { return time.Now().Add(48 * time.Hour) }
	if _, err = a.Generate(ctx, req); err != nil {
		t.Fatalf("new day must reset the cap: %v", err)
	}

	mode = "error"
	b := NewAnthropic(AnthropicConfig{APIKey: "k", Model: "m", BaseURL: srv.URL, Timeout: time.Second})
	if _, err = b.Generate(ctx, req); err == nil {
		t.Fatal("HTTP 500 must be an error")
	}
	mode = "slow"
	if _, err = a.Generate(ctx, req); err == nil {
		t.Fatal("timeout must be an error")
	}
}

// TestFromEnv: the provider exists only when both key and model are set.
func TestFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"nothing", map[string]string{}, false},
		{"key only", map[string]string{"ANTHROPIC_API_KEY": "k"}, false},
		{"model only", map[string]string{"NARRATOR_MODEL": "m"}, false},
		{"both", map[string]string{"ANTHROPIC_API_KEY": "k", "NARRATOR_MODEL": "m"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := FromEnv(env(tc.env), slog.New(slog.NewTextHandler(io.Discard, nil))) != nil; got != tc.want {
				t.Fatalf("got %v", got)
			}
		})
	}
}
